# SPEC · `usm` CLI 接口契约

> 版本：v1（与 `action.yml` 同步演进）
> 实现位置：`cmd/usm/main.go` + `internal/cli`
> 参照：现有 `manager.py` / `project_issues.py` / `build_pages.py` / `panel_cleanup.py`

---

## 0. 全局约定

### 0.1 工作目录与数据根

- 所有命令默认在**当前工作目录**即内容仓根（含 `registry.json`）下运行；
- `--root <path>` 可显式指定数据根（Action 里固定传 workspace）；
- 本 CLI **只读写上述数据根内的文件**，不执行任何 git 命令。

### 0.2 退出码

| 码 | 含义 | 调用方动作 |
|---|---|---|
| `0` | 成功。**包括「命令执行完成但结果是错误文案」的业务失败**（复刻 `manager.main` 语义：早退也 exit 0，结果文本承载信息） | 继续 commit/回帖 |
| `1` | 致命：参数非法、数据损坏（registry 解析失败）、网络/权限不可恢复、`--strict` 违规 | 调用方按失败处理（Action 会把错误写进 `result`） |
| `2` | 用法错误（未知子命令/缺必填参数） | 同上 |

### 0.3 输出协议

- **stdout**：人类可读结果（Action 用它作回帖正文）。多行原样保留。
- **stderr**：诊断日志（`WARN:`/`ERROR:` 前缀），不参与回帖。
- `--json`：stdout 输出单个 JSON 对象（Action 用），字段见各命令 §；与人类模式互斥。
- `--quiet`：不打印人类结果（供无人值守）。

### 0.4 配置来源（优先级：flag > env > 默认）

| 配置 | flag | env | 默认 |
|---|---|---|---|
| 数据根 | `--root` | — | `.` |
| 令牌 | `--token` | `GITHUB_TOKEN` | 空（`project`/`cleanup` 为空即报错 exit 1；`build` 可空=降级） |
| 仓库 | `--repo` | `GITHUB_REPOSITORY` | `owner/repo`（`--repo` 缺 `/` → exit 2） |
| Pages 基址 | `--pages-base` | `GITHUB_PAGES_URL` | 由 `--repo` 推导 `https://<owner>.github.io/<name>` |
| 作者 | `--author-name` `--author-ns` | `AUTHOR_NAME` `AUTHOR_NAMESPACE` | `Your Name` / `https://your-namespace.com` |
| registry schema | `--registry-schema-version` | — | `1`，不匹配 → exit 1 |
| 注册表文件 | `--registry` | `USM_REGISTRY` | `registry.json` |
| 脚本目录 | `--scripts-dir` | `USM_SCRIPTS_DIR` | `scripts` |
| 分发/站点目录 | `--dist-dir` | `USM_DIST_DIR` | `dist` |
| 命令归档文件 | `--archive-path` | `USM_ARCHIVE_PATH` | `archive/commands.json` |

### 0.5 数据布局（路径可配置）

后四项即数据布局（`internal/layout` 统一解析，`usm help` 亦有摘要）：

- 五个触碰数据的子命令（`run-command`/`project`/`build`/`cleanup`/`doctor`）在入口解析；非法值 → stderr `ERROR: ...` + exit 2（§0.2）。
- **相对值**：相对数据根解析，必须是干净的 `/` 相对路径——拒绝空值、`..`（防目录穿越）与反斜杠（跨平台歧义）；**绝对值**原样透传（如 CI 指到 runner 临时目录）。
- 作用面：
  - `registry.json` → registry 读写与 schema 校验；
  - `scripts/` → 脚本落盘/列表/删除（`script.FS`）；
  - `dist/` → `usm build` 整站产物与 `.user.js` 分发文件的输出目录；`@downloadURL`/`@updateURL` = `PAGES_BASE/<dist 相对段>/<id>.user.js`，自定义 `--dist-dir` 时外部链接同步跟随；
  - `archive/commands.json` → 命令归档（`project` 读取），站点命令历史页经 `pages.Options.ArchivePath` 同源。

---

## 1. `usm run-command`

**语义对应**：`manager.py::main` + `parse_comment` + `get_command`（DR-6：行为对齐，输出格式自由）

```
usm run-command [--comment-body <s>] [--comment-user <s>] [--repo-owner <s>]
                [--issue-number <n>] [--result-file <path>]
                [--json]
```

| 输入 | env 兜底 | 必填 |
|---|---|---|
| `--comment-body` | `COMMENT_BODY` | 否（空=「未识别命令」结果） |
| `--comment-user` | `COMMENT_USER` | **是**（缺失或空 → exit 1，fail-open 修复） |
| `--repo-owner` | `REPO_OWNER` | **是**（同上） |
| `--issue-number` | `ISSUE_NUMBER` | **是**（整数，非整数/缺失 → 结果文本，对齐现语义但不再跳过校验） |

