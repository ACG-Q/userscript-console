# 版本帖多层评论（replies）显示设计

日期：2026-10-11
状态：已批准（2026-10-11 用户确认方向 A）
范围：脚本详情页完整显示版本帖嵌套回复（数据链路补 replies，前端零改动）

## 背景

Discussion #3 已有嵌套回复（顶层「测试」下挂「测试2/测试3/测试4」），但脚本详情页只显示 2 条顶层评论，回复全部丢失。

根因在数据层：`DISCUSSION_NODE_QUERY` 只查顶层 `comments`，未查 `replies`。前端其实已就绪——页面 JS `commentEl` 递归渲染 `c.replies`，CSS `.cmt-replies` 缩进样式已备好。

## 目标

- 版本帖下的回复（GitHub schema 单层 `replies`）完整显示在脚本详情页，含缩进嵌套。
- 卡片与详情页 badge 的「N 条回复」= 顶层评论数 + 回复数。
- SSR 无 JS 降级渲染同样带嵌套结构。

## 非目标

- 不做回复的回复（GitHub DiscussionComment schema 无此能力，更深的引用是用户手打 `> 引用` 文本）。
- 不改前端 JS（已消费 `c.replies`）。
- 不做回复独立分页（单条评论回复上限 `first: 100`，版本帖场景够用，YAGNI）。
- 不做 discussion 评论触发命令/回复互动（用户明确排除，方向 B 不在范围）。

## 设计

### 1. 数据层（internal/github）

`DISCUSSION_NODE_QUERY` 的 `comments.nodes` 内嵌：

```graphql
replies(first: 100) { nodes { id author { login } body createdAt } }
```

`gqlComment` 加 `Replies []gqlComment \`json:"replies"\``；`Comment` 加 `Replies []Comment`；`toComment` 递归映射（回复缺 id 同样报错）。`DiscThread`/`DiscussionByNode` 主体不动，自动携带回复。

### 2. 映射链路（cmd/usm/site.go → internal/pages）

- `pages.Comment` 加 `Replies []Comment`。
- `site.go` 映射递归带 `Replies`。
- `discPayloadComment` 加 `Replies []discPayloadComment \`json:"replies,omitempty"\``。因 Go 显式类型转换要求字段类型完全一致，payload 构建由 `discPayloadComment(c)` 转换改为递归构造函数 `newPayloadComment(c cmtView) discPayloadComment`。
- **回复数口径**：卡片 `ReplyCount`（pages.go:643）与详情 `ReplyCount`（pages.go:796）均改为「顶层 + 全部回复数」。

### 3. SSR 降级模板

`cmtView` 加 `Replies []cmtView`；`cmt` 模板自递归：回复包在 `<div class="cmt-replies">` 内逐条嵌套，与 JS 版 DOM 结构一致。构建 `cmtView` 时同样递归（含 `times.Clip`/`RelativeTime` 处理）。

### 4. 安全与上限

- 递归深度：schema 限制一层，实现上按结构自然递归即可（数据不会更深）。
- 回复截断：每条评论 `first: 100`，超出不拉（GraphQL 层截断，无静默丢数据风险——GitHub UI 同样有上限）。
- 正文仍是纯文本，渲染处统一转义（现状不变）。

## 测试

- `internal/github`：嵌套回复解析（含 author 缺省）、空 replies、回复缺 id 报错；`DiscThread` 带回复全量。
- `internal/pages`：payload JSON 含 `replies` 嵌套且 `omitempty`；SSR `cmt` 嵌套输出；卡片/详情 ReplyCount 口径。
- `cmd/usm`：site.go fake `DiscThread` 返回带 Replies，断言 pages.Data 带上。
- 门禁：gofmt/vet/lint/go test/覆盖率 ≥90%/快照（SSR 变化则 `snapshot update`）/build。

## 发布与验收

- patch 版本（v1.4.3），无破坏性变更。
- 内容仓部署后，`39280503d459.html` 显示「测试」下 3 条嵌套回复；badge 回复数含回复。
