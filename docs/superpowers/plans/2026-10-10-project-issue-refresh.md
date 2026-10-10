# project 对账补全（Issue 判等刷新 + 版本帖幂等发布）实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 补全 `usm project` 三处 SPEC 缺口——已有 Issue 判等刷新（含回填 tracking / 重开四分支）、`EnsureDiscussionPost` 版本帖账本幂等发布、内容仓 workflow Commit 条件修复。

**架构：** `internal/projector` 拆两阶段：`reconcileIssues`（SPEC 四分支 Issue 对账）+ `publishDiscussions`（账本幂等发帖），`Project()` 编排并把计数累加进 `Result`；`runProject` 回帖与 changed 判定扩四项或。跨仓单独修 `userscripts/.github/workflows/issue-commands.yml` 的 Commit 条件。

**技术栈：** Go 1.24（标准库 + 既有 fake doer 测试模式）、GitHub GraphQL、GitHub Actions

**规格：** `docs/superpowers/specs/2026-10-10-project-issue-refresh-design.md`（本仓，commit a5c432e，已批准）

---

## 执行约定（沿用项目既有约束）

- 仓库：`C:\Users\LiuJi\Desktop\project-06\userscript-console`（工具仓），master 直接提交（用户已授权）；跨仓任务 9 在 `C:\Users\LiuJi\Desktop\project-06\userscripts`（内容仓）。
- 每任务一个中文 Conventional Commit，只 `git add` 本任务列出的文件；一步一验证。
- Windows PowerShell 5.1：**禁止 `&&`**（用 `cmd1; if ($?) { cmd2 }`）；禁止 Set-Content/Out-File 直改源码（一律 Edit/Write 工具，保 UTF-8）。
- lint 命令：`$(go env GOPATH)\bin\golangci-lint.exe run`（`.golangci.yml` v2，errcheck/gocritic/govet/ineffassign/staticcheck/unused + gofumpt）。
- 本仓无 `gh`、无 `rg`；验收用 `Invoke-RestMethod` + GitHub REST API。
- 测试不得 skip/only；禁止占位符代码。

## 文件结构

| 文件 | 职责 | 涉及任务 |
| --- | --- | --- |
| `internal/github/queries.go` | `DISCUSSION_CATEGORIES_QUERY` 补 `isDefault` 字段 | 1 |
| `internal/github/helpers.go` | `Category.IsDefault`、`gqlCategory` 解码、映射 | 1 |
| `internal/github/github_test.go` | 回归锁 must 条目 + 分类列表断言 | 1 |
| `internal/projector/project.go` | `Result` 扩字段、`Project()` 编排、`TombstoneIssue`/`BuildIssueBody`（git mv 自 projector.go） | 2、3、5 |
| `internal/projector/project_test.go` | 既有测试 + fake doer 基建（git mv 自 projector_test.go） | 2、3、5 |
| `internal/projector/reconcile.go` | 阶段一：`reconcileIssues` + `backfillIssue` + `refreshIssue` | 3、4 |
| `internal/projector/reconcile_test.go` | 阶段一全部测试（迁移自 5 个 TestEnsureIssue* + 新增判等/回填用例） | 3、4 |
| `internal/projector/discuss.go` | 阶段二：`selectCategory` + `EnsureDiscussionPost` + `publishDiscussions` | 5 |
| `internal/projector/discuss_test.go` | 账本四分支 + 分类选择 + 懒加载测试 | 5 |
| `internal/commands/project.go` | 回帖计数行 + changed 四项或 | 6 |
| `internal/commands/project_test.go` | `routeDoer` + 回帖/changed 集成测试 | 6 |
| `docs/commands/project.md` | 回帖示例与幂等语义同步 | 6 |
| `userscripts/.github/workflows/issue-commands.yml` | Commit 条件加 proj OR | 9（内容仓） |

## 前置知识（读代码时必须记住的事实）

1. **`EnsureIssue` 可安全重构**：签名里的 `r *registry.Registry` 参数未被使用；grep 全仓确认唯一调用方是 `Project()`（projector.go:72）与 5 个包内测试，无外部使用方。重构后删除该导出函数并在 commit body 注明。
2. **`REPO_QUERY` 双消费者**：`GetIssue`（variables `number`）与 `repoID()`（variables `number=1`）共用同一查询文档 → 测试响应必须是 combined 结构：`{"data":{"repository":{"id":"R_1","nameWithOwner":"o/r","issue":{...9字段}}}}`。`repoID` 带缓存（`repoIDFetched`），每个 client 生命周期内 REPO_QUERY 至多一次；`CreateIssue`/`CreateDiscussion` 内部都先调 `repoID`。
3. **`ListRepoIssues("ALL")`**：`states` 变量序列化为 `null`（helpers.go:120-123）；`"OPEN"` 会漏已关闭 Issue 导致重复建——这是回填分支必须改 `ALL` 的原因。
4. **`multiRespDoer`**（projector_test.go:32）按调用序回放，超出预置即 `t.Errorf`；目前只记 `lastQuery`，**任务 3 需扩 `queries []string`（全部查询）与 `lastVars map[string]any`（当次 variables）** 以支撑调用序/`states`/发帖 body 断言。
5. **判等即 I-1 契约**：`title 相等 && body 相等` 才跳过；线上 `Issue.State` 值为 `"OPEN"`/`"CLOSED"`。
6. **计数归属**：`backfillIssue`/`refreshIssue` 内部经 `res *Result` 指针累加 `Created/Updated/Reopened`；`publishDiscussions` 返回 `(posted, skipped, errs)` 由 `Project()` 累加。refresh **不** bump `s.UpdatedAt`（否则判等跳过轮也会弄脏 registry）；backfill 沿用现状 bump。
7. **`saveReg` 恒返回 `changed=true`**（commands.go:211）；workflow 看的是 `reply(changed, ...)` 的 bool → `main.go:417` 的 `res.Changed` → action.yml 的 `changed` 输出。changed 判定现位于 `project.go:67`。
8. **快照纪律**：`tests/snapshot/` 只锁 `issuepage` 投影产物（`BuildTitle/BuildBody/DiscussionBody`，取自 corpus 语料的 issues）+ registry canonical + 站点整页。本计划**不改** `issuepage` 与 `BuildIssueBody` 内容 → 预期 `go run ./cmd/usm snapshot check` 无漂移；若漂移，`snapshot update` 单独 commit 并说明原因。
9. **门禁基准**（test.yml）：`gofmt -l .` 空 → `go vet ./...` → golangci-lint v2.14.0 → `go test ./... -covermode=atomic -coverprofile=cover.out` + `go tool cover -func` 总覆盖率 **≥90%**（awk 门）→ `go run ./cmd/usm snapshot check` → registry.json 存在时 `go run ./cmd/usm doctor --check`（本仓不存在，跳过）→ `go build ./...`。
10. **发版链**：轻量 tag `vX.Y.Z` → release.yml（test→build→publish→移动 `v1` tag→回填 `internal/buildinfo/VERSION`）。最新 tag `v1.3.0`，下一个 **`v1.4.0`**。remote：工具仓 `git@github.com:ACG-Q/userscript-console.git`（HEAD a5c432e）、内容仓 `git@github.com:ACG-Q/userscripts.git`（HEAD 6d0bf2d，工作区干净）。
11. **issuepage.DiscussionTitle/Body**（issuepage.go:161/166）签名为**值接收** `(s registry.Script, version string)`；`issueRef` 无 Issue 时返回 `"—"`，不阻断发帖。
12. **registry.DiscussionEntry**（registry.go:52）：`{Version, Number, NodeID, URL, CreatedAt}`，由返回的 `github.Discussion` 填充。
13. **凭据**：`@("protocol=https","host=github.com","") | git credential fill` 取 PAT（解析 `password=` 行）；真实验收全程用 `Invoke-RestMethod`。

### 任务 1：GitHub 分类查询补 isDefault 字段

**文件：**
- 修改：`internal/github/queries.go:114-121`（DISCUSSION_CATEGORIES_QUERY）
- 修改：`internal/github/helpers.go:32-36`（Category）、`:108-112`（gqlCategory）、`:358-361`（映射）
- 测试：`internal/github/github_test.go:302-310`（回归锁 must）、`:525-536`（分类列表子测试）

- [ ] **步骤 1：编写失败的测试**

在 `github_test.go` 回归锁 `must` map（302 行）中追加一行（按现有字母序插在 `"DELETE_COMMENT_MUTATION"` 之后）：

