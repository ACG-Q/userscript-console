# userscript-console · 实施计划

> 单位：人日 = 1 人全职 1 天。每个任务带 **ID / 产出 / 验收命令**，可直接派给 AI。
> 前置阅读：同目录 `README.md`。

---

## 阶段 0 —— 同仓准备（在 `userscript-manager` 当前仓内完成，**不需要新仓、可独立回退**）

**目标**：把「4 个散装 Python 入口」收敛成「1 个 CLI 入口 + 1 个 Action 可调用面」，并消掉审查中的中危项。阶段 1 的 `filter-repo` 会直接复制这里的结果。

| ID | 任务 | 产出 | 验收 |
|---|---|---|---|
| C0-1 | 新建 `console.py`：`console.py <subcommand>` 分发到 `manager/project_issues/build_pages/panel_cleanup.main` | 单入口，**不改任何业务逻辑** | `python console.py build` 与 `python build_pages.py` 产物 diff 为空 |
| C0-2 | `issue-commands.yml` 把 `git add .` 收窄为路径白名单 `registry.json scripts dist archive command_result.txt` | 审查项 M5/M9 关闭 | grep workflow 无 `git add .`；workflow 跑通一次真实命令 |
| C0-3 | 把 `command_result.txt` 握手改为「子命令 stdout 即结果」，保留文件作为兼容层 | 结果可被 `$GITHUB_OUTPUT` 直接取用 | `python console.py run-command` stdout 单独取出即可回帖 |
| C0-4 | 拆分 docs 归属：`docs/commands/`、`docs/design.md`、`docs/code-review*` 标记为将迁移至工具仓；内容仓保留面向脚本的文档 | 迁移清单文件 `docs-ownership.md` | 与 `userscripts/SPEC-DATA.md §3` 一致 |
| C0-5 | 在 `tests/test_console.py` 覆盖 4 个子命令的分发与退出码 | +4 用例 | `python -m pytest tests/ -q` 全绿 |

**估计**：1–2 人日　**回退**：单提交，`git revert` 即回。

---

## 阶段 1 —— 建仓与骨架

| ID | 任务 | 产出 | 验收 |
|---|---|---|---|
| C1-1 | 用 `git filter-repo` 从 `userscript-manager` 克隆导出代码路径：`console.py manager.py project_issues.py build_pages.py panel_cleanup.py pages_assets.py userscript_manager/ tests/ tools/ requirements*.txt ruff.toml mypy.ini .coveragerc docs/commands docs/design.md docs/code-review*` | 新仓 `userscript-console`，**保留历史**（`git log -- <file>` 可追溯） | 新仓 `python -m pytest tests/ -q` 全绿；旧仓不受影响（用 `--source` 镜像操作，勿在原仓执行） |
| C1-2 | 新仓 `.gitignore`：排除 `registry.json scripts/ dist/ archive/`（**本仓永不持有数据**） | 防数据误入 | 提交后 `git status` 干净；故意放一个 `registry.json` 会被忽略 |
| C1-3 | `go mod init github.com/acg-q/userscript-console` + 目录骨架（README §6，**建议布局，可按 DR-7 按 Go 惯例调整**） | 可编译空壳 | `go build ./...` |
| C1-4 | 建立 `tests/snapshot/` 目录（新项目自产基线，随实现提交）与 `tests/fixtures/`（先建空 + 说明，D-06 前置） | 快照与 fixtures 就位 | 快照与 fixtures 目录就位、`usm snapshot check` 可跑 |
| C1-5 | CI：`.github/workflows/test.yml`（setup-go → build → vet → lint → test+cover → snapshot → schema 校验） | 门禁 v0 | 推送后 workflow 绿 |
| C1-6 | Python 侧保留为**回退基线**（DR-6：仅作语义参照，不是裁判）：阶段 3 结束前**不删除** Python 代码与测试 | 回退基线 | 见 MIGRATION §3（双轨期约束）、§4（退出条件） |

**估计**：1–2 人日

---

