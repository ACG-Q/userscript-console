# SPEC · 架构、不变量与测试迁移

> 面向实现 AI：**先读第 2 章的 7 条不变量**，它们比目录结构重要——任何一条破坏都直接变成线上事故。
> 语义参照：`userscript-manager@93dcff2`（Python），本文件的行号/测试名以该提交为准（DR-6：仅功能对照，不逐字符复刻）。

---

## 1. 包结构与职责映射（**职责参照，不是目录模板**，DR-7）

> 本表规定**职责划分与依赖方向**；「职责参照」列说明该包对应 Python 的哪块功能（功能核对清单）。
> 实现可按 Go 惯例增删包、重划分职责、选择类型与错误处理方式，只要：① 依赖方向自上而下；② 职责不混（§7 扩展点不被破坏）；③ 功能清单（`README.md §2`）不缺项。

| Go 包（建议布局） | 职责参照（Python） | 核心导出 | 依赖方向 |
|---|---|---|---|
| `internal/escape` | `userscript_manager/escaping.py` | `EscapeMdCell`（+Markdown 辅助；HTML 转义由 `internal/pages` 的 `html/template` 承担） | 无（**最底层**） |
| `internal/registry` | `registry.py` | `Load` `Save` `Validate` `FindByID` `FindBySourceURL` `Add` `RegistryError` | `escape` |
| `internal/issuepage` | `issue_page.py` | `BuildTitle` `BuildBody` `Marker` `ScriptIDFromBody` `TombstoneTitle/Body` `DiscussionBody/Title` | `escape` |
| `internal/parser` | `issue_parser.py` | `ParseComment` `RemoveCodeBlocks` | 无 |
| `internal/script` | `utils.py` | `BuildHeader` `EnsureURLs` `SyncVersion` `IncrementVersion` `ExtractMeta` `IDs` `Changelog` `Read/WriteSource` `WriteDist` | `escape`、`registry` 类型 |
| `internal/commands` | `commands/*` | `Register` `Get` `GetAll` `Wrap`（panic 边界） | `registry` `script` `sources` |
| `internal/sources` | `sources/*` | `HTTPGet` `Matches` `Adapter` 接口 + 4 实现 | — |
| `internal/github` | `project_issues`/`discussions`/`issue_stats`/`panel_cleanup` 的查询与客户端 | `Client` `Execute` + genqql 生成代码 + `BuildStatsQuery(n)` 模板 | — |
| `internal/projector` | `project_issues.py::project` | `Project` `EnsureDiscussionPost` | `github` `issuepage` `registry` |
| `internal/pages` | `build_pages.py` + `pages_assets.py` | `BuildIndex` `BuildScriptsJSON` `BuildDetail` `BuildCommandPages` `RenderMarkdown` `RenderTemplate` | `issuepage` `escape` `github` |
| `internal/cleanup` | `panel_cleanup.py` | `FetchComments` `Group` `MergeArchive` `Process` | `github` |
| `internal/times` | `issue_stats.py::relative_time`/`clip` | `RelativeTime(t, now)` `Clip` | 无（**now 可注入**） |
| `internal/cli` | `manager.py` 等 4 入口的参数解析与编排 | 各子命令 `Run`（含 `runcmd`） | `commands` `projector` `pages` `cleanup` `registry` |
| `cmd/usm` | `manager.py` 等 4 入口 | 子命令分发 | 上述全部 |
| `static/` | `pages_assets.py` 字符串常量 | `go:embed` 的 `.css`/`.js` | — |

**依赖规则**：只允许上表自上而下；`internal/commands` 不得反向 import `pages`；`github` 不得 import `commands`。

**Go 实现基线（DR-7，惯用法而非照搬）**：
- `context.Context` 贯穿所有 HTTP/GraphQL 调用（超时、取消）；
- error 返回值为主，包装用 `%w`；`panic` 仅允许出现在 `commands.Wrap` 兜底（I-6）；
- 测试以**接口注入 fake**（按方法签名路由），替代"按查询子串路由"的假客户端（§3.2）；
- 模板与静态资源用 `embed.FS`；测试一律表驱动；GraphQL 固定文档走 `genqql` codegen。

---

## 2. 七条不变量（行为契约，**破坏 = 线上事故**）

