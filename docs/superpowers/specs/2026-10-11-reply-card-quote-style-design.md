# 评论嵌套回复卡片样式与引用渲染 · 设计文档

日期：2026-10-11
状态：已定稿（用户反馈"不美观"，选定方案 A + 引用样式）

## 1. 问题

线上详情页嵌套回复区（v1.4.3 上线后）视觉混乱：

- 回复内部横线（border-top）+ 左侧缩进竖线组合出"表格感"，视觉噪点多；
- 双重缩进（margin-left 14px + padding-left 12px）归属感弱；
- 回复的字号/颜色与顶层评论完全一致，无视觉层级降级；
- 正文以 `>` 开头的引用内容（GitHub 引用粘贴）按纯文本整段显示，无引用样式。

## 2. 方案（已获批：方案 A 卡片式回复区 + 引用样式）

### 2.1 回复区卡片化（CSS，`assets.tmpl` component-css）

- `.cmt-replies` 改为卡片容器：`background: var(--bg)`（`.d-disc` 底是 `--bg-subtle`，用 `--bg` 形成嵌套卡片对比）、`border: 1px solid var(--border)`、`border-radius: 8px`、`padding: 2px 12px`；去掉左竖线与双重缩进（margin 归零，仅保留 8px 上间距）。
- 卡内条目 `.cmt-replies .cmt`：去 border-top 首条规则沿用；`font-size` 降为 13px，`b` 降为 12.5px，`time` 11.5px；相邻条目保留极细分隔线（`.cmt + .cmt` 的 border-top），在卡片语境下呈列表分隔而非表格。
- 更深层嵌套 `.cmt-replies .cmt-replies`：退出卡片样式（背景/边框/圆角清空），回归左细竖线缩进（2px `--border` + 10px padding-left），与顶层卡片形成层级区分。

### 2.2 引用样式

- 判定：正文 `strings.TrimSpace` 后以 `>` 开头。
- SSR：`cmtView` 增加 `Quoted bool`（`newCmtView` 计算）；`cmt` 模板 `<p>` 增加 `{{if .Quoted}} class="quoted"{{end}}`。
- JS：`commentEl` 中对 `c.body` trim 后首字符为 `>` 时设置 `p.className = 'quoted'`（SSR 与客户端渲染口径一致）。
- CSS `.cmt p.quoted`：`border-left: 3px solid var(--border-strong); padding-left: 10px; color: var(--text-muted);`（无背景色，两层容器下均主题安全，对齐 `.d-doc blockquote` 语言）。

## 3. 非目标

- 不做回复互动（点赞/回复按钮）；不改 payload 结构（`replies,omitempty` 不变）；不改 badge 口径。

## 4. 测试计划

- 新增：SSR 正文 `>` 前缀 → 输出 `<p class="quoted">`；普通正文 → 无 quoted 类；嵌套回复中引用同样生效。
- 回归：既有嵌套渲染测试（`cmt-replies` 存在）、payload 测试不动。
- 门禁后 `go run ./cmd/usm snapshot update` 刷新基线并提交。

## 5. 发版

v1.4.4；内容仓空 commit 触发 Pages 部署；线上核验回复卡片与引用样式（主题三套抽查）。