## 阶段 2 —— Action 化（**接口先行**，内部仍用 Python；DR-4）

**目标**：内容仓 5 条 workflow 立刻能 `uses:`，拿到拆仓收益；Go 重构随后在接口内进行。

| ID | 任务 | 产出 | 验收 |
|---|---|---|---|
| C2-1 | 写 `action.yml`（完整骨架见 `SPEC-ACTION.md §2`）：composite，3 步 = setup-python → pip → 调 `console.py <command>` 并解析输出到 `$GITHUB_OUTPUT`（checkout 由**调用方** workflow 执行，见 SPEC-ACTION §3.1） | Action v0 | 本仓 CI 里 `uses: ./` 自测通过（**同仓可自测是不分仓的好处**） |
| C2-2 | 输出封装：`authorized` `changed` `result` `warnings` **四个 outputs**；`authorized=false` 时**不执行**命令只回传 | 门禁可被调用方 skip | 单测覆盖 4 个输出的赋值分支 |
| C2-3 | `registry-schema-version` 输入校验：不匹配 → `exit 1` + 明确错误 | 防跨仓 schema 漂移 | 传 `99` 必须失败且信息含 `registry schema` |
| C2-4 | 打 tag `v0.1.0`，内容仓用 `uses: acg-q/userscript-console@<sha>` 接入**一条** workflow（建议先 `command.yml`） | 首个真实调用 | Issue #1 发一条 `/list`，回帖正常、`changed` 正确 |
| C2-5 | 逐条接入其余 workflow（顺序以 `userscripts/PLAN.md` 阶段 2 为准：`command → deploy → sync → cleanup`；`init-panel` 不接入） | 4 条薄壳 | 见 `userscripts/SPEC-WORKFLOWS.md` 各节验收 |
| C2-6 | 内容仓 workflow 全绿后，回填 `userscripts/PLAN.md` 阶段 2（U2-1…U2-7）的任务状态 | 同步进度 | 两仓计划状态一致 |

**估计**：2–3 人日　**回退**：内容仓 workflow 单条 revert 即回到 Python 内联步骤（Python 路径未删）。

---

## 阶段 3 —— Go 重构（按依赖序，**每个模块行为测试+快照通过才进下一个**）

**总规则**：重构一个模块 → 跑它的行为测试与快照 → `git commit` → 才能进下一个。禁止跨模块大爆炸式改写。
**每完成一个模块**，顺带补齐 `SPEC-ARCH-TEST §7` 中该模块对应**扩展点**的最小验收用例（忘注册/破坏扩展点必须测试红）。

### 3.1 P0 · 语义地基（无 I/O）

| ID | 任务 | 对应 Python | 验收 |
|---|---|---|---|
| C3-1 | `internal/escape`：`EscapeMdCell`（Markdown 表格单元格）等 Markdown 辅助；HTML 转义由 `html/template` 承担（I-3） | `escaping.py` | 单测 + XSS 用例；`go test ./internal/escape` |
| C3-2 | `internal/registry`：`Load/Save/Validate/FindBy*`，schema 即契约（struct 或 map 均可，DR-6/DR-7），**格式定型后幂等**（I-2：原子写 + `Load→Save` round-trip 字节不变；格式允许一次性规范化） | `registry.py` | Load→Save round-trip 测试 + 并发写测试（临时文件唯一后缀，关掉审查项 M7） |
| C3-3 | `internal/issuepage`：`BuildTitle/BuildBody/Marker/Tombstone*` | `issue_page.py` | `tests/snapshot/` 投影快照基线通过 + `usm snapshot update` 机制 |
| C3-4 | `internal/parser`：`ParseComment`（首行命令+围栏提取）| `issue_parser.py` | 按语义重写 `test_issue_parser`/`test_unit` 相关用例（用例清单参照）；单行围栏等边界按现测 |

**验收命令**：`go test ./internal/... && go run ./cmd/usm snapshot check`

### 3.2 P1 · 脚本与命令层