```go
		"DISCUSSION_CATEGORIES_QUERY": {"discussionCategories(first: 25)", "isDefault", "nodes { id name slug isDefault }"},
```

改写 `TestDiscussion系列` 的 `分类列表` 子测试（525-536 行），响应加 `isDefault` 并断言：

```go
	t.Run("分类列表", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
			"repository": map[string]any{"discussionCategories": map[string]any{
				"nodes": []any{map[string]any{"id": "DC_1", "name": "版本", "slug": "releases", "isDefault": true}},
			}},
		}), nil}}}
		c := newTestClient(t, d)
		cats, err := c.DiscussionCategories(context.Background())
		if err != nil || len(cats) != 1 || cats[0].NodeID != "DC_1" {
			t.Fatalf("cats=%v err=%v", cats, err)
		}
		if !cats[0].IsDefault {
			t.Error("IsDefault 应解码为 true")
		}
	})
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./internal/github/ -run 'Test固定查询文档回归锁|TestDiscussion系列' -v`
预期：FAIL——回归锁报 `DISCUSSION_CATEGORIES_QUERY 缺 "isDefault"`，分类列表报 `undefined: cats[0].IsDefault`（编译错误也算失败）。

- [ ] **步骤 3：编写最少实现代码**

`queries.go:118` 改为：

```go
      nodes { id name slug isDefault }
```

`helpers.go` Category（32 行）：

```go
type Category struct {
	NodeID    string
	Name      string
Slug      string
	IsDefault bool
}
```

`helpers.go` gqlCategory（108 行）：

```go
type gqlCategory struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	IsDefault bool   `json:"isDefault"`
}
```

`helpers.go` DiscussionCategories 映射（360 行）：

```go
		cats = append(cats, Category{NodeID: n.ID, Name: n.Name, Slug: n.Slug, IsDefault: n.IsDefault})
```

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./internal/github/ -count=1`
预期：PASS。

- [ ] **步骤 5：Commit**

```powershell
git add internal/github/queries.go internal/github/helpers.go internal/github/github_test.go; if ($?) { git commit -m "feat(github): 分类查询补 isDefault 字段供版本帖分类选择" }
```

### 任务 2：projector 骨架重排（git mv + Result 扩字段 + 提取 reconcileIssues）

**文件：**
- 重命名：`internal/projector/projector.go` → `internal/projector/project.go`（`git mv`）
- 重命名：`internal/projector/projector_test.go` → `internal/projector/project_test.go`（`git mv`）
- 修改：`internal/projector/project.go`（Result 扩三字段；`Project()` 循环移入 `reconcile.go`）
- 创建：`internal/projector/reconcile.go`（本任务只放 `reconcileIssues`，逻辑 = 现 `Project()` 循环原样搬移）

- [ ] **步骤 1：重命名文件**

```powershell
git mv internal/projector/projector.go internal/projector/project.go; if ($?) { git mv internal/projector/projector_test.go internal/projector/project_test.go }
```

（docs 中无 `projector.go` 文本引用，grep 已确认；`Project()` 逻辑不变，仅文件名对齐设计 §1。）

- [ ] **步骤 2：Result 扩字段**

`project.go` 的 `Result`（42-49 行）改为：

```go
// Result 投影结果统计。
type Result struct {
	Active   int
	Deleted  int
	NoIssue  int
	Created  int
	Updated  int
	Errors   int
	Reopened int // 回填/判等时重开了已关闭 Issue
	Posted   int // 本轮发布版本帖数
	Skipped  int // 账本末条==当前，跳过发帖数
}
```

- [ ] **步骤 3：创建 reconcile.go（逻辑原样搬移，行为零变化）**

创建 `internal/projector/reconcile.go`：

```go
package projector

import (
	"context"

	"github.com/acg-q/userscript-console/internal/registry"
)

// reconcileIssues 阶段一：遍历 registry 做 Issue 对账。
// 本任务为骨架搬移（逻辑与原 Project 循环一致）；四分支扩展在任务 3/4 落地。
func reconcileIssues(ctx context.Context, env *Env, r *registry.Registry) Result {
	var res Result

	for i := range r.Scripts {
		s := &r.Scripts[i]
		if s.Deleted {
			res.Deleted++
			if s.Issue != nil && env.GHClient != nil {
				if err := TombstoneIssue(ctx, env.GHClient, s.Issue.NodeID); err != nil {
					res.Errors++
					continue
				}
				res.Updated++
			}
			continue
		}
		res.Active++
		if s.Issue == nil {
			res.NoIssue++
			if env.GHClient != nil {
				if err := EnsureIssue(ctx, env, r, s); err != nil {
					res.Errors++
					continue
				}
				res.Created++
			}
		}
	}

	return res
}
```

`project.go` 的 `Project()`（52-82 行）替换为：

```go
// Project 对账投影：阶段一 Issue 对账；阶段二发帖在任务 5 接线。
func Project(ctx context.Context, env *Env, r *registry.Registry) (Result, error) {
	return reconcileIssues(ctx, env, r), nil
}
```

（`EnsureIssue`/`TombstoneIssue`/`BuildIssueBody` 等函数仍留在 `project.go`，任务 3 再动。）

- [ ] **步骤 4：运行测试验证通过（重构护栏）**

运行：`go test ./internal/projector/ ./internal/commands/ -count=1`
预期：PASS（纯搬移，无行为变化；若有测试引用旧文件名路径属误记，包内符号未改名）。

- [ ] **步骤 5：Commit**

```powershell
git add internal/projector/project.go internal/projector/project_test.go internal/projector/reconcile.go; if ($?) { git commit -m "refactor(projector): 文件对齐设计命名并提取 reconcileIssues 骨架" }
```

### 任务 3：回填 tracking 分支（ALL 列表 + 关闭重开 + 迁移 EnsureIssue 测试）

**文件：**
- 修改：`internal/projector/reconcile.go`（`loadIssues` 懒加载闭包 + `backfillIssue`；删 `reconcileIssues` 内旧 EnsureIssue 调用）
- 修改：`internal/projector/project.go`（删除 `EnsureIssue` 函数，84-130 行）
- 修改：`internal/projector/project_test.go`（删 5 个 `TestEnsureIssue*`；`multiRespDoer` 扩 `queries`/`lastVars`）
- 创建：`internal/projector/reconcile_test.go`（迁移 + 新增回填测试）

- [ ] **步骤 1：扩 multiRespDoer 记录能力**

`project_test.go` 的 `multiRespDoer` 结构（32-37 行）改为：

```go
// multiRespDoer 按调用序返回预置响应。
type multiRespDoer struct {
	t         *testing.T
	resps     []string
	calls     int
	lastQuery string
	queries   []string          // 全部查询文档（调用序断言）
	lastVars  map[string]any    // 最近一次 variables
}
```

`Do` 方法中 `m.lastQuery = q` 之后追加：

```go
	m.queries = append(m.queries, q)
	m.lastVars, _ = m2["variables"].(map[string]any)
```

- [ ] **步骤 2：编写失败的测试**

从 `project_test.go` **删除** 5 个测试：`TestEnsureIssueCreate`、`TestEnsureIssueURLFallsBackToRepoOwner`、`TestEnsureIssueUpdate`、`TestEnsureIssueListError`、`TestEnsureIssueCreateError`（144-277 行）。

创建 `internal/projector/reconcile_test.go`，先写回填用例（下面 Test A/B 迁移改写，Test C/D/E 为新增；`fakeGHDoer`/`multiRespDoer`/`newTestGHClient` 复用 project_test.go 同包定义）：

```go
package projector

import (
	"context"
	"strings"
	"testing"

	"github.com/acg-q/userscript-console/internal/registry"
)

// newBackfillEnv 构造回填测试环境：脚本无 Issue，返回可注入 doer 的 env 工厂。
func newBackfillEnv(t *testing.T, doers ...interface{}) (*Env, *registry.Script, *multiRespDoer) {
	t.Helper()
	var d *multiRespDoer
	if len(doers) > 0 {
		d, _ = doers[0].(*multiRespDoer)
	}
	ghc := newTestGHClient(t, doers...)
	s := &registry.Script{
		ID: "new01", Name: "新脚本", Version: "1.0.0",
		Type: registry.TypeSelf, Enabled: true, Deleted: false,
		Changelog: []registry.ChangelogEntry{{Version: "1.0.0", Date: "2026-01-01", Note: "init"}},
	}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", PagesBase: "https://test.github.io/repo", GHClient: ghc}
	return env, s, d
}