### I-1 幂等投影（输出稳定判等）
`BuildTitle(script)` 与 `BuildBody(script)` **同一输入两次输出必须一致**；`projector` 仍以「生成结果 == 线上 Issue」判等决定是否更新，**相等才跳过**。
- 与 Python 输出不再要求一致（DR-6）：首次上线接受一次性重写全部存量 Issue 正文与站点 HTML。
- 验收：`tests/snapshot/` 快照基线 + 上线后内容仓 `git diff` 无意外变更（比较对象是工具自己的上次输出；「与线上 Issue 判等 / 同输入两次输出一致」仍是契约性判等）。

### I-2 registry 序列化（schema 契约 + 格式定型后稳定）
- **schema / 字段集合 / 数据所有权是硬契约**；**格式允许一次性规范化**（DR-6，原「必须与 Python 字节等价、禁止 map」作废）。
- 规范化之后：`Load→Save` 空改动 round-trip **字节不变**；原子写（临时文件+rename）保留；**序列化形态一经定型不得再变**。
- 实现可用 struct 或 map：`encoding/json` 对 map 按键排序，排序即稳定，可接受。
- 验收：`tests/snapshot/` 中 registry 格式基线（定型后一致）+ `Load→Save` 空改动 round-trip 字节不变测试。

### I-3 HTML 一律经模板（XSS 防线）
- 站点/详情/分页 HTML 必须由 `html/template`（`go:embed` 模板）生成，**禁止手拼 HTML 字符串**；上下文自动转义是主要 XSS 防线。Issue 正文投影可用 `text/template`。
- 验收：XSS 用例硬验收——`javascript:`、`<script>`、`onerror` 均被消毒（配合 D-04：直接用 `goldmark+bluemonday`）。
- `internal/escape` 降级为只保留 `EscapeMdCell` 等 Markdown 辅助；原「必须手写 Python 语义 `EscapeHTML`、直接用 `html.EscapeString` 是 bug」的整套对照表作废。
- `EscapeMdCell`（Markdown 表格单元格转义仍需要）：`|` → `\|`、`\r\n`→空格、`\n`→空格（顺序敏感）。

### I-4 软删除 → 同源复活
`/rm <url>` 置 `deleted:true` 并删源码/dist；重发 `/add <url>` **复用原 ID**、就地清 `deleted`、回写文件、追加 changelog「复活同步」，**不得新增条目**。
- 验收：`TestReviveByAdd` 三用例（复活/仍拒活跃重复/复活后 sync 不再被拦）。

### I-5 命令注册不可遗漏
Python 侧 `get_command` 已改为惰性 pkgutil 自动发现；Go 侧用 `init()` + `Register`。
- 验收：**命令清单测试**硬编码期望集合 `{add,up,sync,sync-all,list,info,export,rm,enable,disable}`，新增命令忘记注册即测试红。

### I-6 统一异常边界
任何命令 panic → 转为回帖文本 `` ❌ 命令 /<name> 执行时发生内部错误，已中止（仓库状态可能未变更）。\n```\n<stack 限制3层>\n``` ``，**exit 0**（调用方仍会回帖）。
- 验收：panic 注入用例。

### I-7 时间与相对时间
`RelativeTime(iso, now)`：`刚刚 / N 分钟前 / N 小时前 / N 天前 / ≥30天 → YYYY-MM-DD`；`Clip(s, limit)`：空白折叠 + 截断 + `…`。
- **now 必须是参数**（Python 已如此），否则测试不可重复。
- 验收：`test_issue_stats::test_relative_time_units` 逐分支。

---

## 3. 测试迁移（Python 测试用例 → Go 表驱动；用例数为约数）

### 3.1 优先级与对应关系

