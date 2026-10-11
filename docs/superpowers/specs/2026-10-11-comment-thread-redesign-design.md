# 评论区重设计（GitHub 式头像 + 扁平回复线）· 设计文档

日期：2026-10-11
状态：已定稿（用户对 v1.4.4 卡片式不满意，要求重设计）

## 1. 问题（截图复盘 v1.4.4）

- 回复卡片是一大块白色平板，内部三条回复堆叠成高瘦"表格"，视觉沉重；
- 作者/时间/正文节奏松散，缺少身份锚点（无头像），不像现代讨论区；
- 引用只加了左灰条，`>` 符号仍显示为正文文本；多行正文换行被折叠。

## 2. 方案：GitHub Discussions 式评论线

### 2.1 结构（SSR 与 JS 同构）

```
.cmt（grid: 32px 头像 + 1fr 主区）
├─ .cmt-av.avN   圆形头像（作者首字符，色相按作者名哈希 0-4 五色）
└─ .cmt-main
   ├─ .cmt-head  <b>作者</b> [+ answer-tag] <time>
   ├─ p[.quoted] 正文（white-space: pre-line 保留换行）
   └─ .cmt-replies（扁平：左 2px 竖线缩进，无卡片背景；嵌套同构递归）
```

- 回复区去掉卡片（v1.4.4 方案废弃），改为左竖线 thread 缩进（GitHub 惯例）；
- 回复内头像缩至 24px、正文 13px，形成层级；相邻回复保留极细 hairline；
- 顶层评论间沿用 border-top 分隔。

### 2.2 头像配色

- 五色固定调色板（蓝/绿/紫/玫红/橙，白字，三主题通用）：`#0969da #1a7f37 #8250df #bf3989 #bc4c00`；
- 归属：`sum(作者名各 rune 码点) % 5 → av0..av4`；Go 与 JS 实现同算法保证 SSR/切帖一致；
- `aria-hidden`（纯装饰），首字符取首个 rune（中文安全）。

### 2.3 引用与正文

- 判定后**剥离**每行行首 `> ` / `>` 前缀（`stripQuoteMarkers`），`>` 不再作为正文显示；
- `.cmt p.quoted`：左 3px brand 色条 + `--bg` 底 + 圆角右侧，对齐 docs blockquote 语言；
- `white-space: pre-line`：正文 `\n` 渲染为换行。

### 2.4 payload

- `discPayloadComment` 增加 `quoted,omitempty`（JS 依此打 quoted 类）；body 已在 Go 侧剥离，JS 不再判断 `>`。

## 3. 非目标

- 不接 GitHub 真实头像 URL（后续可做）；不动 badge 口径/回复互动。

## 4. 测试计划

- 头像：SSR 输出 `cmt-av avN` 与首字符（如 ACG-Q → A）；
- 引用剥离：`> 引用内容` 渲染为 `引用内容` 且 html 不含 `> 引`；payload 带 `quoted:true`；
- 回归：`class="quoted"` 计数、cmt-replies、400 截断、HTML 转义。

## 5. 发版

v1.4.5；门禁 → 快照更新 → tag → 内容仓空 commit 部署 → 线上三主题核验。
