# sync —— 上游同步

## 定位

面板命令 `/sync`（单个）与 `/sync-all`（批量）：从 `SourceURL` 抓取最新代码更新账本与产物。`/sync all` 与 `/sync-all` 等价。

## 用法

```
/sync <id|名称|来源URL>
/sync-all          ← 等价 /sync all
```

## 参数

| 位置参数 | 说明 |
|---|---|
| key | 单个脚本的 id/名称/来源 URL；`all` 或缺省 → 批量模式 |

## env

无专属 env（批量抓取失败计入 warnings，不中断）。

## 回帖示例

```
✅ 已同步脚本 "Foo" v1.2 → v1.3
📭 所有脚本已是最新版本
📭 未同步任何脚本（1 个抓取失败）
📭 没有需要同步的脚本
```

## 幂等/边界语义

- **单个模式前置校验**（顺序即回帖顺序）：未找到 → 失败；`self` 脚本 →「脚本 %q 是自写脚本，无法同步」；缺 `SourceURL` → 失败；已停用 → 失败；上游版本 == 当前版本 → 「已是最新版本 v%s」（no-op）。
- **更新落库**：版本/描述/作者/Match/Grant/时间戳整体刷新，changelog 头部插入「同步更新 v旧 → v新」（保留最多 10 条），源码与分发产物同步写盘。
- **批量模式**：只同步 `synced` + 启用 + 未删除 + 有 `SourceURL` 的条目；单个失败跳过不中断；全部最新 → 幂等 no-op；有失败时结果带 warning「N 个脚本抓取失败，已跳过」。

## 代码指针

`internal/commands/sync.go`（`runSyncOne`/`runSyncAll`/`applySynced`）。