const emptyListResp = `{"data":{"repository":{"issues":{"totalCount":0,"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[]}}}}`
const repoIDResp = `{"data":{"repository":{"id":"R_1","nameWithOwner":"o/r","issue":null}}}`
const createOKResp = `{"data":{"createIssue":{"issue":{"id":"I_new","number":42,"state":"OPEN","title":"T","body":"B","createdAt":"2026-01-01T00:00:00Z"}}}}`

// TestBackfillCreatesWhenNoMatch 未命中 → CreateIssue + Created++ + 写 ref + bump UpdatedAt。
func TestBackfillCreatesWhenNoMatch(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{emptyListResp, repoIDResp, createOKResp}}
	env, s, d := newBackfillEnv(t, d)
	var res Result
	if err := backfillIssue(context.Background(), env, s, &res); err != nil {
		t.Fatalf("backfillIssue 应成功: %v (query=%q)", err, d.lastQuery)
	}
	if s.Issue == nil || s.Issue.Number != 42 || s.Issue.NodeID != "I_new" {
		t.Fatalf("Issue ref 未写入: %+v", s.Issue)
	}
	if s.Issue.URL != "https://github.com/o/r/issues/42" {
		t.Errorf("URL = %q", s.Issue.URL)
	}
	if res.Created != 1 || res.Updated != 0 || res.Reopened != 0 {
		t.Errorf("Created=%d Updated=%d Reopened=%d, want 1/0/0", res.Created, res.Updated, res.Reopened)
	}
	if s.UpdatedAt == "" {
		t.Error("回填成功应 bump UpdatedAt")
	}
	if !strings.Contains(d.lastQuery, "createIssue(input:") {
		t.Errorf("最后一次查询应为 CreateIssue, got %q", d.lastQuery)
	}
}

// TestBackfillUsesALLStates 回填列表必须传 states=null（ALL），防重复建已关闭 Issue。
func TestBackfillUsesALLStates(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{emptyListResp, repoIDResp, createOKResp}}
	env, s, _ := newBackfillEnv(t, d)
	var res Result
	if err := backfillIssue(context.Background(), env, s, &res); err != nil {
		t.Fatalf("backfillIssue: %v", err)
	}
	if v, ok := d.lastVars["states"]; ok && v != nil {
		t.Errorf("states 应为 null(ALL)，实际 %#v", v)
	}
}

// TestBackfillMatchOpenReusesIssue 命中且开着 → UpdateIssue + Updated++（不 Reopen、不 Created）。
func TestBackfillMatchOpenReusesIssue(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		`{"data":{"repository":{"issues":{"totalCount":1,"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[{"id":"I_old","number":7,"state":"OPEN","title":"[新脚本] v1.0.0","body":"B","createdAt":"2026-01-01T00:00:00Z"}]}}}}`,
		`{"data":{"updateIssue":{"issue":{"id":"I_old","number":7,"state":"OPEN"}}}}`,
	}}
	env, s, d := newBackfillEnv(t, d)
	var res Result
	if err := backfillIssue(context.Background(), env, s, &res); err != nil {
		t.Fatalf("backfillIssue: %v", err)
	}
	if s.Issue == nil || s.Issue.Number != 7 || s.Issue.NodeID != "I_old" {
		t.Fatalf("应复用命中 Issue: %+v", s.Issue)
	}
	if res.Updated != 1 || res.Created != 0 || res.Reopened != 0 {
		t.Errorf("Updated=%d Created=%d Reopened=%d, want 1/0/0", res.Created, res.Updated, res.Reopened)
	}
}

// TestBackfillClosedReopensThenUpdates 命中且已关闭 → ReopenIssue 先于 UpdateIssue（调用序断言）+ Reopened++/Updated++。
func TestBackfillClosedReopensThenUpdates(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		`{"data":{"repository":{"issues":{"totalCount":1,"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[{"id":"I_old","number":7,"state":"CLOSED","title":"[新脚本] v1.0.0","body":"B","createdAt":"2026-01-01T00:00:00Z"}]}}}}`,
		`{"data":{"reopenIssue":{"issue":{"id":"I_old","number":7,"state":"OPEN"}}}}`,
		`{"data":{"updateIssue":{"issue":{"id":"I_old","number":7,"state":"OPEN"}}}}`,
	}}
	env, s, d := newBackfillEnv(t, d)
	var res Result
	if err := backfillIssue(context.Background(), env, s, &res); err != nil {
		t.Fatalf("backfillIssue: %v", err)
	}
	if len(d.queries) != 3 {
		t.Fatalf("queries = %d, want 3: %v", len(d.queries), d.queries)
	}
	if !strings.Contains(d.queries[1], "reopenIssue(input:") {
		t.Errorf("第 2 次应为 ReopenIssue: %q", d.queries[1])
	}
	if !strings.Contains(d.queries[2], "updateIssue(input:") {
		t.Errorf("第 3 次应为 UpdateIssue: %q", d.queries[2])
	}
	if res.Reopened != 1 || res.Updated != 1 || res.Created != 0 {
		t.Errorf("Reopened=%d Updated=%d Created=%d, want 1/1/0", res.Reopened, res.Updated, res.Created)
	}
}

// TestBackfillListErrorCountsError 列表失败 → 返回 error（调用方 Errors++），不写 ref。
func TestBackfillListErrorCountsError(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{`{"errors":[{"message":"forbidden"}]}`}}
	env, s, _ := newBackfillEnv(t, d)
	var res Result
	if err := backfillIssue(context.Background(), env, s, &res); err == nil {
		t.Fatal("列表失败应返回 error")
	}
	if s.Issue != nil {
		t.Error("失败不应写 ref")
	}
}

// TestReconcileSingleListForManyScripts 多个无 Issue 脚本只拉一次列表（N+1 防回归）。
func TestReconcileSingleListForManyScripts(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		emptyListResp, repoIDResp,
		createOKResp,
		`{"data":{"createIssue":{"issue":{"id":"I_b","number":43,"state":"OPEN","title":"T","body":"B","createdAt":"2026-01-01T00:00:00Z"}}}}`,
	}}
	ghc := newTestGHClient(t, d)
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		{ID: "a", Name: "脚本A", Version: "1.0.0", Type: registry.TypeSelf, Enabled: true},
		{ID: "b", Name: "脚本B", Version: "1.0.0", Type: registry.TypeSelf, Enabled: true},
	}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: ghc}
	res := reconcileIssues(context.Background(), env, r)
	listCalls := 0
	for _, q := range d.queries {
		if strings.Contains(q, "issues(first") {
			listCalls++
		}
	}
	if listCalls != 1 {
		t.Errorf("ListRepoIssues 应只调 1 次，实际 %d（queries=%v）", listCalls, d.queries)
	}
	if res.Created != 2 || res.Errors != 0 {
		t.Errorf("Created=%d Errors=%d, want 2/0", res.Created, res.Errors)
	}
}

