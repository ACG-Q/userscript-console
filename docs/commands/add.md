# add —— 添加脚本

## 定位

面板命令 `/add`：往账本添加自写脚本（评论附代码块）或远程脚本（给来源 URL，抓取元数据并入库）。

## 用法

```
/add <来源URL>
/add            ← 评论后附一个 ``` 代码块
```

- 来源 URL 由 `sources.Detect` 识别站点类型（greasyfork / userscript_zone / github_gist / direct 兜底）。
- 无 URL 且无代码块 → 失败「添加自写脚本需要提供代码块，请在评论中包含 ```...``` 代码块」。

## 参数

| 模式 | 输入 | 产物类型 |
|---|---|---|
| 远程 | `/add <URL>` | `synced`，自动 `Fetch` 元数据，`SyncEnabled=true` |
| 自写 | `/add` + 代码块 | `self`，从 `==UserScript==` 头解析 Name/Version/Author/Match/Grant |

## env（CLI 直跑时）

| 变量 | 说明 |
|---|---|
| `COMMENT_BODY` / `COMMENT_USER` / `ISSUE_NUMBER` | 面板上下文（`usm run-command`） |
| `REPO_OWNER` / `GH_REPO_OWNER` | 仓库 owner（缺省时部分回帖链接退化） |
| `AUTHOR_NAME`（默认 `usm`）/ `AUTHOR_NAMESPACE` | 自写脚本作者头缺失时的回填 |
| `USM_REGISTRY_SCHEMA` | 声明账本 schema，防新旧二进制混用 |

## 回帖示例

```
✅ 已添加脚本 "Foo" v1.2（来源: https://greasyfork.org/scripts/1234，ID: ab12cd）
✅ 已添加自写脚本 "Bar" v0.3（ID: self01）
```

## 幂等/边界语义

- **重复添加**：同 `SourceURL` 已存在 → 失败「脚本已存在（ID: %s），无需重复添加」，不覆盖。
- **复活（I-4）**：同 `SourceURL` 条目已被 `/rm` 软删除 → 不报错，恢复 `Deleted=false`、`Enabled=true` 并重新抓取，回帖「✅ 已复活脚本 %q（来源: %s）」。
- **自写头块缺失**：「无法解析脚本头，请确保代码包含完整的 ==UserScript== 头块」。
- 添加成功即写 `registry.json` + 源码 + `dist/` 分发产物。

## 代码指针

`internal/commands/add.go`、`internal/sources/`（Detect 与各适配器）。
