# userscript-console — 开发规格书 · 仓库定位

> 目标仓库名：**`userscript-console`**（GitHub，与当前内容仓 `userscript-manager` 并列）
> 语言：**Go 1.22+**（DR-2；Rust 替代见 `MIGRATION-PY-TO-GO.md` 第 7 章）
> 现有实现：`userscript-manager` 仓中的 Python 代码，**以它为语义参照重构实现，不重造行为（DR-6：不逐字符复刻）**

---

## 1. 本仓是什么 / 不是什么

**是**：
1. 一个 CLI 工具 `usm`（4 个子命令），可在 CI 与本地运行；
2. 一个 GitHub Action（仓库根 `action.yml`），供内容仓 `uses:` 调用；
3. 这套行为的**测试与验收资产**（快照基线、表驱动测试、门禁）；
4. **显式内部扩展点**：新命令 / 数据源 / 页面 / 查询 / 接口字段加法不动核心（机制与加法路径见 `SPEC-ARCH-TEST.md §7`）。

**不是**：
- 不持有 `registry.json` / 脚本源码 / dist / archive（那些在内容仓）；
- 不声明 `on:` 触发器与 `permissions:`（在内容仓 workflow）；
- 不做 git commit/push（内容仓负责提交，本工具只写文件并输出 `changed`）。

---

## 2. 功能核对清单（现有 Python 实现，DR-7）

以当前 `userscript-manager@93dcff2` 为准。**每一条都是行为验收的对照项（输出格式自由，DR-6）。**
本节是**功能清单，不是实现模板**（DR-7）：4 入口 / 10 命令 / 4 适配器 / 14 查询**缺一不可**，但包结构、类型与算法由实现按 Go 惯例自行设计。

### 2.1 四个入口

| Python 入口 | 行数 | env 输入 | 副作用 | 迁移为 |
|---|---|---|---|---|
| `manager.py` | 80 | `COMMENT_BODY` `COMMENT_USER` `REPO_OWNER` `ISSUE_NUMBER` | 写 `command_result.txt`、print 结果 | `usm run-command` |
| `project_issues.py` | 289 | `GITHUB_TOKEN` `GITHUB_REPOSITORY` | GraphQL 对账投影、可能写 registry | `usm project` |
| `build_pages.py` | 800+ | `GITHUB_TOKEN?` `GITHUB_REPOSITORY` `GITHUB_PAGES_URL` `AUTHOR_NAME` `AUTHOR_NAMESPACE` `GITHUB_REF_NAME` | 写 `dist/`、`dist/scripts.json`、`dist/commands/**`、`dist/build-warnings.txt` | `usm build` |
| `panel_cleanup.py` | 210 | `GITHUB_TOKEN` `GITHUB_REPOSITORY` | 写 `archive/commands.json`、GraphQL 删评论 | `usm cleanup` |

### 2.2 核心库 → 重构优先级

| 模块 | 关键不变量（行为语义，见 §4） | 优先级 |
|---|---|---|
| `userscript_manager/escaping.py` | 只保留 `EscapeMdCell`（竖线转义为反斜杠+竖线、换行→空格）；HTML 转义由 `html/template` 承担（见 I-3） | **P0（其他模块依赖）** |
| `userscript_manager/registry.py` | 原子写（临时文件+rename）+ 格式定型后幂等（I-2）；加载即校验结构；`setdefault` 补 `discussions`/`deleted` | **P0** |
| `userscript_manager/issue_page.py` | 标题/正文稳定判等决定投影是否更新（I-1）；`<!-- script-id: X -->` 首行标记；**墓碑正文不带标记** | **P0** |
| `userscript_manager/issue_parser.py` | 首行 `/cmd args` 解析、围栏代码块提取、`remove_code_blocks` | P1 |
| `userscript_manager/utils.py` | 头部构建/URL 注入/`increment_version`；`jsbeautifier` 美化（**D-03：待定·不阻塞**）；changelog 头插 | P1 |
| `commands/`（9 文件 10 命令） | 注册表 + 统一异常边界（异常→回帖文本）；软删除→复活契约 | P1 |
| `sources/`（4 适配器） | hostname 后缀白名单防绕过；`==UserScript==` 校验；GreasyFork 多级回退（**需 HTML fixture，见 §5**） | P2 |
| `issue_stats.py` / `discussions.py` | 批量别名查询 `build_query(N)`；`node(id)` + 内联片段取 Discussion | P2 |
| `build_pages.py` + `pages_assets.py` | 站点 HTML 结构、`scripts.json` 懒加载、命令归档分页（每页 5） | P3 |

**已注册的 10 个命令**（`get_command` 惰性自动发现）：`add` `up` `sync` `sync-all` `list` `info` `export` `rm` `enable` `disable`

### 2.3 GraphQL 文档（14 个，已在 CI 通过 schema 校验）

