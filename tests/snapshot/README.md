# tests/snapshot —— 新项目自产快照基线（DR-6）

由 `usm snapshot check|update` 引擎（`internal/snapshot`）管理，从语料
`tests/corpus/inputs/` 生成；本 README 属忽略文件（不参与孤儿判定）。

- `check`：与生成器输出逐字符比对（换行归一化 `\r\n→\n`），差异打印逐行 diff → exit 1
- `update`：写回基线（**变更必须人工审阅后随 PR 提交**）

当前基线内容：
- `issue/*` —— 投影标题/正文/墓碑/版本帖（I-1 输出稳定性锁）
- `registry/canonical.json` —— registry 规范化 JSON（I-2 字节稳定性锁）

后续追加：`site/*` 站点整页快照（pages 包接线后）。

自检：`go run ./cmd/usm snapshot check`（在仓库根目录执行）。
