# build —— 构建站点

## 定位

面板命令 `/build` 与 CLI `usm build`：把账本编译为整站产物 `dist/`（脚本副本 + scripts.json + 首页 + 详情页 + 命令归档页 + 文档页），供 GitHub Pages 发布。

## 用法

```
usm build                ← CLI（常配 PAGES_BASE）
usm build --json
/build                   ← 面板
```

## 参数

无位置参数；`--json` 时输出 action.yml 用 JSON。

## env

| 变量 | 说明 |
|---|---|
| `PAGES_BASE` | 站点基址（面板模式缺失 → 失败「build 命令需要配置 PAGES_BASE 环境变量」） |
| `USM_ROOT` / `--root` | 数据根（含 registry.json），默认当前目录 |
| `GITHUB_TOKEN` | 可选：抓取 discussions 供站点讨论区；缺失降级不失败 |

## 回帖示例

```
✅ 构建完成：

- 已构建: 12 个脚本
- 已跳过: 3 个（已删除）
- 站点页面: 24 个

📦 站点基址: https://<owner>.github.io/<repo>/dist/
```

## 幂等/边界语义

- **幂等**：相同输入第二次构建 `changed=false`（内容级 diff 后再写盘）。
- **`env.Site` 为 nil**（单测/无站点场景）：只产出 `dist/` 脚本副本，不产整站页面。
- 已删除脚本跳过构建；源码读取失败计入错误数不中断。
- 整站构建含 `dist/docs/`：顶层 `docs/*.md` + `docs/commands/*.md` 转换为 HTML（`docs/index.md` 缺失则整站不产 docs）。

## 代码指针

`internal/commands/build.go`、`cmd/usm/site.go`、`internal/pages/`。
