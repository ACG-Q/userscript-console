# 版本帖链接回填与标题格式 实现计划

> **面向 AI 代理的工作者：** 用户偏好内联执行（executing-plans，勿用子代理）。步骤使用复选框（`- [ ]`）跟踪进度。

**目标：** Issue changelog 表格带版本帖链接（发帖后当轮回写）；新帖标题 `[版本号] 脚本名称 版本讨论`。

**架构：** `issuepage.DiscussionTitle` 改格式；`projector.BuildIssueBody` changelog 扩 4 列并按账本匹配帖子链接；`publishDiscussions` 发帖成功后用新 body 回写 Issue（失败不计错，下轮判等自愈）。

**技术栈：** Go 标准库 + 既有 httptest 假 GitHub；TDD；门禁 gofmt/vet/lint/test/≥90%/snapshot/build。

**依据规格：** `docs/superpowers/specs/2026-10-11-discussion-link-title-design.md`

---

### 任务 1：标题格式 `[版本号] 脚本名称 版本讨论`

**文件：**
- 修改：`internal/issuepage/issuepage.go:160-163`
- 测试：`internal/issuepage/issuepage_test.go:209-227`

- [ ] **步骤 1：改测试断言（失败）**

`issuepage_test.go:212` 改为：

```go
	if title != "[1.2.3] 示例脚本 版本讨论" {
		t.Errorf("标题 = %q", title)
	}
```

- [ ] **步骤 2：运行确认失败**

运行：`go test ./internal/issuepage/ -run TestDiscussion -count=1`
预期：FAIL，`标题 = "示例脚本 · v1.2.3 版本讨论"`

- [ ] **步骤 3：改实现**

`issuepage.go:160-163`：

```go
// DiscussionTitle 版本帖标题：[版本号] 脚本名称 版本讨论。
func DiscussionTitle(s registry.Script, version string) string {
	return fmt.Sprintf("[%s] %s 版本讨论", version, s.Name)
}
```

- [ ] **步骤 4：运行确认通过**

运行：`go test ./internal/issuepage/ -count=1`
预期：PASS

- [ ] **步骤 5：Commit**

```bash
git add internal/issuepage/issuepage.go internal/issuepage/issuepage_test.go
git commit -m "feat(issuepage): 版本帖标题改为 [版本号] 脚本名称 版本讨论"
```

---

### 任务 2：BuildIssueBody changelog 扩 4 列（帖子列）

**文件：**
- 修改：`internal/projector/project.go:97-100`
- 测试：`internal/projector/project_test.go`（TestBuildIssueBody 之后新增）
- 快照：`tests/snapshot/issue/*-body.md` 由 `snapshot update` 再生成

- [ ] **步骤 1：新增失败测试**

`project_test.go` 在 `TestBuildIssueBodySynced` 后追加：

```go
// TestBuildIssueBodyDiscussionLinks 帖子列按账本匹配；未命中与无账本填 —（spec §2）。
func TestBuildIssueBodyDiscussionLinks(t *testing.T) {
	s := &registry.Script{
		ID: "abc", Name: "MyScript", Version: "2.0.0", Type: registry.TypeSelf, Enabled: true,
		Changelog: []registry.ChangelogEntry{
			{Version: "1.0.0", Date: "2026-10-06", Note: "初始同步"},
			{Version: "2.0.0", Date: "2026-10-12", Note: "修复"},
		},
		Discussions: []registry.DiscussionEntry{{
			Version: "1.0.0", Number: 3, NodeID: "D_3", URL: "https://github.com/o/r/discussions/3",
		}},
	}
	body := BuildIssueBody(s, "", "dist")
	for _, want := range []string{
		"| 版本 | 日期 | 说明 | 帖子 |",
		"| v1.0.0 | 2026-10-06 | 初始同步 | [讨论 #3 →](https://github.com/o/r/discussions/3) |",
		"| v2.0.0 | 2026-10-12 | 修复 | — |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body 缺 %q:\n%s", want, body)
		}
	}
	// 无账本 → 帖子列全 —
	s2 := &registry.Script{ID: "x", Name: "X", Version: "1.0.0", Type: registry.TypeSelf,
		Changelog: []registry.ChangelogEntry{{Version: "1.0.0", Date: "2026-01-01", Note: "n"}}}
	if b := BuildIssueBody(s2, "", "dist"); !strings.Contains(b, "| v1.0.0 | 2026-01-01 | n | — |") {
		t.Errorf("无账本应全 —:\n%s", b)
	}
}
```

