# project —— 项目投影

## 定位

面板命令 `/project` 与 CLI `usm project`：遍历账本，两阶段对账投影到 GitHub Issues / 版本帖——阶段一按标题前缀匹配（**含已关闭**）回填/判等刷新 Issue（必要时重开），阶段二按发牌账本幂等发布版本帖。

## 用法

```
usm project            ← CLI
usm project --json     ← action.yml 模式
/project               ← 面板
```

## 参数

无位置参数；`--json` 输出 `{authorized, changed, result, warnings}`。

## env

| 变量 | 说明 |
|---|---|
| `GH_REPO_OWNER` | 仓库 owner（CLI 模式） |
| `GITHUB_REPOSITORY` / `GH_REPO` | `owner/repo` |
| `GITHUB_TOKEN` | GraphQL/REST 客户端；缺失 → 仅统计不操作 |
| `PAGES_BASE` | 投影内链的站点基址 |

## 回帖示例

```
📊 投影统计：

- 活跃脚本: 12
- 已删除: 3
- 无关联 Issue: 2

✅ 本次操作：
- 创建 Issue: 2
- 更新 Issue: 5
- 重开 Issue: 1
- 发版本帖: 3（跳过: 9）
- 错误: 0
```

## 幂等/边界语义

- **Issue 回填**：无关联 Issue 的脚本走**全量列表**（含 CLOSED）按标题前缀匹配——命中已关闭项先重开再更新，未命中才创建；整个阶段只拉一次列表。
- **判等刷新**：已关联 Issue 用 `GetIssue` 比对 title 与 body，全等则零写；有变更才重开（如需）+更新并 bump `updated_at`。
- **版本帖发牌**：账本末条版本==当前版本 → 跳过（零 API）；否则发帖（标题 `[版本号] 脚本名称 版本讨论`）、追加账本并立即回写 Issue（回写失败不计错，下轮判等自愈）。分类选择 q-a（Q&A）> 列表第一个。
- **Issue changelog**：表格 4 列 `版本 | 日期 | 说明 | 帖子`，帖子列按账本回链 `[讨论 #N →](URL)`，无发牌记录填 `—`。
- **changed**：创建/更新/重开/发帖任一 >0 才为 true；全等且发牌均当前 → changed=false。
- **前置**：`GH_REPO_OWNER` 与 `GH_CLIENT` 都缺失 → 失败「project 命令需要配置 GH_REPO_OWNER 或 GH_CLIENT」。
- 未配置 GitHub API 时仅输出统计，回帖带「⚠️ GitHub API 未配置，仅输出统计」。

## 代码指针

`internal/commands/project.go`、`internal/projector/`。
