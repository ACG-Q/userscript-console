# cli —— usm 子命令汇总

## 定位

`usm` 是本仓的层 2 CLI 入口（GitHub Action `acg-q/userscript-console` 底层即它）。本文汇总顶层子命令；面板命令的详细语义见各自文档页。

## 用法

```
usm <command> [flags]

run-command    执行命令面板评论中的 /command（回帖语义）
project        registry → Issues/版本帖 对账投影
build          构建站点产物（dist/）
cleanup        归档并清理命令面板历史评论（默认 dry-run，--apply 才执行）
doctor         数据一致性自检（--check 供 CI）
snapshot       快照基线 <check|update>
version        打印版本与 registry schema 版本
help           帮助
```

全局约定：默认工作目录即数据根（含 registry.json）；`--root <dir>` 可显式指定（或 env `USM_ROOT`）。

## 子命令要点

### run-command

面板执行入口：解析 `COMMENT_BODY` 中的 `/cmd`，鉴权后执行并回帖。

| env | 说明 |
|---|---|
| `COMMENT_BODY` / `COMMENT_USER` / `ISSUE_NUMBER` | 必填：评论内容、作者、面板 issue 号 |
| `REPO_OWNER` / `GH_REPO_OWNER` | 仓库 owner |
| `PAGES_BASE` | 供 `/build` 等需要站点基址的命令 |
| `AUTHOR_NAME`（默认 `usm`）/ `AUTHOR_NAMESPACE` | 自写脚本作者回填 |
| `GITHUB_TOKEN` / `GITHUB_REPOSITORY` | 回帖与产物提交 |

### doctor

```
usm doctor [--check] [--json] [--root <dir>]
```

数据一致性自检（registry 与源码/分发产物双删对账）；`--check` 供 CI 失败即退出 1。

### build / cleanup / project

见各自的文档页（[build](build.html)、[cleanup](cleanup.html)、[project](project.html)）。

### snapshot

```
usm snapshot check     ← 校验 tests/snapshot 基线与当前代码一致
usm snapshot update    ← 重新生成基线
```

开发者工具：对固定输入跑整站构建、比对/更新基线目录。不读写账本。

### version / help

```
usm version     → usm <ver> + registry-schema-version <N>
usm help        → 帮助
```

## 退出码契约

| 码 | 含义 |
|---|---|
| 0 | 成功（含业务失败的结果文本） |
| 1 | 致命（数据损坏/网络不可恢复/schema 不匹配） |
| 2 | 用法错误（未知子命令/缺参数） |

## 幂等/边界语义

- `USM_REGISTRY_SCHEMA`：action.yml 声明依赖的 registry 格式，与二进制内置不符 → exit 1 拒跑（防新旧混用）；未设置跳过。
- `--json` 时所有子命令输出 `{authorized, changed, result, warnings}`（action.yml 全量走 jq）。

## 代码指针

`cmd/usm/main.go`。