| ID | 任务 | 对应 Python | 验收 |
|---|---|---|---|
| C3-5 | `internal/script`：头部构建/URL 注入/版本自增/ID 生成/changelog/文件读写 | `utils.py` | 按语义重写 `test_header` `test_changelog` `test_formatting` `test_doc_storage`（用例清单参照）；**D-03 待定·不阻塞（DR-6）** `format` 接口留 stub 并标注 |
| C3-6 | `internal/commands`：注册器（`init()` 自动注册 + 命令清单测试）+ 统一 panic→回帖边界 | `commands/__init__.py` | 10 命令清单断言；panic 注入测试返回 `❌ 命令 /x 执行时发生内部错误…` |
| C3-7 | 按语义逐命令重构（Python 测试作用例清单；顺序：`list → info → export → enable/disable → rm → add → up → sync → sync-all`） | 各命令文件 | 每个命令按语义重构对应用例：`test_manager` `test_soft_delete`（**复活契约必测**）`test_sync` `test_changelog` `test_doc_storage` `test_regression` |
| C3-8 | `cmd/usm run-command`：复刻 `manager.main` 的早退语义（行为契约，语义对齐而非逐字；非面板 Issue/无权限/未识别/未知命令均返回结果文本、**exit 0**；panic 由统一边界转回帖文本、**exit 0**（I-6）；仅 `comment-user`/`repo-owner` 缺失、registry 损坏等致命错误 exit 1） | `manager.py` | 表驱动覆盖 5 条早退路径 + 崩溃路径 |

**验收**：`go test ./... -cover` 阶段覆盖率 ≥85%

### 3.3 P2 · 网络与数据源

| ID | 任务 | 对应 Python | 验收 |
|---|---|---|---|
| C3-9 | **先抓 fixture**：GreasyFork / Userscript.zone / gist / direct 各存 2 份真实 HTML/JSON 到 `tests/fixtures/` | — | fixture 提交进仓（D-06 前置，缺此步不得动适配器） |
| C3-10 | `internal/sources`：`httpGet`（浏览器 UA+超时）、白名单 hostname 后缀匹配、4 适配器 | `sources/*` | `test_sources` 按语义全量重写 + fixture 回放测试；`evil.com/path/greasyfork.org` 必须不匹配 |
| C3-11 | `internal/github`：客户端（分页/重试/次级限速）+ 13 个固定文档用 `genqql` 生成；批量统计保留 `BuildStatsQuery(n)` 模板（即 Python 的 `build_query(N)`，D-05） | `project_issues` `discussions` `issue_stats` `panel_cleanup` 的查询 | `go generate ./...` 生成代码入库；CI 加 schema 校验等价物（D-05） |
| C3-12 | `usm project`：对账投影（创建/回填/重开/墓碑/版本帖发布） | `project_issues.py` | 按语义重写 `test_projector`（noop 幂等、账本四分支、墓碑）；动作序列有快照基线 |
| C3-13 | `usm cleanup`：分页拉评论→分组→保留 10→幂等归档→删除 | `panel_cleanup.py` | 按语义重写 `test_panel_cleanup`（幂等/删除失败重试/未超阈值不动） |

### 3.4 P3 · 站点生成

| ID | 任务 | 对应 Python | 验收 |
|---|---|---|---|
| C3-14 | `static/` 导入 `pages_assets.py` 的 CSS/JS（转成 `.css`/`.js` 文件 + `go:embed`） | `pages_assets.py` | 字节 diff 为空 |
| C3-15 | `internal/pages`：`index`（首屏10+`scripts.json`）/`scripts/<id>.html`（版本帖面板）/`commands/page-N.html`（每页5）/`build-warnings.txt` | `build_pages.py` | 按功能点重写页面测试（语义断言）+ 整页模板快照；与 Python 无字节比对（DR-6），幂等验收为同输入两次构建产物一致 |
| C3-16 | Markdown 渲染直接采用 goldmark+bluemonday（D-04 简化，DR-6） | `render_markdown` | XSS 用例硬验收（`javascript:`、`<script>`、`onerror` 均被消毒） |
| C3-17 | `usm build` 退出码与告警文件语义（拉取失败→`build-warnings.txt`、退出仍 0） | `build_pages.main` | 按语义重写 `TestBuildWarningsFile` |

