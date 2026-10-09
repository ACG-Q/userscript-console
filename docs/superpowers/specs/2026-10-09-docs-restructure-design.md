# 设计：双仓文档重构（命令文档 + 扩展开发文档 + 工具仓 Pages 发布）

- 日期：2026-10-09
- 状态：已批准（对话中逐节确认）
- 涉及仓：`userscript-console`（主）、`userscripts`（清理）

## 1. 背景与目标

- `userscripts/docs/` 是历史遗留：对外的 `docs/index.md`（脚本文档索引）与站点导航"文档"入口耦合，内部工作文档（reviews/、superpowers/）混杂其中。用户决定整删。
- `userscript-console` 缺少面向用户的命令文档与面向开发者的扩展文档；且其 `usm build` 的 docs 转换只扫 `docs/` 顶层，不支持子目录。
- 目标：
  1. 工具仓建立 `docs/commands/*.md`（转换为站点 docs 页）与 `docs/dev/*.md`（纯仓库文档，不转换）双轨文档；
  2. 转换逻辑支持 `docs/commands/` 子目录输出；
  3. 工具仓自举发布 GitHub Pages（`usm build` 构建，文档即时可访问）；
  4. 内容仓删除 `docs/` 并处理引用。

## 2. 转换逻辑改动（internal/pages）

- `loadDocs`：顶层 `*.md` 扫描不变；额外且仅额外增扫 `docs/commands/*.md`。其他子目录（含 `dev/`、`superpowers/`）一律跳过，加测试锁死该白名单行为。
- `index.md` 门禁不变：顶层 `docs/index.md` 缺失 → 整站不产 docs、nav `HasDocs=false`（防死链）。
- `docPage` 增加子目录维度；`commands/` 下页面输出键为 `commands/<slug>.html`。slug 冲突检测按完整输出路径判定。
- 渲染层级：`commands/` 下的页返回链接用 `../../index.html`，TOC 中其他 commands 页 href 带 `commands/` 前缀；顶层 docs 行为不变。
- `writeSite` 路径校验：从"拒绝一切 `/`"放宽为"允许恰好一级 `[a-z0-9-]+/` 前缀"；`..`、`\`、绝对路径、更深层级照旧拒绝。

## 3. Pages workflow（.github/workflows/pages.yml）

- 触发：`push: branches [master]` + `workflow_dispatch`。
- 权限：`contents: read`、`pages: write`、`id-token: write`；concurrency group `pages`。
- 构建步骤：checkout → setup-go（go-version-file）→ **CI 现场生成空 `registry.json`（不提交落仓，守住"工具仓不持有账本"边界）** → `go run ./cmd/usm build`（env：`GITHUB_REPOSITORY=ACG-Q/userscript-console`、`GITHUB_PAGES_URL=https://acg-q.github.io/userscript-console`）→ configure-pages → upload-pages-artifact（path `./dist`）→ deploy-pages。
- 用 `go run` 自举而非钉 `@v1` 二进制：文档站永远反映当前 master 的转换代码。
- 一次性手动前置：仓库 Settings → Pages → Source = GitHub Actions。
- 边界：workflow 不提交任何文件；空账本仅存在于 runner 临时工作区。

## 4. 文档内容清单（11 个 md + 内部档案）

### 4.1 docs/index.md（转换，落地页）

仓库定位简介、命令文档目录（相对链接 `commands/<slug>.html`——GitHub 网页浏览时失效属已知取舍）、dev 文档入口（GitHub 绝对 URL，因 dev 不转换）。

### 4.2 docs/commands/（9 个，全部转换）

每篇统一结构：定位（面板 `/cmd` vs CLI 用法）、用法、参数、env、回帖示例、幂等/边界语义、代码指针。

| 文件 | 覆盖 |
|---|---|
| add.md | `/add`：添加自写/远程脚本 |
| list.md | `/list`：脚本清单 |
| info.md | `/info`：单脚本详情 |
| rm.md | `/rm`：软删除与复活契约 |
| sync.md | `/sync`、`/sync-all`：上游同步 |
| project.md | `/project` + `usm project`：GraphQL 对账投影 |
| build.md | `/build` + `usm build`：dist 与整站生成 |
| cleanup.md | `/cleanup` + `usm cleanup`：面板清理 |
| cli.md | usm 顶层入口汇总：run-command、doctor、version、help、snapshot |

### 4.3 docs/dev/extending-sources.md（1 个，不转换）

新增网站脚本源三步走：实现 `Adapter`（`Type`/`MatchURL`/`Fetch`）→ `init()` 中 `register` → hostname 白名单（`MatchHostSuffix` 防绕过语义）+ fixture 回放测试 + 注册清单硬编码断言。以 `greasyfork` 适配器为范例。明确禁止在 `Detect` 或既有适配器中加 if/else 特判；direct 适配器不注册的原因说明。

### 4.4 docs/superpowers/（内部档案，不转换）

既有 specs/plans 保留为内部工作档案（与 `dev/` 同理天然不转换）；`2026-10-08-pages-ui-redesign-design.md` 归档保留。

## 5. 测试与基线

- 新增 pages 测试：`commands/` 转换产出正确（输出键、返回链接层级、TOC href 前缀）；`dev/` 与未知子目录不转换；`commands/<slug>.html` 形式的路径穿越（`..`、`\`、绝对路径、二级以上）拒绝；有 `commands/` 但无顶层 `index.md` 时整站仍不产 docs。
- `go run ./cmd/usm snapshot check`：确认 docs 页对 `scripts.json` 基线的影响；有变化则重生成基线并人工审 diff。
- 全量门禁：`go test ./...`、gofmt、vet、golangci-lint、覆盖率 ≥ 90%。

## 6. 内容仓清理（userscripts）

- `git rm -r docs/`（整删：index.md、reviews/、superpowers/）。
- `SPEC-DATA.md` 中 docs 相关行标注"已移除（2026-10-09）"；`BASELINE.md`/`CUTOVER.md`/`docs-ownership.md`/`PLAN.md` 等历史清单原样不动。
- 站点侧零改动：无 `dist/docs` → `HasDocs=false` → nav 自动隐藏"文档"（既有防死链契约）。
- 推送后确认 Validate/Deploy 双绿、线上无 docs 入口、主页无死链。

## 7. 验收标准

1. `usm build` 在工具仓根目录运行产出 `dist/docs/index.html` 与 `dist/docs/commands/*.html`（9 命令 + index），`docs/dev/` 与 `docs/superpowers/` 不出现在产物中。
2. 站点 `https://acg-q.github.io/userscript-console/docs/` 可访问，导航含"文档"入口，命令页间 TOC 互跳正常。
3. 工具仓 git 历史中无 `registry.json` 新增（边界守住）。
4. userscripts 仓 `docs/` 不存在，站点无"文档"导航项，`go test`/Validate/Deploy 全绿。
5. 全量门禁绿（含覆盖率 ≥ 90%、lint 0 issues、快照一致）。

## 8. 非目标

- 不改 `docs/dev/` 的转换行为（明确不转换）。
- 不为内容仓（userscripts）重建任何对外文档页。
- 不改 build 对缺失 registry 的容错语义（空账本由 CI 现场生成，不动工具代码）。
- 命令文档不覆盖内部 Go API（那是 godoc 的职责）。
