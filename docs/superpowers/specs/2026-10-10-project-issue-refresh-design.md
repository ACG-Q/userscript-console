# 设计：project 对账补全 —— Issue 判等刷新 + 版本帖幂等发布

日期：2026-10-10
状态：已批准
规格依据：SPEC-CLI.md §2（行 88/91/92/114/194）、SPEC-ARCH-TEST.md 行 17/23、SPEC-ARCH-TEST.md I-1

## 背景与问题

`usm project`（面板 `/project`）当前实现与 SPEC 存在三处缺口：

1. **已有 Issue 永不刷新**：`projector.go` 仅在 `s.Issue == nil` 时调用 `EnsureIssue`；已关联 Issue 的脚本整条跳过。版本、描述、changelog、grant 变化都不会回写到线上 Issue。
2. **版本帖发布未接线**：SPEC 要求 `internal/projector` 提供 `EnsureDiscussionPost`（`project_issues.py::ensure_discussion_post` 的 Go 对应），账本幂等发布版本 Discussion。`github.CreateDiscussion` / `DiscussionCategories` 已就绪但无生产调用方；registry `discussions` 账本恒为空；站点讨论面板因此永远回退 Issue 面板。
3. **对账缺「回填 tracking / 重开」分支**：SPEC-CLI.md:88 要求幂等对账四分支「创建 / 回填 tracking / 重开+更新 / 墓碑化」，现状只有「创建 / 墓碑」。`ListRepoIssues("OPEN")` 也无法匹配已关闭的 Issue（会导致重复建 Issue）。

附带发现：内容仓 `issue-commands.yml` 的 Commit 步骤只看 `steps.cmd.outputs.changed`，而账本追加发生在 proj 步骤 —— 发帖失败后下轮补发成功时账本可能不被提交，导致重复发帖。

## 决策记录

| 问题 | 决策 |
|---|---|
| 空账本首次行为 | **立即回填**：空账本视为「末条 != 当前版本」，首次即发当前版本帖 |
| Discussion 分类 | **Q&A 优先**：slug `q-a` → `isDefault` 标记 → 列表第一个 |
| workflow Commit 条件 | **修**：加 `|| steps.proj.outputs.changed == 'true'` |
| 已有 Issue 刷新策略 | **判等刷新**（I-1 契约）：`GetIssue` 拉线上 → 本地生成 → 相等跳过，不等才 `UpdateIssue` |
| 验收方式 | **真实触发**：发版后在 Issue #1 评论 `/project` 实跑验证 |
| 架构 | **两阶段拆分**：`reconcileIssues` + `publishDiscussions`，`Project()` 编排 |

## §1 架构与数据流

```
/project (面板) 或 usm project (CLI)
        │
        ▼
internal/commands/project.go  runProject（不变：装载/保存 registry、组回帖）
        │
        ▼
internal/projector/project.go     Project() —— 编排器
        │
        ├─► 阶段一 reconcileIssues(env, r) → Result   [projector/reconcile.go 新文件]
        │      SPEC 四分支：创建 / 回填+重开 / 判等更新+重开 / 墓碑化
        │
        └─► 阶段二 publishDiscussions(env, r) → 累加进 Result  [projector/discuss.go 新文件]
               账本幂等四分支；失败收集进 errors，不阻断

依赖方向不变：projector → github / issuepage / registry（SPEC-ARCH-TEST 表内）
```

- `Project()` 签名不变（`ctx, *Env, *Registry → Result, error`），调用方 `runProject` 零改动。
- 阶段一失败（API 错误）：记入 `Result.Errors`，**继续阶段二**（发帖只依赖 `s.Issue` ref 与标题，不依赖判等结果）。
- 阶段二处理所有 `!Deleted` 脚本；停用（`!Enabled`）照发（停用 ≠ 删除，与对账口径一致）。
- registry 保存仍在 `runProject` 末尾一次完成（`registry.Save` 原子落盘）。

## §2 阶段一 reconcileIssues 四分支

```go
// projector/reconcile.go
func reconcileIssues(ctx, env, r) Result
```

