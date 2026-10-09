# project —— 项目投影

## 定位

面板命令 `/project` 与 CLI `usm project`：遍历账本，对账投影到 GitHub Issues / 版本帖——确保每个活跃脚本有配套 Issue（创建/更新/关闭）。

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
- 错误: 0
```

## 幂等/边界语义

- 对账是幂等的：重复投影只补差（已存在且一致的 Issue 不动）。
- **前置**：`GH_REPO_OWNER` 与 `GH_CLIENT` 都缺失 → 失败「project 命令需要配置 GH_REPO_OWNER 或 GH_CLIENT」。
- 未配置 GitHub API 时仅输出统计，回帖带「⚠️ GitHub API 未配置，仅输出统计」。

## 代码指针

`internal/commands/project.go`、`internal/projector/`。