```
project_issues: REPO_QUERY, LABELS_QUERY, LIST_QUERY, CREATE_LABEL_MUTATION,
                CREATE_MUTATION, UPDATE_MUTATION, CLOSE_MUTATION, REOPEN_MUTATION
discussions:    DISCUSSION_CATEGORIES_QUERY, CREATE_DISCUSSION_MUTATION, DISCUSSION_NODE_QUERY
issue_stats:    build_query(N)  ← 动态别名模板，无法用 codegen，见 D-05
panel_cleanup:  PANEL_QUERY, DELETE_COMMENT_MUTATION
```
权威来源：当前仓 `tools/validate_graphql.py::collect_queries`。**14 个文档的查询语义（字段/变量/返回集）必须等价**；文本形式自由，由 genqql codegen + CI schema 校验保障（D-05）。

---

## 3. 对外接口三层

```
┌─ 层3  GitHub Action  ─── action.yml（inputs/outputs）────────── 内容仓 uses:
├─ 层2  CLI 子命令      ─── usm run-command|project|build|cleanup ── 内容仓 run: 或本地调试
└─ 层1  库（Go package）── internal/...                          ── 仅本仓测试使用
```

- 层2 是**可本地执行的**：`go run ./cmd/usm build` 必须在内容仓工作目录下完整跑通（保留今天 `python build_pages.py` 的调试体验）。
- 层3 只做「参数搬运 + 输出封装」，**不含业务逻辑**（这样 Python→Go 只改层1/层2）。

---

## 4. 关键不变量（行为契约，破坏即线上事故）

细节与验收以 SPEC-ARCH-TEST §2 为准，编号 I-1..I-7 与全项目文档交叉引用一致。

1. **I-1 幂等投影（输出稳定判等）**：`BuildTitle`/`BuildBody` 同一输入两次输出必须一致；projector 仍以「生成结果 == 线上 Issue」判等决定是否更新。与 Python 输出不再要求一致（DR-6）：首次上线接受一次性重写全部 Issue；验收 = 快照基线 + 上线后 `git diff` 无意外变更。
2. **I-2 registry 序列化（schema 契约 + 格式定型后稳定）**：schema/字段集合/数据所有权是硬契约；格式允许一次性规范化（DR-6，原「必须与 Python 字节等价、禁止 map」作废）；规范化后 `Load→Save` 空改动 round-trip 字节不变、原子写（临时文件+rename）保留、序列化形态一经定型不得再变。实现可用 struct 或 map（`encoding/json` 对 map 按键排序，排序即稳定，可接受）。
3. **I-3 HTML 一律经模板（XSS 防线）**：站点/详情/分页 HTML 必须由 `html/template` 生成，禁止手拼字符串；上下文自动转义是主要 XSS 防线，配 XSS 用例（`javascript:`、`<script>`、`onerror` 被消毒）硬验收。`internal/escape` 降级为只保留 `EscapeMdCell` 等 Markdown 辅助；原「必须手写 Python 语义 `EscapeHTML`、直接用 `html.EscapeString` 是 bug」的整套对照表作废（Markdown 表格单元格转义仍需 `EscapeMdCell`：`|`→`\|`、换行→空格）。Issue 正文投影可用 `text/template`。
4. **I-4 软删除复活契约**：`/rm` 后重发 `/add <url>` 必须复用原 ID 就地复活（`add_sync`，测试 `tests/test_soft_delete.py::TestReviveByAdd`）。
5. **I-5 命令注册**：新增命令 = 新文件 + 注册器自动发现；漏注册是线上「未知命令」事故（现有 Go 方案用 `init()` 注册 + 一个测试断言命令清单）。
6. **I-6 统一异常边界**：任何命令 panic → 转为回帖文本 + **exit 0**（`commands.Wrap` 兜底，详见 SPEC-ARCH-TEST §2 I-6）。
7. **I-7 时间与相对时间**：`relative_time` 中文单位（刚刚/N 分钟前/…），`now` 必须可注入（测试用）；`Clip` 空白折叠 + 截断 + `…`。

---

## 5. 已知风险与决策登记（当前无阻塞项）

| ID | 风险 | 建议 | 需要人类拍板？ |
|---|---|---|---|
| **D-03** | `jsbeautifier` 等价物未定（`/add` `/up` 美化输出） | `format` 接口留 stub，标注**待定·不阻塞**（DR-6）；拍板后再实现 | 否（待定·不阻塞） |
| **D-04** | Markdown→HTML：Python `markdown+fenced+tables`+`nh3` vs Go `goldmark+bluemonday` **字节必不等** | 直接用 `goldmark+bluemonday`（DR-6），XSS 用例硬验收，无需与 Python 字节比对 | 否 |
| **D-05** | `build_query(N)` 动态别名模板无法 codegen | 保留字符串模板 + 在 CI 继续跑 `validate_graphql.py`（或 Go 版 schema 校验）；其余 13 个走 `genqql` | 否 |
| **D-06** | GreasyFork/Userscript.zone 解析：`bs4`→`goquery` 选择器语义差异 | **先抓 HTML fixture 再动代码**（当前 0 fixture、userscript_zone 覆盖率仅 43%） | 否 |
| **D-07** | Action 下载二进制仅 Linux runner 可用 | v0 先 Linux-only（与现状一致），需要时再加 runner 矩阵 | 否 |