- [ ] **步骤 2：运行确认失败**

运行：`go test ./internal/projector/ -run TestBuildIssueBodyDiscussionLinks -count=1`
预期：FAIL，缺 `| 版本 | 日期 | 说明 | 帖子 |`

- [ ] **步骤 3：改实现**

`project.go` changelog 段（97-100 行）改为：

```go
	b.WriteString("\n---\n\n**changelog**:\n\n| 版本 | 日期 | 说明 | 帖子 |\n|---|---|---|---|\n")
	for _, c := range s.Changelog {
		fmt.Fprintf(&b, "| v%s | %s | %s | %s |\n", c.Version, c.Date, c.Note, discussionLinkFor(s, c.Version))
	}
	return b.String()
}

// discussionLinkFor 版本 → 账本帖子链接；无条目返回 —。
func discussionLinkFor(s *registry.Script, version string) string {
	for _, d := range s.Discussions {
		if d.Version == version {
			return fmt.Sprintf("[讨论 #%d →](%s)", d.Number, d.URL)
		}
	}
	return "—"
}
```

- [ ] **步骤 4：运行 projector 全测试**

运行：`go test ./internal/projector/ -count=1`
预期：PASS（reconcile 测试用 BuildIssueBody 派生期望值，自动跟随）

- [ ] **步骤 5：重生成快照并核对**

```powershell
go run ./cmd/usm snapshot update
git diff --stat tests/snapshot
go run ./cmd/usm snapshot check
```
预期：`tests/snapshot/issue/*-body.md` 仅 changelog 表头/行格式变化（+ 帖子列 `—`）；check 通过

- [ ] **步骤 6：Commit**

```bash
git add internal/projector/project.go internal/projector/project_test.go tests/snapshot
git commit -m "feat(projector): Issue changelog 表格扩帖子列并按账本回链"
```

---

### 任务 3：发帖成功后立即回写 Issue

**文件：**
- 修改：`internal/projector/discuss.go:42-80`
- 测试：`internal/projector/discuss_test.go`

- [ ] **步骤 1：新增失败测试**

`discuss_test.go` 追加：

```go
// TestPublishWritesBackIssue 发帖成功 → 用新账本 body 回写 Issue（spec §3）。
func TestPublishWritesBackIssue(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		categoriesResp, repoIDResp, createDiscResp,
		`{"data":{}}`, // updateIssue 回写
	}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: newTestGHClient(t, d)}
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		publishScript("a", "1.0.0", nil),
	}}
	posted, _, errs := publishDiscussions(context.Background(), env, r)
	if posted != 1 || errs != 0 {
		t.Fatalf("posted=%d errs=%d", posted, errs)
	}
	if d.calls != 4 {
		t.Errorf("应 4 次调用（分类/repoID/发帖/回写）: calls=%d", d.calls)
	}
}

// TestPublishWritebackFailureNotCounted 回写失败不计错、不阻塞（spec §3）。
func TestPublishWritebackFailureNotCounted(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		categoriesResp, repoIDResp, createDiscResp,
		`{"errors":[{"message":"server error"}]}`, // 回写失败
		createDiscResp, // 脚本 b 发帖成功（repoID 已缓存）
	}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: newTestGHClient(t, d)}
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		publishScript("a", "1.0.0", nil),
		publishScript("b", "2.0.0", nil),
	}}
	posted, _, errs := publishDiscussions(context.Background(), env, r)
	if posted != 2 || errs != 0 {
		t.Errorf("posted=%d errs=%d, want 2/0（回写失败不计错）", posted, errs)
	}
	if len(r.Scripts[0].Discussions) != 1 {
		t.Errorf("发牌与回写解耦，账本照常: %+v", r.Scripts[0].Discussions)
	}
}
```

注意：`publishScript` 脚本 `Issue.NodeID = "I_1"` 已具备回写条件。

- [ ] **步骤 2：运行确认失败**

运行：`go test ./internal/projector/ -run 'TestPublishWritesBackIssue|TestPublishWritebackFailureNotCounted' -count=1`
预期：TestPublishWritesBackIssue FAIL（calls=3 ≠ 4）；TestPublishWritebackFailureNotCounted FAIL（多出的回写请求会被 multiRespDoer 报多余请求）

- [ ] **步骤 3：改实现**