// TestReconcileAPIErrorContinues 每步 API 错误 → Errors++ 并继续下一脚本。
func TestReconcileAPIErrorContinues(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		`{"errors":[{"message":"boom"}]}`, // 脚本 a 列表失败
	}}
	ghc := newTestGHClient(t, d)
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		{ID: "a", Name: "脚本A", Version: "1.0.0", Type: registry.TypeSelf, Enabled: true},
		{ID: "b", Name: "脚本B", Version: "1.0.0", Type: registry.TypeSelf, Enabled: true},
	}}
	env := &Env{Root: "/tmp", RepoOwner: "o", GHClient: ghc}
	res := reconcileIssues(context.Background(), env, r)
	if res.Errors < 1 {
		t.Errorf("Errors = %d, want >=1", res.Errors)
	}
	// 第二脚本仍被处理（Active 全量统计）
	if res.Active != 2 {
		t.Errorf("Active = %d, want 2", res.Active)
	}
}
```

注意 `backfillIssue` 的签名按本任务实现定为 `backfillIssue(ctx context.Context, env *Env, s *registry.Script, res *Result) error`（列表由函数内部按需拉取一次并缓存，见步骤 3 的 `loadIssues` 方案；若采用闭包注入则签名带 `loadIssues` 参数，测试同步调整——**以步骤 3 实现为准，回改测试签名**）。

- [ ] **步骤 3：运行测试验证失败**

运行：`go test ./internal/projector/ -run 'Backfill|Reconcile' -count=1`
预期：FAIL——`undefined: backfillIssue`。

- [ ] **步骤 4：编写最少实现代码**

`reconcile.go` 顶部加 import `"fmt"`、`"strings"`、`"github.com/acg-q/userscript-console/internal/github"`，并改写 `reconcileIssues` + 新增 `backfillIssue`：

```go
func reconcileIssues(ctx context.Context, env *Env, r *registry.Registry) Result {
	var res Result

	// 整轮只拉一次 Issue 列表（含关闭态），多个 Issue==nil 脚本复用，防 N+1。
	var cachedIssues []github.Issue
	var listErr error
	listed := false
	loadIssues := func() ([]github.Issue, error) {
		if !listed {
			cachedIssues, listErr = env.GHClient.ListRepoIssues(ctx, "ALL")
			listed = true
		}
		return cachedIssues, listErr
	}

	for i := range r.Scripts {
		s := &r.Scripts[i]
		if s.Deleted {
			res.Deleted++
			if s.Issue != nil && env.GHClient != nil {
				if err := TombstoneIssue(ctx, env.GHClient, s.Issue.NodeID); err != nil {
					res.Errors++
					continue
				}
				res.Updated++
			}
			continue
		}
		res.Active++
		if s.Issue == nil {
			res.NoIssue++
			if env.GHClient != nil {
				if err := backfillIssue(ctx, env, s, loadIssues, &res); err != nil {
					res.Errors++
					continue
				}
			}
		}
	}

	return res
}

// backfillIssue 回填 tracking 分支：按标题前缀 [Name] 匹配（含已关闭），
// 命中→重开(如需)+更新+写 ref；未命中→新建+写 ref。
func backfillIssue(ctx context.Context, env *Env, s *registry.Script, loadIssues func() ([]github.Issue, error), res *Result) error {
	existing, err := loadIssues()
	if err != nil {
		return fmt.Errorf("列出 Issue 失败: %w", err)
	}

	titlePrefix := fmt.Sprintf("[%s]", s.Name)
	var matched *github.Issue
	for i := range existing {
		if strings.HasPrefix(existing[i].Title, titlePrefix) {
			matched = &existing[i]
			break
		}
	}

	issueBody := BuildIssueBody(s, env.PagesBase, env.distSeg())

	if matched == nil {
		iss, err := env.GHClient.CreateIssue(ctx, titlePrefix+" v"+s.Version, issueBody)
		if err != nil {
			return fmt.Errorf("创建 Issue 失败: %w", err)
		}
		s.Issue = &registry.IssueRef{Number: iss.Number, NodeID: iss.NodeID, URL: issueURL(env, iss.Number)}
		res.Created++
		s.UpdatedAt = nowRFC3339()
		return nil
	}

	if matched.State == "CLOSED" {
		if err := env.GHClient.ReopenIssue(ctx, matched.NodeID); err != nil {
			return fmt.Errorf("重开 Issue #%d 失败: %w", matched.Number, err)
		}
		res.Reopened++
	}
	if err := env.GHClient.UpdateIssue(ctx, matched.NodeID, titlePrefix+" v"+s.Version, issueBody); err != nil {
		return fmt.Errorf("更新 Issue #%d 失败: %w", matched.Number, err)
	}
	s.Issue = &registry.IssueRef{Number: matched.Number, NodeID: matched.NodeID, URL: issueURL(env, matched.Number)}
	res.Updated++
	s.UpdatedAt = nowRFC3339()
	return nil
}
```

`project.go` 删除整个 `EnsureIssue` 函数（84-130 行，含注释）。

- [ ] **步骤 5：运行测试验证通过**

运行：`go test ./internal/projector/ ./internal/commands/ -count=1`
预期：PASS。若 `backfillIssue` 签名与测试不一致，按实现回改测试调用处。

- [ ] **步骤 6：Commit**

```powershell
git add internal/projector/reconcile.go internal/projector/reconcile_test.go internal/projector/project.go internal/projector/project_test.go; if ($?) { git commit -m "feat(projector): 回填 tracking 分支支持已关闭 Issue 重开与整轮单次列表

BREAKING CHANGE: 移除导出函数 projector.EnsureIssue，逻辑并入包内 backfillIssue
（grep 确认无外部调用方）。" }
```

### 任务 4：判等刷新分支（Issue != nil → GetIssue 判等 + 关闭重开）

**文件：**
- 修改：`internal/projector/reconcile.go`（`reconcileIssues` 增 `else if` 分支 + 新增 `refreshIssue`）
- 测试：`internal/projector/reconcile_test.go`（新增判等/重开/错误用例 + `repoIssueResp` 辅助）

- [ ] **步骤 1：编写失败的测试**

在 `reconcile_test.go` 追加辅助与用例：

```go
// repoIssueResp 构造 REPO_QUERY（GetIssue）combined 响应：repository.id + issue 9 字段。
func repoIssueResp(t *testing.T, number int, title, body, state string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{
		"id": "R_1", "nameWithOwner": "o/r",
		"issue": map[string]any{
			"id": "I_1", "number": number, "title": title, "body": body, "state": state,
			"createdAt": "2026-01-01T00:00:00Z", "updatedAt": "2026-01-01T00:00:00Z",
			"author":    map[string]any{"login": "bot"},
			"comments":  map[string]any{"totalCount": 0},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// refreshEnv 构造带已关联 Issue 脚本的环境。
func refreshEnv(t *testing.T, d *multiRespDoer) (*Env, *registry.Script) {
	t.Helper()
	s := &registry.Script{
		ID: "r1", Name: "回填", Version: "1.2.0", Type: registry.TypeSelf, Enabled: true,
		UpdatedAt: "2026-01-01T00:00:00Z",
		Issue:     &registry.IssueRef{Number: 5, NodeID: "I_5", URL: "https://github.com/o/r/issues/5"},
	}
	env := &Env{
		Root: "/tmp", RepoOwner: "o", RepoName: "o/r",
		PagesBase: "https://test.github.io/repo", GHClient: newTestGHClient(t, d),
	}
	return env, s
}

// TestRefreshIssueEqualSkipsUpdate 线上标题/正文一致 → 仅 1 次 GetIssue、零写、不 bump UpdatedAt。
func TestRefreshIssueEqualSkipsUpdate(t *testing.T) {
	env, s := refreshEnv(t, nil)
	title := "[回填] v1.2.0"
	body := BuildIssueBody(s, env.PagesBase, env.distSeg())
	d := &multiRespDoer{t: t, resps: []string{repoIssueResp(t, 5, title, body, "OPEN")}}
	env.GHClient = newTestGHClient(t, d)

	var res Result
	if err := refreshIssue(context.Background(), env, s, &res); err != nil {
		t.Fatalf("refreshIssue: %v", err)
	}
	if res.Updated != 0 || res.Reopened != 0 {
		t.Errorf("Updated=%d Reopened=%d, want 0/0", res.Updated, res.Reopened)
	}
	if s.UpdatedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("判等跳过不应 bump UpdatedAt: %s", s.UpdatedAt)
	}
	if d.calls != 1 {
		t.Errorf("calls = %d, want 1（仅 GetIssue）", d.calls)
	}
}

// TestRefreshIssueTitleMismatchUpdates 标题不等 → UpdateIssue + Updated++。
func TestRefreshIssueTitleMismatchUpdates(t *testing.T) {
	env, s := refreshEnv(t, nil)
	d := &multiRespDoer{t: t, resps: []string{
		repoIssueResp(t, 5, "[回填] v0.9.0", BuildIssueBody(s, env.PagesBase, env.distSeg()), "OPEN"),
		`{"data":{"updateIssue":{"issue":{"id":"I_5","number":5,"state":"OPEN"}}}}`,
	}}
	env.GHClient = newTestGHClient(t, d)

	var res Result
	if err := refreshIssue(context.Background(), env, s, &res); err != nil {
		t.Fatalf("refreshIssue: %v", err)
	}
	if res.Updated != 1 || res.Reopened != 0 {
		t.Errorf("Updated=%d Reopened=%d, want 1/0", res.Updated, res.Reopened)
	}
	if len(d.queries) != 2 || !strings.Contains(d.queries[1], "updateIssue(input:") {
		t.Errorf("第 2 次调用应为 UpdateIssue: %v", d.queries)
	}
	if s.UpdatedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("判等刷新不应 bump UpdatedAt: %s", s.UpdatedAt)
	}
}

// TestRefreshIssueClosedReopensFirst 线上已关闭且内容不等 → Reopen 先于 Update + Reopened++。
func TestRefreshIssueClosedReopensFirst(t *testing.T) {
	env, s := refreshEnv(t, nil)
	d := &multiRespDoer{t: t, resps: []string{
		repoIssueResp(t, 5, "[回填] v0.9.0", "旧正文", "CLOSED"),
		`{"data":{"reopenIssue":{"issue":{"id":"I_5","number":5,"state":"OPEN"}}}}`,
		`{"data":{"updateIssue":{"issue":{"id":"I_5","number":5,"state":"OPEN"}}}}`,
	}}
	env.GHClient = newTestGHClient(t, d)

	var res Result
	if err := refreshIssue(context.Background(), env, s, &res); err != nil {
		t.Fatalf("refreshIssue: %v", err)
	}
	if res.Reopened != 1 || res.Updated != 1 {
		t.Errorf("Reopened=%d Updated=%d, want 1/1", res.Reopened, res.Updated)
	}
	if !strings.Contains(d.queries[1], "reopenIssue(input:") || !strings.Contains(d.queries[2], "updateIssue(input:") {
		t.Errorf("调用序应为 Get→Reopen→Update: %v", d.queries)
	}
}

// TestReconcileRefreshBranch 状态：有 Issue 的脚本走判等刷新；错误 → Errors++ 继续。
func TestReconcileRefreshBranch(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		`{"errors":[{"message":"boom"}]}`, // 脚本 a GetIssue 失败
	}}
	env, _ := refreshEnv(t, d)
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{*refreshScript()}}
	res := reconcileIssues(context.Background(), env, r)
	if res.Errors != 1 || res.Active != 1 {
		t.Errorf("Errors=%d Active=%d, want 1/1", res.Errors, res.Active)
	}
	if res.NoIssue != 0 || res.Created != 0 {
		t.Errorf("NoIssue=%d Created=%d, want 0/0（有 Issue 不应回填）", res.NoIssue, res.Created)
	}
}
```

其中 `refreshScript()` 抽自 `refreshEnv` 的脚本字面量（任务 3 的 `newBackfillEnv` 与本任务共用脚本构造时同步抽取，若不便抽取则内联字面量）。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./internal/projector/ -run 'Refresh|ReconcileRefresh' -count=1`
预期：FAIL——`undefined: refreshIssue`。