---

## 6. 本仓目录规划

```
userscript-console/
├── action.yml                     # 层3：inputs/outputs + 下载校验执行二进制
├── cmd/usm/main.go                # 层2：子命令分发
├── internal/
│   ├── cli/                       #   各子命令的参数与编排
│   ├── escape/                    # P0：EscapeMdCell 等 Markdown 辅助（HTML 转义交给模板）
│   ├── registry/                  # P0：读写/校验/原子写（schema 即契约，格式定型后幂等 I-2）
│   ├── issuepage/                 # P0：标题正文 + marker + 墓碑
│   ├── parser/                    # P1：/cmd 解析、围栏代码块
│   ├── script/                    # P1：头部构建、版本、changelog、文件读写
│   ├── commands/                  # P1：10 个命令 + 注册器
│   ├── sources/                   # P2：4 适配器 + 白名单
│   ├── github/                    # P2：GraphQL 客户端 + 14 文档 + codegen
│   ├── projector/                 # P2：Issue 投影对账（创建/回填/重开/墓碑/版本帖）
│   ├── times/                     # P2：相对时间/Clip（now 可注入，I-7）
│   ├── pages/                     # P3：站点生成（templates/：go:embed 的 html/template；含 pages_assets 静态资源 embed）
│   │   └── templates/             #   HTML 模板（禁止手拼 HTML 字符串）
│   └── cleanup/                   # P3：分组/归档/删除
├── static/                        #   从 pages_assets.py 导出的 .css/.js（go:embed）
├── tests/
│   ├── snapshot/                  #   新项目自产快照基线（模板/投影快照）
│   └── fixtures/                  #   GreasyFork 等 HTML 快照（D-06 前置）
├── .github/workflows/
│   ├── test.yml                   # build+vet+lint+test+cover≥90+snapshot+schema
│   └── release.yml                # tag → 多平台二进制 + SHA256 + 移动 v1 tag
└── docs/                          #   design.md、commands/ 从内容仓迁来（归属见 SPEC-DATA）
```

> **建议布局（DR-7）**：以上目录是起点不是枷锁——可按 Go 惯例增删包、重划分职责，前提是 ① 依赖方向自上而下（SPEC-ARCH-TEST §1）；② §7 扩展点机制不被破坏；③ 功能核对清单（§2）不缺项。

---

## 7. 责任边界速查

| 事项 | 工具仓 | 内容仓 |
|---|---|---|
| `on:` / `permissions:` | ❌ | ✅ |
| `git add/commit/push` | ❌（只输出 `changed`） | ✅（**路径白名单，见 SPEC-WORKFLOWS §4**） |
| `gh workflow run deploy-pages` | ❌ | ✅ |
| 数据文件内容 | ✅ 写 | ✅ 拥有、审查 diff |
| release / 版本 tag | ✅ | pin 版本（`@<sha>` 或 `@v1` 或 `@vX.Y.Z`） |
| 快照与行为测试 | ✅ | 迁移期保留 Python 测试（回退用） |
| Pages 部署与 `_site` 组装 | ❌（`build` 只产 dist） | ✅ |

---

## 8. 调用方式（v1.1.0+）

### 8.1 源码模式（默认，`use-binary: false`）

始终跟随所引用 commit 的源码，**无需任何版本配置**：

```yaml
uses: acg-q/userscript-console@v1.1.0    # 或 @v1、@<sha>
with:
  command: build
  github-token: ${{ secrets.GITHUB_TOKEN }}
```

### 8.2 二进制模式（`use-binary: true`）——**零手填版本**

只需写 tag，**`binary-version` 与 `binary-sha256` 自动推导**：

```yaml
# 精确语义化版本 tag（推导到对应 release）
uses: acg-q/userscript-console@v1.1.0
with:
  command: build
  github-token: ${{ secrets.GITHUB_TOKEN }}
  use-binary: true
  # binary-version 与 binary-sha256 自动从 @v1.1.0 推导

# 大版本 tag（自动取最新 v1.x release）
uses: acg-q/userscript-console@v1
with:
  command: build
  github-token: ${{ secrets.GITHUB_TOKEN }}
  use-binary: true
```

### 8.3 显式声明版本（内容仓安全惯例 / 收紧信任链）

```yaml
uses: acg-q/userscript-console@<40位sha>
with:
  command: build
  github-token: ${{ secrets.GITHUB_TOKEN }}
  use-binary: true
  binary-version: '1.1.0'
  binary-sha256: '<64位十六进制，来自 release checksums.txt>'
```

### 8.4 最小权限声明

内容仓 workflow 需要的最小 `permissions`：

```yaml
permissions:
  contents: write    # git commit/push（project/build/cleanup 可能改动数据文件）
  issues: write      # 评论回帖、删评论
  actions: write     # gh workflow run deploy-pages.yml
  discussions: write # discussion 相关命令
```
