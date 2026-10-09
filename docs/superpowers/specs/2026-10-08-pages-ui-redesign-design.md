# 站点 UI 仿照 projec-02 重设计（设计文档）

日期：2026-10-08
状态：已获用户批准（四节逐节确认 + 数据源清单逐项确认，§2 依确认订正为 Discussions 数据源）
范围仓：userscript-console（主）+ userscripts（发版升钉）

## 背景与目标

当前 Go 站点模板为简易内联样式，与 Python 原版（projec-02）的完整 UI 存在代差。用户要求：**完全仿照 projec-02 的样式**重做站点页面，并在完成后本地验收、再发版上线。

- 样式基准：`C:\Users\LiuJi\Desktop\projec-02`（`pages_assets.py` 的 6 段资产 + `build_pages.py` 的页面组装）
- 参考对比：`C:\Users\LiuJi\Desktop\project-06\ui-compare\`（左=当前 Go，右=projec-02 目标）
- 验收方式：本地渲染新页面 → 刷新 ui-compare 供用户过目 → 点头后发 v1.1.5 + 升钉上线

## 方案选型（已定）

**方案 1：共享模板片段 + 逐页内联。** 资产搬成 Go 模板 `{{define}}` 共享片段，各页按 Python 同样组合内联输出。产物每页自包含，snapshot 清单、`assemble_site`、部署管线零改动。备选方案 2（独立资产文件）与方案 3（embed 注入）已否决：前者偏离产物同构、改动面大，后者引入不必要的机制复杂度。

## §1 范围与页面结构

| 模板 | 改造后骨架 |
|---|---|
| `index.tmpl` | `.frame` > `nav` > `hero`（H1+副标题+3 统计卡）> `main`（脚本列表标题 + 筛选 chips + 卡片列表 + 加载更多）> `foot` |
| `detail.tmpl` | `.frame` > `nav` > `main`（`detail-card`：标题+版本 pills+元信息 → `d-doc` Markdown 正文 → `d-disc` 评论折叠区 → 版本表格）> `foot` |
| `commands.tmpl` | `.frame` > `nav` > `main`（归档组列表 + 分页器，新卡片样式）> `foot` |
| `commands-index.tmpl` | **不动**（projec-02 同为 meta-refresh 跳转页，批次 2 已对齐） |
| `docs.tmpl`（新增） | **文档站内页**：构建时把 `docs/` **仅顶层** `*.md` 逐个转 HTML（`docs/index.md` → `site/docs/index.html`，`SPEC-DATA-MODEL.md` → `site/docs/spec-data-model.html`，kebab 命名），shell-only（无 extra_js），页面内列全部文档目录；导航「文档」链接指向 `docs/index.html`（按页面深度推 `../` 前缀） |
| `assets.tmpl`（新增） | `{{define}}` 资产：`token-css`、`component-css`、`filter-js`、`list-js`、`disc-js`，外加 `page-shell`（统一外壳：`data-theme="github-light"` 默认值、防闪烁主题 bootstrap 脚本、`nav`、`foot`、归档链接按 `root_href` 推导前缀——对应 Python `page()`，3646B） |

- 资产逐字取自 `pages_assets.py` 常量（TOKEN_CSS 2208B / COMPONENT_CSS 21671B / FILTER_JS 638B / LIST_JS 3461B / DISC_JS 4010B）与 `build_pages.py` 的 `page()` 外壳及主题初始化段，不重写、不"顺手优化"。
- 各页资产组合照抄 `build_pages.py`：首页=shell+FILTER+LIST；详情=shell+LIST+DISC；命令页=**shell only**（`page(title, body, root_href=…)` 无 `extra_js`）。
- **品牌文案**：结构与文案以 Python 为基准（含 H1 副标题的 `/add`、`/up` 说明），品牌名沿用 Go 现状「脚本控制台 / Userscript Console」——验收时如需换成 Python 的「油猴脚本管理器」一句话即可改。
- theme rail 按钮与侧滑抽屉标记以 `projec-02/dist` 产物为准照抄（`page()` 外壳/资产段中来）。
- projec-02 零外部资源（无 css/js/font/图片文件），页面外链均为内容链接——同样保持。
- `design-demos.html` 为开发工具页，不纳入站点。

## §2 数据获取（用户指令：没有数据就获取）

**不新建管道，扩展现有 `siteBuilder.fetchData`：**

1. `pages.Data.IssueStats` 从 `map[string]int`（nodeID→评论数）改为结构化 `map[string]IssueStat`（`IssueStat{Comments int; Closed bool}`）——单一数据源，同步修正所有读取点。
2. GraphQL 查询已 SELECT `state` 与 `comments { totalCount }`（queries.go），**查询字符串不改**，仅把 `state` 从解析层透传进 `Data`。
3. 首页 hero 统计卡：`评论回复` = 全部 issue 评论数之和；`已解答` = state==CLOSED 的脚本数；构建时计算。
4. 卡片徽章：`N 回复`、`已解答`（对应 Python `reply_count`/`is_closed` badge，build_pages.py:254-268）用同一份数据渲染。
5. **降级分支保留**（与 Python `degraded` 一致）：无 `GITHUB_TOKEN` 或拉取失败 → hero 显示 `—`、徽章不显示、告警入 `build-warnings.txt`。线上部署带 token 出真实数字；本地 `set GITHUB_TOKEN=…` 可看真数。
6. 讨论区 `d-disc` 数据源为现有 `Data.DiscussionComments`（已在拉取），直接用。

## §3 交互与行为保持

- **JS 分配**：首页=shell+filter+list；详情=shell+list+disc；命令页=shell only（照 Python `page()`/`extra_js` 组装）。
- **主题系统**：`data-theme` + `localStorage['asm-theme']` 持久化 + 防闪烁 bootstrap + 右下角 theme rail + 侧滑抽屉（github-light 默认 / terminal-dark / vivid-purple），初始化逻辑照搬 `page()` 外壳与 THEME 段。
- **批次 2 修复落点（测试逐条重新断言，测试名不变）**：
  - I7④ 初始隐藏 → 新 `list-js` 初始化（Python LIST_JS 本身只渲染前 batch 条）+ 元素缺省不报错守卫
  - I7①③ 全局分页/相对链接 → 服务端逻辑不动，仅换分页器样式类
  - I7② meta refresh → `commands-index.tmpl` 不动
  - I10 null 守卫 → 搬运 JS 自带元素判空，重新断言
  - I8/I9 消毒与转义 → `pages.go` 渲染管线不动（`RenderMarkdown`/`clipText`/`template.HTML` 不碰）
  - I11 快照守护 → 快照清单不变
- **相对链接规则不变**：子目录页 `../` 前缀；`TestBuildLinksAreRelative` 更新到新标记后继续把关。
- **I7 新落点提醒**：导航链接（首页→归档、详情→归档/首页）改由 `nav`/`foot` 呈现，原位置断言需改到新位置。

## §4 测试与验收

**测试（红→绿，先测后改）：**
1. 重写既有 DOM 断言到新选择器（`.card`、`loadMoreBtn`、`badge-*`、hero、导航位置等），语义不变。
2. 批次 2 行为回归：I7④/I10/I7①/I7②/I11 在新标记下逐条重断言。
3. 新增：stats 求和与降级单测；`state` 透传单测（沿用 `fakeDoer` 假客户端模式）；主题 rail/`data-theme` 默认值断言。
4. `usm snapshot update` → 人工过目 diff（仅样式结构变化，链接与内容不变）→ 提交。

**门禁**：gofmt / go vet / go test 覆盖 ≥90% / snapshot check / go build + golangci-lint 0 issues，全绿才提交。

**验收与发版：**
1. 本地 `usm build` 渲染 → 重新生成 `ui-compare` 对比文件 → 用户过目、提意见、改到点头。
2. 点头后：工具仓 tag `v1.1.5` → Release success → 取 linux-amd64 sha256 → 内容仓 7 处 uses SHA + `binary-version: '1.1.5'` + `binary-sha256` → 两仓 CI 全绿。

## 非目标 / 越界项

- 不改构建产物结构（无独立资产文件、无资产目录）。
- 不新建 GitHub 数据管道（复用 `fetchData`，仅补 state 透传）。
- `design-demos.html` 不进站点。
- 批次 3（8 Minor）/ 批次 4（12 F 差异）计划另行编写，不混入本设计。

## 风险与对策

- **快照 diff 巨大**（每页 ~25KB CSS 变化）→ 人工过目时只核对结构与链接，不逐行读 CSS。
- **搬运 JS 与 Go 数据不匹配**（如 JS 读取 `reply_count` 的卡片数据字段）→ `scripts.json`/卡片 HTML 携带 Python 同名 data 属性，联调时对照 `pages_assets.py` 校准。
- **覆盖率先降后升**（模板分支增多）→ 新增降级/透传/主题测试补齐，保持 ≥90%。
