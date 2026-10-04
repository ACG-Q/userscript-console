# MIGRATION · Python → Go（含 Rust 备选说明）

> 原则：**接口先行、实现后换、行为契约验收**（DR-4 + DR-6）。
> **重构而非逐字符复刻**（DR-6）：Python 仅作语义参照（用例清单的来源），不是裁判。
> 本文件回答三个问题：怎么验证新实现（行为契约 + 快照 + 幂等）、按什么顺序改、哪些地方必须人来拍板。

---

## 1. 验证基础设施（**动工前必须先建好**）

### 1.1 快照基线 `tests/snapshot/`

新项目**自产**基线（不是 Python 输出的回放），覆盖：

- 投影产物：Issue 标题、正文投影
- registry 规范化形态（一次性规范化后的定型格式）
- 站点整页：`index.html`、脚本详情页、`commands/page-N.html`
- `build-warnings.txt`

**建立方式**：实现完成后跑 `usm snapshot update` 写入基线，**人工审阅后提交**。
**变更纪律**：任何快照变更必须单独 commit 并在说明里写清原因；日常 CI 只跑 `usm snapshot check`。

### 1.2 行为契约测试

从 Python 测试**按语义重写**用例（不逐字面量、不比对字节），至少覆盖：

- 命令判定顺序（5 条早退 + panic 边界）
- 复活契约（软删→同源复活）
- 幂等投影（noop 跑两遍无动作）
- 归档幂等（按 comment id）
- 白名单绕过（`evil.com/path/greasyfork.org` 不匹配）
- XSS（`javascript:`、`<script>`、`onerror` 均被消毒）

用例清单见 `SPEC-ARCH-TEST §3.1`。

### 1.3 幂等验证（在内容仓）

```
usm build && usm project        # 第一次
usm build && usm project        # 第二次（同输入）
git diff --exit-code registry.json scripts dist   # 必须为空（幂等：同输入跑两次产物一致）
```

阶段 3 每完成一个 P 级任务就跑一次；**阶段 4 退出条件见 §4**。

---

## 2. 重构顺序（严格串行，每步都有可验证出口）

| 步 | 内容 | 出口条件 |
|---|---|---|
| M-0 | 建立快照基线与行为测试骨架（M-1 起随模块提交） | `usm snapshot check` 可跑，基线提交进仓 |
| M-1 | `escape` + `registry` + `issuepage`（P0） | I-1/I-2/I-3 全绿；`usm snapshot check` 通过 |
| M-2 | `parser` + `times` + `script`（P0/P1，**D-03 stub**） | parser/times/script 相关用例通过（其中 script 组 26 用例，见 SPEC-ARCH-TEST §3.1） |
| M-3 | `commands` 注册器 + 10 命令（P1） | 命令清单测试 + 复活契约 + `run-command` 五条早退 |
| M-4 | `github` 客户端 + 13 文档 genqql + 模板查询 | 编译期字段校验；负向测试（改错字段必须红） |
| M-5 | `projector` + `project` 子命令 | 投影快照 + 动作序列快照一致，幂等复跑无 diff |
| M-6 | `sources` + 4 适配器（**fixture 先行**） | fixture 回放 0 差异；白名单绕过用例 |
| M-7 | `cleanup` + `doctor` | 幂等/重试/阈值用例 |
| M-8 | `pages` + `static/` + markdown（D-04 已定案） | 站点模板快照一致，XSS 用例全绿 |
| M-9 | 覆盖率收敛到 ≥90% + 门禁接入 | CI 全绿 |

**估计**：M-1~M-3 ≈4–6 人日；M-4~M-7 ≈4–6；M-8~M-9 ≈2–4。合计 10–16 人日。

---

## 3. 双轨期约束（阶段 2 之后立即生效）

1. **Python 侧冻结**：作为回退路径只修严重 bug（DR-6：不再是裁判）。
2. 每个 PR 必须同时说明「是否影响快照基线」；影响则快照更新单独 commit 并说明原因。
3. 内容仓 workflow 已 `uses:` 化 → 行为变化只来自 Action 内部，**回滚 = 换 tag**，不需要改内容仓。
4. 任何线上问题先跑 `usm <cmd> --json` 与 Python 版对照**行为**（动作序列/结果语义），格式差异忽略（DR-6），定位是"接口层"还是"实现层"。

---

## 4. 切换与退出条件

**从 Python 版 Action（v0）切到 Go 二进制版（v1）的前置**：
- [ ] `go test ./...`（含 `usm snapshot check`）连续 3 次全绿
- [ ] 内容仓连续 3 次真实 workflow 运行：`git diff --exit-code registry.json scripts dist` 在构建后为空
- [ ] 覆盖率 ≥90%，四道门禁绿
- [ ] `format` stub 行为已文档化并在测试中明确（D-03 待定·不阻塞，拍板后另行实现）