| P | Python 测试文件 | 用例数(约) | Go 目标 | 备注 |
|---|---|---|---|---|
| **P0** | —（golden 已废） | — | `TestSnapshot` | 新项目自产快照基线，建立后是回归网（不再充当对拍 Python 的裁判） |
| **P0** | `test_escaping` + `test_unit` | 20 | `internal/escape` 表驱动 | EscapeMdCell + XSS 用例 |
| **P0** | `test_registry` | 10 | `internal/registry` | 含损坏文件/软删默认值 |
| **P0** | `test_issue_body` | 17 | `internal/issuepage` | 墓碑无 marker、索引降级 |
| **P0** | `test_issue_parser` | — | `internal/parser` | 首行命令/围栏提取（对应 PLAN C3-4） |
| P1 | `test_manager` | 8 | `cmd/usm run-command` | 五条早退 |
| P1 | `test_soft_delete` | 11 | `internal/commands` | **I-4 复活契约** |
| P1 | `test_header` `test_changelog` `test_formatting` `test_doc_storage` | 26 | `internal/script` | `test_formatting` 涉 D-03（`format` 留 stub，待定·不阻塞） |
| P1 | `test_sync` `test_regression` | 20 | `internal/commands` | 行为回归重点 |
| P1 | `test_issue_stats` | 10 | `internal/times` + `internal/github` | 相对时间/批量查询 |
| P2 | `test_projector` | 19 | `internal/projector` | **noop 幂等 + 账本四分支**，假件改 schema 化 |
| P2 | `test_discussions` | 11 | `internal/github` | 断言 `node(id)` 而非 `discussion(id)` 的回归必须保留 |
| P2 | `test_panel_cleanup` | 12 | `internal/cleanup` | 幂等归档 + 删除失败重试 |
| P2 | `test_sources` | 21 | `internal/sources` | **需 D-06 HTML fixture** |
| P3 | `test_pages` | 88 | `internal/pages` | 字面量断言 → 语义断言（见 3.3） |

### 3.2 假客户端改造（本项目测试的系统性弱点，**必须修**）

现状：13 处假客户端按**查询子串路由**（如 `if "issues(first" in query`），字段名写错照样全绿——线上 `Query.discussion` 事故正是这样漏掉的。
Go 侧要求：
1. genqql 生成的类型让**字段名错误直接编译失败**（13 个固定文档）；
2. `Client` 接口注入 fake：按**方法签名**（如 `CreateIssue(input)`）而非字符串路由；
3. 断言 `variables` 与调用方意图一致（如 `UpdateIssue` 的 `id` 必须是被测对象）；
4. 保留一个**负向测试**：故意把某查询字段改错 → CI 的 schema 校验必须报错（等价现在 `tools/validate_graphql.py` 的验证能力）。

### 3.3 `test_pages` 88 用例的迁移（语义断言 + 模板快照）

| 类型 | 现状举例 | Go 写法 |
|---|---|---|
| 字符串字面量 | `assertIn('<a class="btn primary" href=…>安装脚本</a>', html)` | 整页结构改由模板快照锁定（`usm snapshot check`），节点级语义断言：解析成节点再断言属性（用 `golang.org/x/net/html` 或 `goquery`），或改为「包含 `class="btn primary"` 且 `href` 为 X」两条断言 |
| 语义断言 | `assertIn("复活成功", out)` | 保留原样 |
| 快照类 | `data-total="12"`、分页 5 条 | 保留原样（它们锁的是行为不是实现） |

**不许**为了过测试改行为；**不许**把断言改弱到失去意义（评审时逐条对照本表）。

---

## 4. 门禁（工具仓 CI，与 Python 仓四道门禁等价升级）

| Python 现状 | Go 等价 | 阈值 |
|---|---|---|
| `ruff check .`（E4/E7/E9/F/I） | `golangci-lint run`（`govet` `staticcheck` `errcheck` `ineffassign` `gofumpt`） | 0 issue |
| `mypy`（disallow_untyped_defs 等） | Go 编译器本身 + `staticcheck`；可选 `golangci-lint` 开 `unused` | 0 error |
| `coverage report --fail-under=90` | `go test -coverprofile` + `-covermode=atomic` | **≥90%** |
| `tools/validate_graphql.py` | genqql 编译期（13 文档）+ CI 校验 `BuildStatsQuery(1)` 模板 | 0 error |
| 快照 | `usm snapshot check` | 新项目基线一致 |
| — | `go vet ./...`、`go mod verify`、`gofumpt -l` | 0 |

**新增**：`staticcheck` 的 `SA1019`（弃用 API）、`errcheck`（未检查 error）必须开——Go 项目最常见的两类真实缺陷。

---

## 5. `static/` 静态资源迁移