- [ ] **步骤 3：编写最少实现代码**

`reconcile.go` 的 `reconcileIssues` 中 `if s.Issue == nil { ... }` 块后追加分支：

```go
		} else if env.GHClient != nil {
			if err := refreshIssue(ctx, env, s, &res); err != nil {
				res.Errors++
				continue
			}
		}
```

同文件新增（放在 `backfillIssue` 之后）：

```go
// refreshIssue 判等刷新已有 Issue（I-1 契约：title 与 body 全等才跳过）。
// 线上已关闭且内容有变时先 Reopen 再 Update；成功刷新不 bump UpdatedAt。
func refreshIssue(ctx context.Context, env *Env, s *registry.Script, res *Result) error {
	iss, err := env.GHClient.GetIssue(ctx, s.Issue.Number)
	if err != nil {
		return fmt.Errorf("获取 Issue #%d 失败: %w", s.Issue.Number, err)
	}
	title := fmt.Sprintf("[%s] v%s", s.Name, s.Version)
	body := BuildIssueBody(s, env.PagesBase, env.distSeg())
	if iss.Title == title && iss.Body == body {
		return nil
	}
	if iss.State == "CLOSED" {
		if err := env.GHClient.ReopenIssue(ctx, iss.NodeID); err != nil {
			return fmt.Errorf("重开 Issue #%d 失败: %w", iss.Number, err)
		}
		res.Reopened++
	}
	if err := env.GHClient.UpdateIssue(ctx, iss.NodeID, title, body); err != nil {
		return fmt.Errorf("更新 Issue #%d 失败: %w", iss.Number, err)
	}
	res.Updated++
	return nil
}
```

- [ ] **步骤 4：运行测试验证通过（含既有回归）**

运行：`go test ./internal/projector/ ./internal/commands/ -count=1`
预期：PASS。注意：既有 commands 集成测试（`TestRunProjectTombstone` 等）断言宽松，新增 GetIssue 调用可能让其 `Errors` 变为非零但不断言该值——若出现断言失败，按真实行为修正测试期望，不得放宽实现。

- [ ] **步骤 5：Commit**

```powershell
git add internal/projector/reconcile.go internal/projector/reconcile_test.go; if ($?) { git commit -m "feat(projector): 已关联 Issue 判等刷新与关闭态先重开后更新" }
```

### 任务 5：阶段二 publishDiscussions（账本幂等发帖）+ Project 编排接线

**文件：**
- 创建：`internal/projector/discuss.go`（`selectCategory` + `EnsureDiscussionPost` + `publishDiscussions`）
- 创建：`internal/projector/discuss_test.go`
- 修改：`internal/projector/project.go`（`Project()` 调 `publishDiscussions` 并累加 `Posted/Skipped/Errors`）

- [ ] **步骤 1：编写失败的测试**

创建 `internal/projector/discuss_test.go`（复用同包 `multiRespDoer`/`newTestGHClient`；`categoriesResp`/`repoIDResp`/`createDiscResp` 常量如下）：