前置：`env.GHClient == nil` → 跳过全部 API 动作，仅统计（保持现有降级行为）。

| 分支 | 条件 | 动作 | Result 计数 |
|---|---|---|---|
| 墓碑化 | `Deleted && Issue != nil` | `TombstoneIssue`（现状不动） | `Deleted++`, `Updated++` |
| 回填 tracking | `!Deleted && Issue == nil` | `ListRepoIssues("ALL")`（由 `"OPEN"` 改 `"ALL"`）按标题前缀 `[Name]` 匹配 → 命中且**已关闭** → `ReopenIssue` + `UpdateIssue` → 写 ref；命中且开着 → `UpdateIssue` → 写 ref；未命中 → `CreateIssue` → 写 ref | `NoIssue++`；`Created++` 或 `Reopened++`+`Updated++` |
| 判等更新+重开 | `!Deleted && Issue != nil` | `GetIssue(number)` 拉线上标题+正文 → 本地 `BuildIssueBody` + 标题 `[Name] v{Version}` → 相等跳过；不等 → 线上 closed 先 `ReopenIssue` 再 `UpdateIssue` | `Updated++` 或零计数 |
| 跳过 | `Deleted && Issue == nil` | 无动作 | `Deleted++` |

要点：

- **判等即 I-1 契约**：`title 相等 && body 相等` 才跳过，任一不等才更新。
- `ListRepoIssues` 在一次 `reconcileIssues` 调用内只拉一次（多个 `Issue == nil` 脚本复用），避免 N+1。
- `GetIssue` 仅 `Issue != nil` 时按需调用（每脚本最多 1 次）。
- 每步 API 错误：`Errors++` 继续下一脚本。
- 新增 `Result.Reopened` 字段。

## §3 阶段二 publishDiscussions 账本幂等四分支

```go
// projector/discuss.go
func publishDiscussions(ctx, env, r) (posted, skipped, errs int)
```

对每个 `!Deleted` 脚本：

| 分支 | 条件 | 动作 |
|---|---|---|
| 跳过 | 账本非空 且 末条 `Version == s.Version` | 无动作，`Skipped++` |
| 发布+追加 | 账本为空，或末条版本 ≠ 当前版本 | 发布流程成功 → `append(DiscussionEntry{Version, Number, NodeID, URL, CreatedAt})` → `Posted++` |
| 失败不阻断 | 发布流程任一步出错 | `Errors++`，**不追加账本**，继续下一脚本（下轮重试 = 失败即重发） |
| 墓碑跳过 | `Deleted` | 无动作 |

发布流程（分类懒加载，整轮只查一次，缓存于局部变量）：

1. 分类未缓存 → `DiscussionCategories(ctx)` → 选择规则：**slug == `q-a`（Q&A）→ `isDefault` 标记 → 列表第一个**；查询失败 → 判本脚本失败
2. `CreateDiscussion(ctx, catID, issuepage.DiscussionTitle(s, s.Version), issuepage.DiscussionBody(s, s.Version))`
3. 成功 → 追加账本（字段来自返回的 `Discussion`）

要点：

- 判等只看账本末条（SPEC 原文），不回查线上 —— 账本是唯一事实源。
- `s.Issue == nil` 时正文 issue 引用显示 `—`（`issuepage.issueRef` 现有行为），不阻断发帖。
- `env.GHClient == nil` → 整阶段跳过，仅统计。

## §4 输出：Result、回帖、JSON

```go
type Result struct {
    Active, Deleted, NoIssue int
    Created, Updated, Errors int
    Reopened  int // 回填/判等时重开了已关闭 Issue
    Posted    int // 本轮发布版本帖数
    Skipped   int // 账本末条==当前，跳过发帖数
}
```

`runProject` 回帖在现有格式上追加（其余不动）：

```
✅ 本次操作：
- 创建 Issue: 0
- 更新 Issue: 1
- 重开 Issue: 0        ← 新增
- 发布版本帖: 1        ← 新增（跳过数 >0 时附加「跳过: N」）
- 错误: 0
```

