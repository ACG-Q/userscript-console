# tests/snapshot —— 新项目自产快照基线（DR-6）

由 `usm snapshot check|update` 引擎（`internal/snapshot`）管理：

- `check`：与生成器输出逐字符比对（换行归一化 `\r\n→\n`），差异打印逐行 diff → exit 1
- `update`：写回基线（**变更必须人工审阅后随 PR 提交**）

基线内容（生成器在 cmd 层注册）：
- 投影快照：`issue/` —— BuildTitle/BuildBody/Tombstone/Discussion 的代表性输入输出
- registry 规范形态：`registry/` —— 规范化 JSON 样例
- 站点整页：`site/` —— index/detail/commands 的完整 HTML（模板回归锁）

自检：`go run ./cmd/usm snapshot check`（生成器接线后启用，见 CI test.yml TODO）。