1. 把 `pages_assets.py` 的 `TOKEN_CSS`/`COMPONENT_CSS`/`PREVIEW_CSS`/`FILTER_JS`/`LIST_JS`/`DISC_JS` **原样导出**为 `static/{tokens,components,preview}.css`、`static/{filter,list,disc}.js`；
2. `go:embed static/*`；构建时拼接顺序必须与 Python 一致：`TOKEN+COMPONENT+PREVIEW` → `<style>`，`extra_js` 顺序 `FILTER→LIST` / `DISC`；
3. 验收：CSS/JS 资源段与 Python 版 **字节原样**（迁移成本低，保留比对）；HTML 结构由模板生成，以 `usm snapshot check` 锁定。
4. 之后 `pages_assets.py` 即可删除（阶段 4 与 Python 参照实现一起）。

---

## 6. 错误处理与日志约定

- 可恢复错误（**单脚本**同步失败、讨论统计拉取失败→`build` 降级渲染）→ 结果文本/`warnings`，**不中断整体**；例外：`cleanup` 的评论拉取失败 = exit 1 且零改动（SPEC-CLI §4）；
- 不可恢复（registry 解析失败、`project`/`cleanup` 缺 token、schema 不匹配）→ stderr `ERROR:` + exit 1；例外：`build` 缺 token = 降级渲染 exit 0（SPEC-CLI §3）；
- 一律不用 `panic` 做控制流（除 `commands.Wrap` 捕获的兜底）；
- 不把 token、comment body 原文写进日志（comment body 可能含代码块，日志只记长度与前 40 字符摘要）。

---

## 7. 扩展点设计（DR-7：保持扩展能力，**新增功能不动核心**）

> 已拍板范围：**内部扩展点**——命令/数据源/页面/查询/接口字段全部走「接口 + 注册器 / 模板」，
> 新增一个扩展项只允许新增文件与注册行，**禁止**在分发器、构建主流程里写 `if/else` 特判。
> 通用铁律（I-5 模式推广到所有扩展点）：**忘注册必须让测试红**（清单断言硬编码期望集合）。

| 扩展点 | 接口 / 机制 | 新增一个要动什么 | 必过的验收 |
|---|---|---|---|
| **新命令** | `commands.Command` 接口（name/help/usage/handler）+ `init()` 自动注册（I-5） | 新文件一个 + 注册一行 + help 文案 | 命令清单断言（漏注册即红）+ 该命令行为用例 |
| **新数据源** | `sources.Adapter` 接口 + hostname 白名单条目（后缀匹配防绕过） | 新适配器文件 + 白名单注册 + 真实 HTML/JSON fixture | fixture 回放用例 + 白名单绕过负向用例 |
| **新页面 / 站点区块** | `internal/pages/templates/*.tmpl`（`embed.FS`）+ 对应渲染函数 | 新 `.tmpl` 文件 + 渲染函数挂入构建主流程 | 模板快照（`usm snapshot check`）+ XSS 用例 |
| **新 GraphQL 查询** | `genqql` codegen（固定文档）；动态批量保留 `BuildStatsQuery(n)` 模板（D-05） | 新文档 + `go generate ./...`（生成代码入库） | 编译期字段校验 + 负向测试（改错字段必须红） |
| **新 Action input / CLI flag** | `SPEC-ACTION §1` 契约；CLI 配置优先级 flag > env > 默认（SPEC-CLI §0.4） | 加字段 + **缺省值 = 旧行为**（向后兼容） | 默认分支单测 + 契约文档同 PR 双向同步 |
| **新 registry 可选字段** | `setdefault` 语义（SPEC-DATA §1.2） | 字段 + 默认值，**不 bump** schema | 加载即补默认 + `Load→Save` 幂等（I-2） |
| **内容仓新触发器** | 同一 Action，`command:` 白名单任选（SPEC-ACTION §2） | 内容仓新增 workflow，**工具零改动** | SPEC-WORKFLOWS §6 checklist |

**反例（评审红线）**：为一个新命令在 `cmd/usm` 里加 `case` 分支；为一个新站点在既有适配器里塞 `if hostname ==`；为一个新页面类型在 `BuildIndex` 里堆字符串拼接——这些都是"破坏扩展点"的实现，评审直接打回。