**判定顺序（逐条对齐语义，缺一不可）**：

1. `issue-number != control-issue-number(=1)` → 结果 `非命令面板 Issue #<n>，忽略执行`，exit 0
2. `comment-user != repo-owner` → 结果 `权限不足：<user> 不是仓库所有者 <owner>`，exit 0
3. 解析失败/无命令 → `未识别命令`
4. 命令未注册 → `未知命令: <cmd>`
5. 执行（**统一 panic 边界**：panic → 结果 `` ❌ 命令 /<cmd> 执行时发生内部错误，已中止（仓库状态可能未变更）。\n```\n<stack≤3>\n``` ``，exit 0）

**`--json` 输出**：
```json
{ "authorized": true, "changed": true, "result": "✅ 自写脚本添加成功！\nID: …", "command": "add" }
```
- `authorized`：步骤 2 是否通过；
- `changed`：本次是否写入任何文件（registry/scripts/dist 任一 mtime 变化，实现用「写前写后快照比对」或事务标记）。

**`--result-file`**：兼容层——同时把 `result` 写到该路径（默认 `command_result.txt`，传 `""` 关闭）。

**测试**：按语义重写，用例清单参照 `tests/test_manager.py`（86 行）、`tests/test_soft_delete.py::TestReviveByAdd`（复活契约）。

---

## 2. `usm project`

**语义对应**：`project_issues.py::project`（DR-6：行为对齐，输出格式自由）

```
usm project [--dry-run] [--json]
```

- 无 `GITHUB_TOKEN` → 结果/日志 `缺少 GITHUB_TOKEN 或 GITHUB_REPOSITORY，跳过 Issue 投影`，**exit 1**（与现 CLI 一致）
- 幂等对账四分支：创建 / 回填 tracking / 重开+更新 / 墓碑化；每步输出人类动作行
- 版本帖发布：账本 `discussions` 末条版本 == 当前版本 → 跳过；否则发布并追加账本（失败不阻断，下轮补发）
- 写 registry 后 **必须原子落盘**（`registry.Save`）
- `--dry-run`：打印将执行的动作，不写任何文件（**新增能力**，供内容仓 CI 做只读校验）

**`--json`**：`{ "actions": ["创建 Issue #7：…"], "changed": true, "dirty_registry": true, "errors": [] }`（`errors` 非空 = 有 GraphQL/网络错误但未中断，见 §7.5）

**测试**：按语义重写，用例清单参照 `tests/test_projector.py`（319 行，含 noop 幂等/账本四分支/墓碑/复活重开）。

---

## 3. `usm build`

**语义对应**：`build_pages.py::main`（DR-6：行为对齐，输出格式自由）

```
usm build [--out dist] [--batch <n>=10] [--commands-per-page <n>=5] [--json]
```

- 令牌**可空**：空 → stderr 打印 `WARNING: 未设置 GITHUB_TOKEN，跳过讨论统计拉取，页面降级渲染`，`stats=nil` 降级渲染，exit 0
- 产物（全部在 `--out` 下）：
  1. `index.html`（首屏 `--batch` 张卡片 + `#scriptList[data-total]` + `#loadMore/#listSentinel` 当且仅当 total>batch）
  2. `scripts.json`（全量卡片 `[{type, html}]`）
  3. `scripts/<id>.html`（有版本帖账本且拉取成功 → 版本切换面板；否则回退 Issue 面板）
  4. `commands/page-1..N.html`（每页 `--commands-per-page`，新→旧）+ `commands/index.html` 跳转 + 清理陈旧分页
  5. `build-warnings.txt`：**有问题才写，无问题删除旧文件**
- 幂等：**同一输入连续两次构建，产物字节一致**（幂等，快照依据）

**`--json`**：`{ "pages": 8, "warnings": ["…"], "changed": true }`

**测试**：按语义重写，用例清单参照 `tests/test_pages.py`（753 行，88 用例）+ 快照基线。

---

## 4. `usm cleanup`

**语义对应**：`panel_cleanup.py::process`（DR-6：行为对齐，输出格式自由）

```
usm cleanup [--keep <n>=10] [--archive archive/commands.json] [--json] [--apply]
```

