# 评论真实头像 + 悬停用户卡片 · 设计文档

日期：2026-10-11
状态：已定稿（demo `demo/user-card.html` 已获批）

## 1. 需求

- 评论头像从首字母色块升级为 **GitHub 真实头像**（`author.avatarUrl`，加载失败回退首字母）；
- 悬停头像 → 实时拉取 `api.github.com/users/<login>` 渲染用户卡片（demo 已验证：骨架屏/缓存/错误态/翻转定位）。

## 2. 设计

### 2.1 数据链路（真实头像）

- GraphQL：讨论评论查询（`DISCUSSION_NODE_QUERY` 的 replies 与评论列表）`author { login avatarUrl }`；
- `github.Comment` 增 `AvatarURL`（`gqlComment.toComment` 解码）；
- `pages.Comment`/`cmtView`/payload 增 `avatar,omitempty`；`cmd/usm/site.go mapComments` 透传；
- SSR/JS：`.cmt-av` 容器内**始终渲染首字母**，有 avatarUrl 时叠加绝对定位 `<img class="cmt-av-img">`（`loading="lazy"`，`onerror="this.remove()"` 自毁露出首字母）。
- login 说明：`Comment.Author` 本就是 GraphQL `login`，用户卡片直接用 `data-login={{.Author}}`，无需新字段。

### 2.2 用户卡片（纯前端，disc-js 内）

- 触发：头像 `mouseenter` 200ms 延迟；卡片内可停留；移出 180ms 关闭；`scroll` 时重定位；
- 数据：`fetch https://api.github.com/users/<login>`；`sessionStorage` 缓存（key `uc:<login>`，30 分钟 TTL）防 60 次/时限流；
- 内容：真实头像、姓名、@login、bio（2 行截断）、位置/博客、仓库/关注/正在关注三列统计、主页链接按钮（`rel="noopener"`）；
- 状态：请求中 shimmer 骨架屏；**失败静默降级**（不弹卡片，避免限流噪音）；
- 定位：头像下方优先，越界上翻，水平夹取到视口内；`role="tooltip"`；
- 样式：全部走主题 token（三主题适配），阴影同 disc-ver-menu 级别。

### 2.3 安全

- 卡片内容一律 `textContent`/esc 拼接（bio/位置/博客是用户可控文本）；外链仅 `html_url` 与 blog（blog 补 https:// 前缀）；
- 图片 src 来自 GitHub CDN（`avatars.githubusercontent.com`），SSR 模板上下文转义。

## 3. 非目标

- 不做卡片内互动（关注按钮）；不做 Issue 命令评论的头像（命令归档无此需求）。

## 4. 测试计划

- github：`toComment` 解码 avatarUrl（fixture 带字段）；
- pages：SSR 有 avatar → `<img class="cmt-av-img"` + 首字母仍在；无 avatar → 无 img；payload `avatar` omitempty；卡片 CSS/JS 存在性断言（`ucard`、`api.github.com/users`）。

## 5. 发版

v1.4.6；门禁 → 快照 → tag → 内容仓部署 → 线上悬停核验（真实头像 + 卡片）。