```go
package projector

import (
	"context"
	"strings"
	"testing"

	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/registry"
)

const categoriesResp = `{"data":{"repository":{"discussionCategories":{"nodes":[` +
	`{"id":"DC_1","name":"Q&A","slug":"q-a","isDefault":true},` +
	`{"id":"DC_2","name":"一般","slug":"general","isDefault":false}]}}}}`
// repoIDResp 复用 reconcile_test.go 中任务 3 定义（同包），勿重复声明。
const createDiscResp = `{"data":{"createDiscussion":{"discussion":{"id":"D_1","number":7,` +
	`"title":"T","body":"B","url":"https://github.com/o/r/discussions/7",` +
	`"createdAt":"2026-10-10T00:00:00Z"}}}}`

// publishScript 构造带指定账本的脚本（version 为当前版本，ledger 为已有账本）。
func publishScript(id, version string, ledger []registry.DiscussionEntry) registry.Script {
	return registry.Script{
		ID: id, Name: "脚本" + id, Version: version, Type: registry.TypeSelf, Enabled: true,
		Issue:       &registry.IssueRef{Number: 1, NodeID: "I_1", URL: "https://github.com/o/r/issues/1"},
		Discussions: ledger,
	}
}

// TestPublishEmptyLedgerPosts 空账本 → 分类+repoID+发帖三连，追加账本，posted=1。
func TestPublishEmptyLedgerPosts(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{categoriesResp, repoIDResp, createDiscResp}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: newTestGHClient(t, d)}
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		publishScript("a", "1.0.0", nil),
	}}
	posted, skipped, errs := publishDiscussions(context.Background(), env, r)
	if posted != 1 || skipped != 0 || errs != 0 {
		t.Fatalf("posted=%d skipped=%d errs=%d, want 1/0/0", posted, skipped, errs)
	}
	led := r.Scripts[0].Discussions
	if len(led) != 1 || led[0].Version != "1.0.0" || led[0].Number != 7 || led[0].NodeID != "D_1" ||
		led[0].URL != "https://github.com/o/r/discussions/7" || led[0].CreatedAt != "2026-10-10T00:00:00Z" {
		t.Errorf("账本条目错误: %+v", led)
	}
}

// TestPublishSkipsWhenCurrent 末条==当前 → skipped=1，零 API 调用。
func TestPublishSkipsWhenCurrent(t *testing.T) {
	d := &multiRespDoer{t: t, resps: nil} // 任何调用都会 t.Errorf
	env := &Env{Root: "/tmp", RepoOwner: "o", GHClient: newTestGHClient(t, d)}
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		publishScript("a", "1.0.0", []registry.DiscussionEntry{{Version: "1.0.0", Number: 3, NodeID: "D_3"}}),
	}}
	posted, skipped, errs := publishDiscussions(context.Background(), env, r)
	if posted != 0 || skipped != 1 || errs != 0 {
		t.Errorf("posted=%d skipped=%d errs=%d, want 0/1/0", posted, skipped, errs)
	}
}

// TestPublishPostsWhenStale 末条≠当前 → 发布+追加。
func TestPublishPostsWhenStale(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{categoriesResp, repoIDResp, createDiscResp}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: newTestGHClient(t, d)}
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		publishScript("a", "1.0.0", []registry.DiscussionEntry{{Version: "0.9.0", Number: 3, NodeID: "D_3"}}),
	}}
	posted, skipped, errs := publishDiscussions(context.Background(), env, r)
	if posted != 1 || skipped != 0 || errs != 0 {
		t.Errorf("posted=%d skipped=%d errs=%d, want 1/0/0", posted, skipped, errs)
	}
	led := r.Scripts[0].Discussions
	if len(led) != 2 || led[1].Version != "1.0.0" {
		t.Errorf("应追加而非覆盖: %+v", led)
	}
}

// TestPublishFailureNoLedgerAppend 发帖失败 → errs=1、不追加，且继续下一脚本。
func TestPublishFailureNoLedgerAppend(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		categoriesResp, repoIDResp,
		`{"errors":[{"message":"rate limit"}]}`, // 脚本 a 发帖失败
		createDiscResp, // 脚本 b 发帖成功（repoID 已缓存）
	}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: newTestGHClient(t, d)}
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		publishScript("a", "1.0.0", nil),
		publishScript("b", "2.0.0", nil),
	}}
	posted, skipped, errs := publishDiscussions(context.Background(), env, r)
	if posted != 1 || errs != 1 {
		t.Errorf("posted=%d errs=%d, want 1/1", posted, errs)
	}
	if len(r.Scripts[0].Discussions) != 0 {
		t.Errorf("失败脚本不应追加账本: %+v", r.Scripts[0].Discussions)
	}
	if len(r.Scripts[1].Discussions) != 1 {
		t.Errorf("后续脚本应继续处理: %+v", r.Scripts[1].Discussions)
	}
}

// TestSelectCategory 分类选择：q-a > isDefault > 第一个。
func TestSelectCategory(t *testing.T) {
	cases := []struct {
		name string
		cats []github.Category
		want string
	}{
		{"q-a 优先", []github.Category{{NodeID: "X", Slug: "general", IsDefault: true}, {NodeID: "Q", Slug: "q-a"}}, "Q"},
		{"isDefault 次之", []github.Category{{NodeID: "A", Slug: "a"}, {NodeID: "B", Slug: "b", IsDefault: true}}, "B"},
		{"否则第一个", []github.Category{{NodeID: "A", Slug: "a"}, {NodeID: "B", Slug: "b"}}, "A"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := selectCategory(c.cats); got != c.want {
				t.Errorf("selectCategory = %q, want %q", got, c.want)
			}
		})
	}
}

// TestPublishCategoriesError 分类查询失败 → 该轮所有待发脚本 errs++（错误缓存整轮）。
func TestPublishCategoriesError(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{`{"errors":[{"message":"boom"}]}`}}
	env := &Env{Root: "/tmp", RepoOwner: "o", GHClient: newTestGHClient(t, d)}
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		publishScript("a", "1.0.0", nil),
		publishScript("b", "1.0.0", nil),
	}}
	posted, _, errs := publishDiscussions(context.Background(), env, r)
	if posted != 0 || errs != 2 {
		t.Errorf("posted=%d errs=%d, want 0/2", posted, errs)
	}
	if d.calls != 1 {
		t.Errorf("分类查询应整轮只调 1 次: calls=%d", d.calls)
	}
}

// TestPublishSkipsAndNilClient Deleted 跳过、GHClient nil 整阶段跳过（零调用）。
func TestPublishSkipsAndNilClient(t *testing.T) {
	d := &multiRespDoer{t: t, resps: nil}
	env := &Env{Root: "/tmp", RepoOwner: "o", GHClient: newTestGHClient(t, d)}
	del := publishScript("a", "1.0.0", nil)
	del.Deleted = true
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{del}}
	if posted, skipped, errs := publishDiscussions(context.Background(), env, r); posted != 0 || skipped != 0 || errs != 0 {
		t.Errorf("Deleted 应完全跳过: %d/%d/%d", posted, skipped, errs)
	}
	envNo := &Env{Root: "/tmp", RepoOwner: "o", GHClient: nil}
	r2 := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{publishScript("a", "1.0.0", nil)}}
	if posted, _, errs := publishDiscussions(context.Background(), envNo, r2); posted != 0 || errs != 0 {
		t.Errorf("GHClient nil 应零动作: %d/%d", posted, errs)
	}
}

// TestProjectStageOrder 阶段一出错仍进阶段二（Errors 来自阶段一，Posted 来自阶段二）。
func TestProjectStageOrder(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		`{"errors":[{"message":"boom"}]}`, // 阶段一 GetIssue 失败
		categoriesResp, repoIDResp, createDiscResp, // 阶段二发帖
	}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: newTestGHClient(t, d)}
	s := publishScript("a", "1.0.0", nil)
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{s}}
	res, err := Project(context.Background(), env, r)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if res.Errors != 1 || res.Posted != 1 {
		t.Errorf("Errors=%d Posted=%d, want 1/1（阶段一失败不阻断阶段二）", res.Errors, res.Posted)
	}
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./internal/projector/ -run 'Publish|SelectCategory|ProjectStageOrder' -count=1`
预期：FAIL——`undefined: publishDiscussions`、`undefined: selectCategory`（编译错误也算失败）。

- [ ] **步骤 3：编写最少实现代码**

创建 `internal/projector/discuss.go`：

```go
package projector

import (
	"context"
	"fmt"

	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/issuepage"
	"github.com/acg-q/userscript-console/internal/registry"
)

// selectCategory 版本帖分类选择：slug q-a（Q&A）> isDefault > 列表第一个。
func selectCategory(cats []github.Category) string {
	for _, c := range cats {
		if c.Slug == "q-a" {
			return c.NodeID
		}
	}
	for _, c := range cats {
		if c.IsDefault {
			return c.NodeID
		}
	}
	return cats[0].NodeID
}

// EnsureDiscussionPost 为脚本发布版本帖并追加账本（调用方负责幂等判定）。
// 失败返回 error 且不追加账本，供下轮重试。
func EnsureDiscussionPost(ctx context.Context, env *Env, s *registry.Script, categoryID string) error {
	title := issuepage.DiscussionTitle(*s, s.Version)
	body := issuepage.DiscussionBody(*s, s.Version)
	d, err := env.GHClient.CreateDiscussion(ctx, categoryID, title, body)
	if err != nil {
		return fmt.Errorf("发布版本帖失败: %w", err)
	}
	s.Discussions = append(s.Discussions, registry.DiscussionEntry{
		Version:   s.Version,
		Number:    d.Number,
		NodeID:    d.NodeID,
		URL:       d.URL,
		CreatedAt: d.CreatedAt,
	})
	return nil
}

// publishDiscussions 阶段二：账本幂等发帖。判等只看账本末条（账本是唯一事实源）；
// 分类懒加载整轮一次（错误也缓存）；失败不阻断后续脚本。
func publishDiscussions(ctx context.Context, env *Env, r *registry.Registry) (posted, skipped, errs int) {
	if env.GHClient == nil {
		return 0, 0, 0
	}
	var catID string
	var catErr error
	catLoaded := false
	for i := range r.Scripts {
		s := &r.Scripts[i]
		if s.Deleted {
			continue
		}
		if n := len(s.Discussions); n > 0 && s.Discussions[n-1].Version == s.Version {
			skipped++
			continue
		}
		if !catLoaded {
			cats, err := env.GHClient.DiscussionCategories(ctx)
			if err != nil {
				catErr = fmt.Errorf("获取分类失败: %w", err)
			} else {
				catID = selectCategory(cats)
			}
			catLoaded = true
		}
		if catErr != nil {
			errs++
			continue
		}
		if err := EnsureDiscussionPost(ctx, env, s, catID); err != nil {
			errs++
			continue
		}
		posted++
	}
	return posted, skipped, errs
}
```

`project.go` 的 `Project()` 改为：