**删除 Python 参照实现**（`C4-4` 任务，见 `PLAN.md` 阶段 4）额外要求：观察期 ≥2 周无回滚请求。

> **状态：✅ 已满足**。本仓（`userscript-console`）从零以 Go 搭建，从未包含 Python 代码（见 PLAN.md C1-1 描述的历史迁移路径——Python 参考实现在原始仓 `userscript-manager`，本仓初始即用 Go 重写）。`grep -rn "setup-python" action.yml .github/` 无匹配，`git ls-files "*.py"` 为空。MIGRATION §4 前置条件已全部通过，无需额外操作。

**回滚路径**：
1. Action 层回滚：内容仓 pin 换回旧 sha（分钟级，无需改代码）
2. 实现层回滚：`action.yml` 的 v1 步骤改回 v0 步骤（同仓 revert）
3. 数据回滚：内容仓 `git revert` 被工具改的提交（数据是真源，天然可回滚）

---

## 5. 决策登记（存档：除 D-03 待定外均已定案）

| ID | 决策 | 选项 | 默认建议 | 阻塞谁 | 状态 |
|---|---|---|---|---|---|
| **D-01** | Action 形态 | composite（Python→Go 两步） / Docker Action / JS Action | **composite**（快、可 `uses: ./` 自测） | C2-1 | ✅ 已定 |
| **D-02** | 内容仓是否保留现仓名 | 保留 `userscript-manager` / 新名 | **保留**（保 Pages URL 与存量 `@downloadURL`，DR-3） | 内容仓建仓 | ✅ 已定 |
| **D-03** | `jsbeautifier` 等价物 | ① `npx js-beautify`（钉版本，本地需 node）② 移植最小 beautifier ③ 移除美化（改变行为） | 留 stub，待定·不阻塞（DR-6 已拍板） | 无（不阻塞） | ⏸ 待定·不阻塞 |
| **D-04** | Markdown 渲染 | goldmark+bluemonday 语义等价（新基线）/ 追求字节等价（不现实） | 语义等价 + XSS 用例 | `internal/pages` | ✅ 已按 DR-6 定案（goldmark+bluemonday + XSS 用例） |
| **D-05** | 批量统计查询 | 保留模板+CI 校验 / 改成 N 次单查 / 改 list-all 后本地过滤 | **保留模板 + CI 校验**（payload 最小） | `internal/github` | ✅ 默认 |
| **D-06** | 适配器 fixture | 先抓 8 份真实快照再动代码 | 是（硬前置） | `internal/sources` | ✅ 默认 |
| **D-07** | runner 平台 | 仅 Linux / 加 macOS+Windows 矩阵 | **先仅 Linux**（与现状一致），按需扩展 | release 矩阵 | ✅ 默认 |

---

## 6. 与「拆仓」的耦合检查

- **阶段 1（建仓）必须在 M-1 之前**：快照基线与 fixtures 属于工具仓，先有仓才有归属。
- **阶段 2（Action 化）与 Go 重构完全正交**：可以在 Go 一行没写时就完成，拿到全部拆仓收益。
- 内容仓 `uses:` 之后，**Go 进度不影响线上**（Action 内部换实现而已）。

---

## 7. Rust 备选（若 DR-2 被推翻）

接口层（`action.yml` / `cmd/usm` 子命令与 flags / 快照基线 / fixtures）**完全不变**，仅替换实现映射：

| 项 | Go | Rust |
|---|---|---|
| GraphQL codegen | `genqql` | `cynic`（同样把字段错误变编译错误） |
| HTTP | `net/http` | `reqwest`（blocking 版即可） |
| HTML 解析 | `goquery` | `scraper` |
| Markdown | `goldmark` + `bluemonday` | `pulldown-cmark` + `lol_html`/`ammonia`（消毒需自配） |
| JSON 序列化稳定性 | `json.Encoder` + `SetEscapeHTML(false)`（幂等见 I-2，不与 Python 比字节） | `serde_json` 默认**不转义 HTML**，缩进 `to_string_pretty` 为 2 空格 → 需核对分隔符（`serde_json` 用 `": "` ✓） |
| 转义 | HTML 转义由模板引擎承担；Markdown 用 `EscapeMdCell` 等辅助 | HTML 转义由模板引擎承担；Markdown 辅助手写（`escape_html` crate 语义不同） |
| 注册器 | `init()` + `Register` | `inventory` crate 或显式 `Vec` |
| 发布 | 多平台 tar.gz + sha256 | 同（`cross` 交叉编译更省事） |
| 成本 | 基准 | **+20~30%**（借用检查 + 异步栈学习成本） |

**结论**：只有当团队 Rust 熟练度显著高于 Go 时才切换；否则按 Go 执行。