**验收**：全量 `go test ./... -cover` ≥90%；`go run ./cmd/usm build` 在内容仓工作目录通过站点快照基线，同输入两次构建产物一致（幂等）。

**估计**：10–16 人日（DR-6 删除逐字对拍与双跑后下调，与 MIGRATION §2 合计一致）

---

## 阶段 4 —— 发布与切换实现（调用方不改）

| ID | 任务 | 产出 | 验收 |
|---|---|---|---|
| C4-1 | `release.yml`：`vX.Y.Z` tag → `GOOS/GOARCH` 矩阵（linux-amd64 起步，D-07）→ 计算 `sha256` → 生成 `checksums.txt` → 更新**同仓 `action.yml` 内置的 `VERSION` + `SHA256` 常量**并随 release 提交 | 自校验的分发 | 新 tag 后 `action.yml` 中版本与 sha 一致（CI 断言） |
| C4-2 | `action.yml` 内部实现从「Python 步骤」换成「下载二进制 + sha256sum 校验 + 执行」（**inputs/outputs 一字不改**） | Action v1 | 内容仓 4 条 workflow **不改一行**，全部重跑绿 |
| C4-3 | 移动大版本 tag `v1`；内容仓 pin 改为 `@<commit-sha>`（安全惯例），`@v1` 留给外部 | 稳定接口 | 见 `SPEC-ACTION.md §5` |
| C4-4 | ~~删除 Python 参照实现与 Python 测试~~（**已完成**：本仓从零用 Go 搭建，从未包含 Python 代码，MIGRATION §4 退出条件已通过 ✅） | 仓库瘦身 | ✅ 已满足：零 Python 文件待删 |
| C4-5 | `usm doctor --check`：registry 结构 + 数据一致性（源码/dist 存在性、孤儿检测） | 内容仓 `validate` workflow 的依赖 | 内容仓 CI 调用它 |

**估计**：2–3 人日（不含 C4-4 的观察期）

---

## 里程碑与总账

| 里程碑 | 完成标志 | 人日（M0–M2 为累计，M3 起为增量） |
|---|---|---|
| M0 | 阶段 0 全绿（同仓，CLI 单入口 + git add 收窄） | 1–2 |
| M1 | 两仓建立，旧仓 workflow 仍全绿 | 2–4 |
| **M2** | **内容仓 4 条 workflow 全部 `uses:` 化（Python 版 Action）——拆仓收益兑现** | **4–7** |
| M3 | Go P0+P1（地基+命令层）快照与行为测试全过 | +4–6 |
| M4 | Go P2+P3 全过，覆盖率 ≥90% | +6–10 |
| **M5** | **Action 内部切 Go 二进制，Python 实现删除** | **+2–3（总计 15–21）** |

---

## 风险登记（随进度更新）

| ID | 风险 | 缓解 | 状态 |
|---|---|---|---|
| R1 | 投影/站点格式再变导致二次全量重写 | 快照基线锁定格式；格式变更必须过快照评审；上线前在内容仓跑 `git diff --exit-code registry.json` | 开放 |
| R2 | D-03 `jsbeautifier` 无 Go 等价物 | `format` 留 stub（DR-6 待定·不阻塞），仅影响 /add /up 的美化输出 | ⏸ 待定·不阻塞 |
| R3 | bs4→goquery 解析差异 | C3-9 先抓 fixture；fixture 缺失禁止改适配器 | 开放 |
| R4 | Markdown 输出漂移（D-04） | XSS 用例 + 快照 | 开放 |
| R5 | 观察期两套代码维护成本 | 严格阶段门禁：M2 之后不再往 Python 加功能 | 开放 |
| R6 | 供应链（Action 下载二进制） | sha256 内置校验 + tag→sha pin | 设计已定 |