```go
// Project 对账投影：阶段一 Issue 对账（含判等刷新），阶段二账本幂等发帖。
func Project(ctx context.Context, env *Env, r *registry.Registry) (Result, error) {
	res := reconcileIssues(ctx, env, r)
	posted, skipped, errs := publishDiscussions(ctx, env, r)
	res.Posted, res.Skipped = posted, skipped
	res.Errors += errs
	return res, nil
}
```

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./internal/projector/ ./internal/commands/ ./internal/issuepage/ -count=1`
预期：PASS。

- [ ] **步骤 5：Commit**

```powershell
git add internal/projector/discuss.go internal/projector/discuss_test.go internal/projector/project.go; if ($?) { git commit -m "feat(projector): 版本帖账本幂等发布与分类选择接线 Project 两阶段" }
```

### 任务 6：runProject 回帖扩计数 + changed 四项或 + 文档同步

**文件：**
- 修改：`internal/commands/project.go:60-79`（回帖格式与 `reply` changed 判定）
- 测试：`internal/commands/project_test.go`（`routeDoer` 路由器 + 回帖/changed 集成用例）
- 修改：`docs/commands/project.md`（回帖示例与幂等语义）

- [ ] **步骤 1：编写失败的测试**

`project_test.go` 追加路由 doer（按查询子串 + 可选 variables.number 路由，未命中即 `t.Errorf`，兼做零意外调用断言）：

```go
// routeRule 单条路由：qContains 匹配查询文档子串；varNumber>0 时校验 variables.number。
type routeRule struct {
	qContains string
	varNumber int
	resp      string
}

// routeDoer 按查询内容路由响应（project 集成测试需要区分 REPO_QUERY 的 GetIssue/repoID 两消费者）。
type routeDoer struct {
	t     *testing.T
	rules []routeRule
}