`discuss.go` `publishDiscussions` 中发帖成功分支改为：

```go
		if err := EnsureDiscussionPost(ctx, env, s, catID); err != nil {
			errs++
			continue
		}
		posted++
		writeBackIssue(ctx, env, s)
```

并在 `publishDiscussions` 后新增：

```go
// writeBackIssue 发帖成功后立即把新账本 body 回写面板 Issue。
// 失败不计错：发帖与账本已落盘，下轮判等刷新自愈（spec §3）。
func writeBackIssue(ctx context.Context, env *Env, s *registry.Script) {
	if s.Issue == nil {
		return
	}
	title := fmt.Sprintf("[%s] v%s", s.Name, s.Version)
	body := BuildIssueBody(s, env.PagesBase, env.distSeg())
	if err := env.GHClient.UpdateIssue(ctx, s.Issue.NodeID, title, body); err != nil {
		return
	}
}
```

- [ ] **步骤 4：运行 projector 全测试**

运行：`go test ./internal/projector/ -count=1`
预期：PASS。若 `TestProjectStageOrder` 失败（阶段二发帖后多一次回写请求而响应耗尽），在其 `resps` 末尾追加 `` `{"data":{}}` ``。

- [ ] **步骤 5：Commit**

```bash
git add internal/projector/discuss.go internal/projector/discuss_test.go
git commit -m "feat(projector): 发帖成功后立即回写 Issue 版本帖链接"
```

---

### 任务 4：文档、门禁与发版 v1.4.2

**文件：**
- 修改：`docs/commands/project.md`

- [ ] **步骤 1：更新文档**

`project.md` 幂等语义条目改为：

```markdown
- **版本帖发牌**：账本末条版本==当前版本 → 跳过（零 API）；否则发帖、追加账本并立即回写 Issue（失败不计错，下轮判等自愈）。标题格式 `[版本号] 脚本名称 版本讨论`；分类选择 q-a（Q&A）> 列表第一个。
```

并在 Issue 模板段落注明 changelog 表格为 4 列（含帖子链接）。

- [ ] **步骤 2：全量门禁**

```powershell
gofmt -l .
go vet ./...
$lint = Join-Path (go env GOPATH) "bin\golangci-lint.exe"; & $lint run
go test ./... -count=1 -covermode=atomic -coverprofile="cover.out"
go tool cover "-func=cover.out" | Select-Object -Last 1
go run ./cmd/usm snapshot check
go build ./...
```
预期：gofmt 无输出、lint 0 issues、全 PASS、总覆盖 ≥90%、快照一致、build 通过

- [ ] **步骤 3：Commit + 发版**

```bash
git add docs/commands/project.md
git commit -m "docs(commands): project 版本帖回写与标题格式"
git push origin master
git tag v1.4.2
git push origin v1.4.2
```

- [ ] **步骤 4：确认 CI success（Release v1.4.2 + Test）**

轮询 `actions runs` 至 conclusion=success（Release 资产齐全；CI 会回填 VERSION commit，属正常）。

---

### 任务 5：内容仓真实验收（两轮）

**环境：** `C:\Users\LiuJi\Desktop\project-06\userscripts`；PAT 取法与先前一致（`git credential fill`）。

- [ ] **步骤 1：第一轮 `/project`（回写生效）**

发评论 → 等 run（约 75 s）→ 期望回帖 `更新 Issue: 1、重开 Issue: 0、发版本帖: 0（跳过: 1）、错误: 0`；`git -C ...userscripts pull` 后 `registry.json` 出现 bot commit（body 变化触发判等刷新），Issue #2 的 changelog 表格含 `[讨论 #3 →](...discussions/3)`。

- [ ] **步骤 2：第二轮 `/project`（全幂等）**

再发 → 期望回帖全 0（含跳过: 1）、无新 commit、Deploy skipped。

- [ ] **步骤 3：汇报验收结果**（回帖截图级摘录 + Issue body 片段）

---

## 计划自检

- 规格覆盖：§1 标题→任务 1；§2 四列表格→任务 2；§3 回写→任务 3；§测试/§发布→任务 4/5。无遗漏。
- 占位符：无（所有步骤含完整代码与命令）。
- 类型一致：`discussionLinkFor`/`writeBackIssue` 在定义任务中给出签名；`publishScript`/`multiRespDoer`/`repoIDResp` 均为测试包既有设施。