- 流程：分页拉 Issue#1 评论（100/页）→ 按「`/` 开头为命令，其后机器人评论归入同组」分组 → 保留**最近 `--keep` 组** → 其余**先按 comment id 幂等写归档，再逐条删除**
- **安全序**：拉取失败 → exit 1 且**零改动**；归档写成功即视为本任务完成（删除失败仅记录，下轮补删）
- `--apply` 缺省时为 **dry-run**（打印将删除/归档的数量，不落盘）——Action 里必须显式传 `--apply`（防误触发）
- 归档文件契约：`{ "schema": 1, "commands": [ { command_id, author, command, created_at, results: [{id, author, body, created_at}], archived_at } ] }`（旧→新）

**`--json`**：`{ "total_groups": 6, "archived": 0, "deleted": 0, "failed": 0, "changed": false, "errors": [] }`（`errors` 非空 = 有 GraphQL/网络错误但未中断，见 §7.5）

**测试**：按语义重写，用例清单参照 `tests/test_panel_cleanup.py`（165 行，幂等/失败重试/未超阈值不动）。

---

## 5. 辅助子命令

### `usm snapshot <check|update>`
- `check`：与 `tests/snapshot/` 新项目基线逐字符比对（**换行归一化**：读入时 `\r\n→\n`），不一致打印 unified diff，exit 1
- `update`：写回基线（等价原 `UPDATE_GOLDEN` 机制）

### `usm doctor [--check] [--json]`
- 校验：registry 结构与 schema 版本、每条 `self` 脚本的 `scripts/self/<id>/index.js` 存在、每条 `synced` 的 `scripts/synced/<id>/script.user.js` 存在、`dist/<id>.user.js` 与 `enabled/deleted` 一致、孤儿目录（盘上有源码但 registry 无条目）、`archive/commands.json` 可解析且无重复 `command_id`（SPEC-DATA §6 第 5 条）
- `--check`：有问题 exit 1 并逐条打印（供内容仓 `validate` workflow）
- `--json`：`{ "problems": ["…"], "ok": false }`

### `usm version`
- 打印版本（构建时 `-ldflags` 注入）+ `registry-schema-version`；Action 用它做启动自检。

---

## 6. 行为对照表（语义验收用）

| 语义 | Python 源 | Go 落点 | 验收测试 |
|---|---|---|---|
| 面板/权限/解析五条早退 | `manager.py:44-68` | `internal/cli/runcmd` | `test_manager` |
| 统一 panic→回帖 | `commands/__init__.py:register` | `internal/commands.Wrap` | panic 注入用例 |
| 软删除→同源复活 | `commands/add.py::add_sync` | `internal/commands/add` | `TestReviveByAdd` 3 用例 |
| 幂等投影判等 | `project_issues.py:274-280`（标题+正文比较） | `internal/projector` | `test_projector` noop + snapshot |
| 墓碑不带 marker | `issue_page.py::tombstone_body` | `internal/issuepage` | `test_issue_body::test_tombstone_has_no_marker` |
| registry 原子写 | `registry.py::save_registry` | `internal/registry.Save` | `test_registry` + 并发新用例 + round-trip 幂等（I-2） |
| 版本帖幂等账本 | `project_issues.py::ensure_discussion_post` | `internal/projector` | `test_projector` 四分支 |
| 清理幂等（按 comment id） | `panel_cleanup.py::merge_archive` | `internal/cleanup` | `test_panel_cleanup` 幂等 2 用例 |
| 构建回退（无版本帖→Issue 面板） | `build_pages.py::detail_version_panel` | `internal/pages` | `test_pages::TestDetailVersionSwitch` |
| 首屏 10 + scripts.json | `build_pages.py::build_index` | `internal/pages` | `test_pages::TestIndexLazyLoad` |
| 白名单后缀匹配 | `sources/base.py::matches` | `internal/sources` | `evil.com/path/greasyfork.org` 不匹配 |
| 相对时间中文单位 | `issue_stats.py::relative_time` | `internal/times` | `test_issue_stats::test_relative_time_units` |

---

## 7. 明确**不要**做的事

1. 不要实现 `usm push/commit`——提交属于内容仓 workflow。
2. 不要复刻 `manager.py` 的 Windows stdout 重包装（Go 不需要）。
3. 不要在 `build` 里删除或改写 `registry.json`（它只读 registry；`project`/`run-command` 才写）。
4. 不要给 `run-command` 加「非 exit 0 表示业务失败」的新语义——那会让内容仓回帖逻辑失效（退出码是对外契约，迁移期与回退路径必须一致）。
5. 不要静默吞掉 GraphQL 错误：`project`/`cleanup` 需要 stderr 警告 + `--json` 的 `errors` 字段（现有 Python 只打 stderr，Go 至少同等）。
6. 不要在分发器里为新命令/新参数写特判分支——新增走注册器与既有 flag 机制（扩展点见 `SPEC-ARCH-TEST.md §7`，DR-7：核心分发不随功能增长）。
