# rm —— 软删除与复活契约

## 定位

面板命令 `/rm`：软删除脚本——条目保留（支持复活），源码与分发产物移除。

## 用法

```
/rm <id|名称|来源URL>
```

## 参数

| 位置参数 | 说明 |
|---|---|
| key | 同 `/info` 的匹配规则；缺省报错「用法: /rm <id|名称|来源URL>」 |

## env

无专属 env。

## 回帖示例

```
✅ 已软删除脚本 "Foo"（ID: ab12cd，支持 /add 复活）
```

## 幂等/边界语义

- **软删除（I-4）**：仅置 `Deleted=true` 并更新 `UpdatedAt`，条目保留在账本；`/list` 不再列出。
- **幂等**：已是删除状态 → 「脚本 %q 已是已删除状态（ID: %s）」，不重复处理。
- **复活**：同 `SourceURL` 再次 `/add` 即恢复（见 [add](add.html)）。
- **部分失败**：registry 已改但源码/分发产物移除失败 → 回帖「❌ 已软删除脚本 %q，但源文件失败: %v」（成功标记保留，下轮 doctor 可发现）。

## 代码指针

`internal/commands/rm.go`、`internal/script.RemoveSource/RemoveDist`。