**changed 判定**（决定 workflow 是否 commit）：`Created > 0 || Updated > 0 || Reopened > 0 || Posted > 0`。账本追加仅发生在 `Posted` 时，必然伴随 registry 变更，语义一致。

`usm project --json`：`{actions, changed, dirty_registry, errors}` 结构不变；`errors` 非空 = 有 GraphQL/网络错误未中断。

降级行为不变：无 `GHClient` → 仅统计 + 回帖尾部「⚠️ GitHub API 未配置，仅输出统计」。

## §5 跨仓 workflow 修复

文件：`userscripts/.github/workflows/issue-commands.yml`

Commit 步骤条件：

```yaml
# 现在
if: ${{ !cancelled() && steps.cmd.outputs.authorized == 'true' && steps.cmd.outputs.changed == 'true' && steps.cmd.outcome == 'success' }}
# 改为
if: ${{ !cancelled() && steps.cmd.outputs.authorized == 'true' && (steps.cmd.outputs.changed == 'true' || steps.proj.outputs.changed == 'true') && steps.cmd.outcome == 'success' }}
```

- 部署派发步骤条件已是 `steps.commit.outcome == 'success'`，自动跟随，无需改。
- 失败回帖步骤无需改（proj 失败已覆盖）。
- `steps.proj.outputs.changed` 来自 action 的 `changed` 输出（`project` 子命令 → `runProject` → §4 扩后判定）；`action.yml` 无需改。
- `proj` 步骤带 `if: steps.cmd.outputs.authorized == 'true'`，未授权时 proj 不跑 → 输出空字符串 → 表达式短路安全。
- `git add registry.json scripts dist archive` 白名单不变（账本在 registry.json 内）。

交付顺序：工具仓先合入发版 → 内容仓 workflow 修复单独 commit（对旧版二进制兼容：旧版 `Posted` 恒 0，条件退化为原行为）。

## §6 测试与验收

测试策略：TDD、表驱动、fake 按方法签名路由（沿用 `projector_test.go` 现有模式）。

阶段一 `reconcile_test.go`（对照 SPEC-CLI.md:92 用例清单）：

- 四分支各一：创建 / 回填 tracking（含已关闭 → Reopen）/ 判等更新（title 或 body 任一不等才 `UpdateIssue`，相等零调用）/ 墓碑
- 复活重开：`Issue != nil` 且线上 closed → `ReopenIssue` 先于 `UpdateIssue`（调用序断言）
- `ListRepoIssues("ALL")` 单次调用多脚本复用（N+1 防回归）
- API 错误 → `Errors++` 不中断
- `GHClient == nil` → 零调用纯统计

阶段二 `discuss_test.go`（账本四分支）：

- 空账本 → 发布+追加（回填）
- 末条 == 当前 → 跳过，零 API 调用
- 末条 ≠ 当前 → 发布+追加（含 changelog note 匹配进正文）
- 发布失败 → 不追加账本、`Errors++`、继续下一脚本
- 分类选择：`q-a` 命中 / 无 `q-a` → 默认 / 都无 → 第一个；分类查询失败 → 该脚本失败
- `Deleted` 跳过、`GHClient == nil` 跳过

编排与输出：`Project` 两阶段顺序、阶段一出错仍进阶段二、`runProject` 回帖含新计数、changed 四项或判定。

验收流程：

1. `go vet` / `go test ./...` / `golangci-lint` / 覆盖率 ≥90% / 快照门禁全绿
2. 打 tag → Release CI → 工具仓发版
3. 内容仓修 workflow Commit 条件（§5，独立 commit）
4. Issue #1 评论 `/project` 真实触发 → 确认：issue #2 判等刷新、v1.0.18 Discussion 创建、账本追加、commit+deploy 链路走通；再跑一次全程 skip（幂等）

## 范围外

- `sync` 命令本身不改（版本不变时 no-op 是正确的幂等行为）。
- 分类 slug 不做环境变量配置（YAGNI）。
- 不改 action.yml 输出结构、不改 deploy-pages.yml。
- 不实现 Discussion 评论/回复、不实现删除版本帖。
