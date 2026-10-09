# cleanup —— 面板清理

## 定位

面板命令 `/cleanup` 与 CLI `usm cleanup`：归档命令面板历史评论到 `archive/commands.json`，`--apply` 时删除不再保留的评论。

## 用法

```
usm cleanup                    ← dry-run（默认）
usm cleanup --apply            ← 实际执行
usm cleanup --keep N           ← 每命令组保留最近 N 条（默认 10）
/cleanup [--apply] [--keep N]  ← 面板
```

## 参数

| 参数 | 说明 |
|---|---|
| `--apply` | 实际删除；缺失为 dry-run。面板不带参时 env `USM_APPLY=true` 同效 |
| `--keep N` | 每命令组保留最近 N 条（env `USM_KEEP`）；N<=0 视为默认 10 |

## env

| 变量 | 说明 |
|---|---|
| `GITHUB_TOKEN` | 拉取与删除评论 |
| `GITHUB_REPOSITORY` | `owner/repo` |
| `USM_APPLY` / `USM_KEEP` | 面板模式的标志等价物 |

## 回帖示例

```
🧹 cleanup 完成

脚本总数: 12
拉取评论: 48
已删除评论: 30
归档路径: archive/commands.json
归档命令组: 9
归档条目: 48

⚠️ 当前为 dry-run，添加 --apply 执行实际删除
```

## 幂等/边界语义

- **归档先落盘再删评论**：删除失败时下轮只补删除（幂等键 `command_id`，孤儿组用首条 result id）。
- dry-run 只统计不删；`--keep N` 与归档合并逻辑共用 `cleanup.MergeArchive`。
- 无 `GHClient` 或无面板 issue 上下文时只做归档统计。

## 代码指针

`internal/commands/cleanup.go`、`internal/cleanup/`。