func (d *routeDoer) Do(req *http.Request) (*http.Response, error) {
	b, _ := io.ReadAll(req.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	q, _ := m["query"].(string)
	vars, _ := m["variables"].(map[string]any)
	for _, r := range d.rules {
		if !strings.Contains(q, r.qContains) {
			continue
		}
		if r.varNumber > 0 {
			n, _ := vars["number"].(float64)
			if int(n) != r.varNumber {
				continue
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(r.resp))}, nil
	}
	d.t.Errorf("routeDoer 未命中: query=%q vars=%v", q, vars)
	return nil, fmt.Errorf("routeDoer 未命中")
}
```

新增两个集成测试（沿用 `buildTestEnvWithSource` + loadReg/saveReg 改脚本的既有模式）：

```go
// ghRoutes 构造 project 常用路由集：
// getIssue(n) → REPO_QUERY issue；update/reopen/categories/repoID/createDiscussion 由调用方追加。
func getIssueRule(n int, resp string) routeRule {
	return routeRule{qContains: "issue(number: $number)", varNumber: n, resp: resp}
}

// issueCombinedResp 构造 REPO_QUERY combined 响应。
func issueCombinedResp(t *testing.T, number int, title, body, state string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{
		"id": "R_1", "nameWithOwner": "o/r",
		"issue": map[string]any{
			"id": "I_5", "number": number, "title": title, "body": body, "state": state,
			"createdAt": "2026-01-01T00:00:00Z", "updatedAt": "2026-01-01T00:00:00Z",
			"author":    map[string]any{"login": "bot"},
			"comments":  map[string]any{"totalCount": 0},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestRunProjectRefreshReopenPublish 有 Issue 且线上关闭+内容过期 → Reopen+Update；
// 账本空 → 发帖；回帖含新计数行；Changed=true。
func TestRunProjectRefreshReopenPublish(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	env.RepoOwner = "o"
	env.RepoName = "o/r"
	env.PagesBase = "https://test.github.io/repo"

	reg, _ := loadReg(env)
	var target *registry.Script
	for i := range reg.Scripts {
		if reg.Scripts[i].ID == "self01" {
			reg.Scripts[i].Issue = &registry.IssueRef{Number: 5, NodeID: "I_5", URL: "https://github.com/o/r/issues/5"}
			reg.Scripts[i].Discussions = nil
			target = &reg.Scripts[i]
		} else if !reg.Scripts[i].Deleted {
			// 其余活跃脚本给足 Issue 与当前账本，隔离出 self01 单变量
			reg.Scripts[i].Issue = &registry.IssueRef{Number: 6, NodeID: "I_6", URL: "https://github.com/o/r/issues/6"}
			reg.Scripts[i].Discussions = []registry.DiscussionEntry{{Version: reg.Scripts[i].Version, Number: 1, NodeID: "D_1"}}
		}
	}
	staleTitle := "[测试脚本] v0.0.1"
	staleBody := "过期正文"
	if err := saveReg(env, reg); err != nil {
		t.Fatalf("saveReg: %v", err)
	}
	_ = target

	d := &routeDoer{t: t, rules: []routeRule{
		getIssueRule(5, issueCombinedResp(t, 5, staleTitle, staleBody, "CLOSED")),
		{qContains: "reopenIssue(input:", resp: `{"data":{"reopenIssue":{"issue":{"id":"I_5","number":5,"state":"OPEN"}}}}`},
		{qContains: "updateIssue(input:", resp: `{"data":{"updateIssue":{"issue":{"id":"I_5","number":5,"state":"OPEN"}}}}`},
		{qContains: "discussionCategories", resp: `{"data":{"repository":{"discussionCategories":{"nodes":[{"id":"DC_1","name":"Q&A","slug":"q-a","isDefault":true}]}}}}`},
		{qContains: "issue(number: $number)", varNumber: 1, resp: `{"data":{"repository":{"id":"R_1","nameWithOwner":"o/r","issue":null}}}`},
		{qContains: "createDiscussion(input:", resp: `{"data":{"createDiscussion":{"discussion":{"id":"D_9","number":9,"title":"T","body":"B","url":"https://github.com/o/r/discussions/9","createdAt":"2026-10-10T00:00:00Z"}}}}`},
	}}
	ghc, err := github.New("tok", "o/r", github.WithDoer(d), github.WithRetry(0))
	if err != nil {
		t.Fatalf("GHClient: %v", err)
	}
	env.GHClient = ghc

	res, err := Execute("project", env, "", nil)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if !res.Changed {
		t.Error("有更新+发帖时 Changed 应为 true")
	}
	for _, want := range []string{"更新 Issue: 1", "重开 Issue: 1", "发布版本帖: 1", "错误: 0"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("回帖缺 %q:\n%s", want, res.Text)
		}
	}
	// 账本已追加并落盘
	reg2, _ := loadReg(env)
	for _, s := range reg2.Scripts {
		if s.ID == "self01" {
			if len(s.Discussions) != 1 || s.Discussions[0].Number != 9 {
				t.Errorf("self01 账本应追加 9 号帖: %+v", s.Discussions)
			}
		}
	}
}

// TestRunProjectIdempotentNoChange 判等相等+账本当前 → 零写调用、Changed=false、回帖含跳过计数。
func TestRunProjectIdempotentNoChange(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	env.RepoOwner = "o"
	env.RepoName = "o/r"
	env.PagesBase = "https://test.github.io/repo"

	reg, _ := loadReg(env)
	var rules []routeRule
	for i := range reg.Scripts {
		s := &reg.Scripts[i]
		if s.Deleted {
			continue
		}
		num := 5 + i
		s.Issue = &registry.IssueRef{Number: num, NodeID: fmt.Sprintf("I_%d", num), URL: fmt.Sprintf("https://github.com/o/r/issues/%d", num)}
		title := fmt.Sprintf("[%s] v%s", s.Name, s.Version)
		body := projector.BuildIssueBody(s, env.PagesBase, env.layout().DistSeg())
		rules = append(rules, getIssueRule(num, issueCombinedResp(t, num, title, body, "OPEN")))
		s.Discussions = []registry.DiscussionEntry{{Version: s.Version, Number: 1, NodeID: "D_1"}}
	}
	if err := saveReg(env, reg); err != nil {
		t.Fatalf("saveReg: %v", err)
	}

	d := &routeDoer{t: t, rules: rules} // 任何写路由缺失 → 未命中 → t.Errorf
	ghc, err := github.New("tok", "o/r", github.WithDoer(d), github.WithRetry(0))
	if err != nil {
		t.Fatalf("GHClient: %v", err)
	}
	env.GHClient = ghc

	res, err := Execute("project", env, "", nil)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if res.Changed {
		t.Error("全等+账本当前时 Changed 应为 false")
	}
	if !strings.Contains(res.Text, "更新 Issue: 0") || !strings.Contains(res.Text, "发布版本帖: 0") {
		t.Errorf("回帖计数应全 0:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "跳过: ") {
		t.Errorf("跳过数>0 时应回帖附跳过计数:\n%s", res.Text)
	}
}
```

测试需 import `projector`（已有 `github`/`registry`/`io`/`json`/`fmt`/`strings`/`http`）。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./internal/commands/ -run 'RunProjectRefreshReopenPublish|RunProjectIdempotentNoChange' -count=1`
预期：FAIL——回帖缺「重开 Issue」「发布版本帖」行；幂等用例可能因 Changed 旧判定（两项或）失败或跳过行缺失。

- [ ] **步骤 3：编写最少实现代码**

`internal/commands/project.go` 回帖段（54-69 行）改为：

```go
	if env.GHClient != nil {
		msg += fmt.Sprintf("\n📊 本次操作：\n"+
			"- 创建 Issue: %d\n"+
			"- 更新 Issue: %d\n"+
			"- 重开 Issue: %d\n"+
			"- 发布版本帖: %d",
			res.Created, res.Updated, res.Reopened, res.Posted)
		if res.Skipped > 0 {
			msg += fmt.Sprintf("（跳过: %d）", res.Skipped)
		}
		msg += fmt.Sprintf("\n- 错误: %d\n", res.Errors)
	} else {
		msg += "\n⚠️ GitHub API 未配置，仅输出统计"
	}

	return reply(res.Created > 0 || res.Updated > 0 || res.Reopened > 0 || res.Posted > 0, "%s", msg)
```

`docs/commands/project.md`：回帖示例补「重开 Issue」「发布版本帖」行（含跳过示例）；幂等语义节补两条——已有 Issue 按标题+正文判等，一致不动；版本帖以 `discussions` 账本末条对当前版本判等，一致跳过（附跳过计数）。

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./internal/commands/ ./internal/projector/ -count=1`
预期：PASS。

- [ ] **步骤 5：Commit**

```powershell
git add internal/commands/project.go internal/commands/project_test.go docs/commands/project.md; if ($?) { git commit -m "feat(commands): project 回帖补重开/版本帖计数并扩 changed 四项或" }
```

### 任务 7：全量门禁验证（test.yml 基准全绿）

**文件：** 无新文件；若 lint/覆盖率/快照失败则修对应源码或测试（每类修复单独 commit）。

- [ ] **步骤 1：按 test.yml 顺序跑门禁**

逐条执行（PowerShell 5.1，禁止 `&&`；任何一步失败先修复再重跑）：

```powershell
gofmt -l .
go vet ./...
$(go env GOPATH)\bin\golangci-lint.exe run
go test ./... -covermode=atomic -coverprofile=cover.out
go tool cover -func=cover.out | Select-Object -Last 1   # 总覆盖率必须 ≥90%
go run ./cmd/usm snapshot check
go build ./...
```

预期：gofmt 输出空；lint 0 issue；总覆盖率 **≥90%**；`snapshot check` 无漂移（本计划不改 `issuepage`/`BuildIssueBody`，若漂移必须查明原因，禁止无脑 `snapshot update`）。

- [ ] **步骤 2：修复发现的问题**

- 覆盖率不足：优先补任务 3/4/5 新分支的边角用例（如 `publishDiscussions` 的 `Skipped` 累加、`refreshIssue` 的 `GetIssue` 错误路径已在计划内）。
- 每类修复一个 commit，例如 `test(projector): 补充判等刷新边角用例`、`fix(lint): ...`。

- [ ] **步骤 3：重跑全部门禁**

运行：步骤 1 的全部命令。
预期：全绿。

### 任务 8：发版 v1.4.0（tag → Release CI）

**文件：** 无源码文件；产出 tag 与 release。

- [ ] **步骤 1：确认工作区干净且在 master 最新**

```powershell
git status --short
git log --oneline -3
```
预期：`status` 空；最近提交为本计划各任务 commit。

- [ ] **步骤 2：打轻量 tag 并推送**

```powershell
git tag v1.4.0
git push origin master
git push origin v1.4.0
```

- [ ] **步骤 3：等待并核查 Release CI**

取 PAT：`@("protocol=https","host=github.com","") | git credential fill`（解析 `password=` 行），然后轮询：

```powershell
$r = Invoke-RestMethod -Headers @{Authorization="Bearer $PAT"} "https://api.github.com/repos/ACG-Q/userscript-console/actions/runs?event=push&per_page=3"
$r.workflow_runs | Select-Object name, status, conclusion, html_url
```

预期：release.yml（test→build→publish→移动 `v1` tag→回填 `internal/buildinfo/VERSION`）全部 conclusion=success。若失败，修复后**不得 amend**，新增 fix commit 并重新打 `v1.4.1`（计划外版本号需回报用户）。

- [ ] **步骤 4：确认 release 产物**

```powershell
$rel = Invoke-RestMethod -Headers @{Authorization="Bearer $PAT"} "https://api.github.com/repos/ACG-Q/userscript-console/releases/tags/v1.4.0"
$rel.assets | Select-Object name, size
```

预期：资产存在且 size > 0。

### 任务 9：内容仓 workflow Commit 条件修复 + 真实验收

**文件：**
- 修改：`C:\Users\LiuJi\Desktop\project-06\userscripts\.github\workflows\issue-commands.yml:50`（Commit 步骤 `if`）

- [ ] **步骤 1：修 workflow Commit 条件**

`issue-commands.yml:50`：

```yaml
if: ${{ !cancelled() && steps.cmd.outputs.authorized == 'true' && (steps.cmd.outputs.changed == 'true' || steps.proj.outputs.changed == 'true') && steps.cmd.outcome == 'success' }}
```

（对旧版二进制兼容：旧版无 `Posted` 等输出，`proj.outputs.changed` 恒空/旧值，条件退化为原行为。）

- [ ] **步骤 2：提交并推送内容仓**

```powershell
git -C C:\Users\LiuJi\Desktop\project-06\userscripts status --short
git -C C:\Users\LiuJi\Desktop\project-06\userscripts add .github/workflows/issue-commands.yml
git -C C:\Users\LiuJi\Desktop\project-06\userscripts commit -m "fix(workflow): proj 步骤 changed 也触发 registry 提交防重复发帖"
git -C C:\Users\LiuJi\Desktop\project-06\userscripts push origin master
```

- [ ] **步骤 3：真实触发验收（第一轮：有动作）**

用 PAT 在工具仓 Issue #1 发评论触发（内容仓 workflow 监听其 repo 的 issue_comment，实际目标为**内容仓** issue——以内容仓 `issue-commands.yml` 的 `on:` 与既有触发 issue 为准；若监听 issue 号不同，对相应 issue 发 `/project`）：

```powershell
$b = @{body="/project"} | ConvertTo-Json
Invoke-RestMethod -Headers @{Authorization="Bearer $PAT"; "Accept"="application/vnd.github+json"} -Method Post -Body $b -ContentType "application/json" "https://api.github.com/repos/ACG-Q/userscripts/issues/1/comments"
```

轮询 actions run 后核验（REST 读内容仓 registry 所在 commit、issues、discussions）：
- issue #2（既有 Issue）标题/正文被判等刷新（有变化才更新）；
- 当前版本 Discussion 创建成功（如 v1.0.18，以内容仓实际版本为准）；
- 内容仓出现 registry.json 的 commit（`proj.changed=true` 或 `cmd.changed=true` 触发）且 deploy 成功；
- 回帖统计含「重开 Issue」「发布版本帖」行。

- [ ] **步骤 4：真实触发验收（第二轮：全幂等）**

再次发 `/project` 评论，核验：
- 回帖「创建/更新/重开/发布版本帖」全 0，附「跳过: N」；
- `Changed=false` → workflow **不产生** registry commit；
- 无重复 Discussion（discussions 账本未变）。

- [ ] **步骤 5：收尾核查**

```powershell
git -C C:\Users\LiuJi\Desktop\project-06\userscript-console status --short
git -C C:\Users\LiuJi\Desktop\project-06\userscripts status --short
```
预期：两仓工作区干净。

---

## 计划完成定义

- 任务 1–9 全部步骤复选框勾选。
- 工具仓：门禁全绿 + tag v1.4.0 发版成功。
- 内容仓：workflow 修复已推送；两轮真实 `/project` 验收通过（有动作轮 + 全幂等轮）。
- 无 BREAKING CHANGE 遗留说明缺失；`EnsureIssue` 移除已在 commit body 注明。
