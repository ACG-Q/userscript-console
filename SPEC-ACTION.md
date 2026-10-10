# SPEC · GitHub Action（`action.yml`）

> 位置：**`userscript-console` 仓库根**（DR-1，不单开仓）
> 类型：composite（`runs.using: composite`），仅 Linux runner（D-07）
> 版本策略：`inputs.version` 缺省 = 文件内内置常量（随 release 更新），调用方 pin commit-sha

---

## 1. 接口定义（**两仓契约，改动必须双向同步**）

### 1.1 Inputs

| 名称 | 必填 | 默认 | 说明 |
|---|---|---|---|
| `command` | ✅ | — | `run-command` \| `project` \| `build` \| `cleanup` \| `doctor` |
| `github-token` | ✅ | — | composite 无法读 `secrets.*`，**必须显式传入**（通常 `${{ secrets.GITHUB_TOKEN }}`） |
| `comment-body` | | `''` | 仅 `run-command`；调用方传 `${{ github.event.comment.body }}` |
| `comment-user` | | `''` | 仅 `run-command`；`${{ github.event.comment.user.login }}` |
| `repo-owner` | | `${{ github.repository_owner }}` | 门禁比较对象 |
| `issue-number` | | `'1'` | 仅 `run-command` |
| `registry-schema-version` | | `'1'` | 不匹配 → 失败（跨仓 schema 防漂移） |
| `registry-path` | | `''` | 注册表文件路径（相对数据根或绝对）；env `USM_REGISTRY` 透传 |
| `scripts-dir` | | `''` | 脚本目录；env `USM_SCRIPTS_DIR` 透传 |
| `dist-dir` | | `''` | 分发/站点产物目录；env `USM_DIST_DIR` 透传（`@downloadURL` 跟随） |
| `archive-path` | | `''` | 命令归档文件路径；env `USM_ARCHIVE_PATH` 透传 |
| `comment-id` | | `''` | 仅 `run-command`；门禁未授权时要删除的评论数字 ID（env `COMMENT_ID`） |
| `post-reply` | | `'true'` | 仅 `run-command`；执行结果是否回帖到 Issue（env `POST_REPLY`） |
| `pages-out` | | `''` | 仅 `build`；构建后把站点产物搬入该目录（env `USM_PAGES_OUT`）；空=不搬移 |
| `keep` | | `'10'` | 仅 `cleanup`，保留的命令组数 |
| `apply` | | `'false'` | 仅 `cleanup`；**必须显式 `true` 才真正删除**（默认 dry-run） |
| `version` | | `''` | 二进制版本别名（等价 `binary-version`，兼容早期调用方） |
| `use-binary` | | `'false'` | `true`=二进制模式（缺省自动推导版本+校验和）；`false`=源码模式（默认） |
| `binary-version` | | `''` | 二进制版本号（不含 v）。缺省时从 ACTION_REF 推导：精确 `vX.Y.Z` tag 直接用；大版本 `vN` 取最新 `vN.x` release。显式传值可覆盖。 |
| `binary-sha256` | | `''` | 二进制 sha256（64 位十六进制）。缺省时从同 release 的 `checksums.txt` 自动取 `usm-linux-amd64` 校验和。显式传值可收紧。 |

> **env 透传与防注入**：上表 7 个新 input（路径 4 + `comment-id`/`post-reply`/`pages-out`）
> 一律只经 `env` 进入 usm（`USM_REGISTRY`/`USM_SCRIPTS_DIR`/`USM_DIST_DIR`/`USM_ARCHIVE_PATH`/
> `COMMENT_ID`/`POST_REPLY`/`USM_PAGES_OUT`），**绝不内插进 shell 命令行**
> （SPEC-ACTION §4.2 同一理由）；usm 侧按 flag > env > 默认 解析（SPEC-CLI §0.4/§0.5）。

### 1.2 Outputs

| 名称 | 类型 | 含义 |
|---|---|---|
| `authorized` | `'true'`/`'false'` | 仅 `run-command`：权限门禁结果，供调用方 `if:` skip |
| `changed` | `'true'`/`'false'` | 本次是否改动了数据文件（决定调用方要不要 commit） |
| `result` | string | 回帖正文（人类可读，多行） |
| `warnings` | string | `build` 的 `build-warnings.txt` 内容（单行化，`%0A` 分隔） |
| `version` | string | usm 版本（二进制模式为二进制自报；源码模式经 `USM_VERSION=action_ref` 解析，非语义化 ref 回落仓库 `VERSION` 文件） |

---

## 2. `action.yml` 完整骨架（v0 = Python 实现；v1 换二进制，**接口不变**）

```yaml
name: 'Userscript Console'
description: '油猴脚本控制台：命令执行 / Issue 投影 / 站点构建 / 面板清理'
branding: { icon: 'terminal', color: 'green' }

inputs:
  command:               { required: true }
  github-token:          { required: true }
  comment-body:          { default: '' }
  comment-user:          { default: '' }
  repo-owner:            { default: '${{ github.repository_owner }}' }
  issue-number:          { default: '1' }
  registry-schema-version: { default: '1' }
  keep:                  { default: '10' }
  apply:                 { default: 'false' }
  version:               { default: '0.1.0' }   # ← release 时由 CI 改写并提交

outputs:
  authorized: { value: '${{ steps.run.outputs.authorized }}' }
  changed:    { value: '${{ steps.run.outputs.changed }}' }
  result:     { value: '${{ steps.run.outputs.result }}' }
  warnings:   { value: '${{ steps.run.outputs.warnings }}' }

runs:
  using: composite
  steps:
    # ── v0（阶段 2）──────────────────────────────────────
    - name: Setup Python
      uses: actions/setup-python@…            # SHA pin（沿用内容仓惯例）
      with: { python-version: '3.11', cache: 'pip' }
    - name: Install deps
      shell: bash
      run: pip install -r "${{ github.action_path }}/requirements.txt"
    - name: Run
      id: run
      shell: bash
      env:
        GITHUB_TOKEN: ${{ inputs.github-token }}
        GITHUB_REPOSITORY: ${{ github.repository }}
        COMMENT_BODY: ${{ inputs.comment-body }}
        COMMENT_USER: ${{ inputs.comment-user }}
        REPO_OWNER: ${{ inputs.repo-owner }}
        ISSUE_NUMBER: ${{ inputs.issue-number }}
      run: |
        set -euo pipefail
        USM="${{ github.action_path }}/console.py"
        ARGS=(--root "$GITHUB_WORKSPACE" --json)
        case "${{ inputs.command }}" in
          run-command) ARGS+=(run-command) ;;
          project)     ARGS+=(project) ;;
          build)       ARGS+=(build) ;;
          cleanup)     ARGS+=(cleanup --keep "${{ inputs.keep }}"
                              ${{ inputs.apply == 'true' && '--apply' || '' }}) ;;
          doctor)      ARGS+=(doctor --check) ;;
          *) echo "未知 command: ${{ inputs.command }}"; exit 2 ;;
        esac
        python "$USM" "${ARGS[@]}" > out.json || rc=$?
        … # 解析 out.json → $GITHUB_OUTPUT（authorized/changed/result/warnings）

    # ── v1（阶段 4，接口不变）───────────────────────────
    # - name: Fetch binary
    #   shell: bash
    #   run: |
    #     URL="https://github.com/${{ github.repository_owner }}/userscript-console/releases/download/v${{ inputs.version }}/usm-linux-amd64"
    #     curl -fsSL -o "$RUNNER_TEMP/usm" "$URL"
    #     echo "<sha256>  $RUNNER_TEMP/usm" | sha256sum -c -     # ← 内置校验，缺一不可
    #     chmod +x "$RUNNER_TEMP/usm"
```

**实现要求**：
1. `run` 步骤必须 `set -euo pipefail`；
2. 子命令用 **case 白名单**，未知值 `exit 2`（不要拼进 shell 字符串执行 —— 现有 `COMMENT_BODY` 由 env 传递正是为了防注入，**必须保持 env 传递，绝不能内插到命令行**）；
3. `result` 可能多行：写 `$GITHUB_OUTPUT` 用 heredoc 语法（`result<<EOF … EOF`）；
4. `changed`/`authorized` 只能是 `'true'`/`'false'` 字符串。

---

## 3. 调用方示例（核心步骤示意）

> **完整可运行形态以 `userscripts/SPEC-WORKFLOWS.md §2` 为准**（含提交后 `gh workflow run deploy-pages.yml` 派发部署、各失败路径 `set -euo pipefail` 与 `!cancelled()` 回帖——见本节末「必须保留的三个细节」）。本节示例与之冲突时以 SPEC-WORKFLOWS 为准，两者必须同 PR 同步。

### 3.1 命令处理（替换现 `issue-commands.yml`）

> 门禁/回帖已收进 action：`comment-id` 未授权时自动删评（合并 gate.py）、`post-reply` 自动回帖执行结果（合并 reply.py）——不再需要 workflow 侧的 gate 步骤与 Reply 步骤（设计 D7）。

```yaml
name: Issue Commands Manager
on: { issue_comment: { types: [created] } }
permissions: { contents: write, issues: write, actions: write, discussions: write }
concurrency: { group: issue-commands, cancel-in-progress: false }

jobs:
  execute:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    if: ${{ !github.event.issue.pull_request && github.event.issue.number == 1 }}
    steps:
      - uses: actions/checkout@<sha>

      - name: Run command                # 门禁 + 执行 + 结果回帖都在 action 内
        id: cmd
        uses: acg-q/userscript-console@<sha>        # ← 或 ./（同仓自测）
        with:
          command: run-command
          github-token: ${{ secrets.GITHUB_TOKEN }}
          comment-body: ${{ github.event.comment.body }}
          comment-user: ${{ github.event.comment.user.login }}
          comment-id: ${{ github.event.comment.id }}   # 未授权 → 自动删该评论
          issue-number: ${{ github.event.issue.number }}
          post-reply: 'true'                              # 执行结果回帖

      - name: Project issues           # 投影仍需跑（registry 可能已变）
        id: proj
        if: steps.cmd.outputs.authorized == 'true'
        uses: acg-q/userscript-console@<sha>
        with: { command: project, github-token: '${{ secrets.GITHUB_TOKEN }}' }

      - name: Commit changes           # ⚠️ 路径白名单（审查项：禁止 git add .）
        id: commit
        if: ${{ !cancelled() && steps.cmd.outputs.authorized == 'true' && steps.cmd.outputs.changed == 'true' && steps.cmd.outcome == 'success' }}
        run: |
          git config user.name "github-actions[bot]"
          git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
          git add registry.json scripts dist archive
          git diff --staged --quiet || (git commit -m "Apply command: ${{ github.event.comment.user.login }}" && git push)

      - name: Trigger site deploy      # commit/push 的 token 不触发 workflow → 用 PAT 显式派发
        if: ${{ !cancelled() && steps.commit.outcome == 'success' }}
        env: { GITHUB_TOKEN: '${{ secrets.GITHUB_TOKEN }}' }
        run: gh workflow run deploy-pages.yml

      - name: 失败兜底回帖              # 失败也回帖（现规范）
        if: ${{ !cancelled() && (steps.cmd.outcome == 'failure' || steps.proj.outcome == 'failure' || steps.commit.outcome == 'failure') }}
        env: { GITHUB_TOKEN: '${{ secrets.GITHUB_TOKEN }}' }
        run: |
          gh issue comment ${{ github.event.issue.number }} \
            -R ${{ github.repository }} \
            --body "⚠️ 执行/提交/派发环节失败，请查看 Actions 日志"
```

### 3.2 二进制零手填调用示例（v1.1.0+）

```yaml
# 源码模式（默认，始终跟随 commit 源码，无需版本配置）
- uses: acg-q/userscript-console@v1.1.0
  with:
    command: build
    github-token: ${{ secrets.GITHUB_TOKEN }}

# 二进制模式 + 自动推导版本（仅写 tag，不写 binary-version/sha256）
- uses: acg-q/userscript-console@v1.1.0
  with:
    command: build
    github-token: ${{ secrets.GITHUB_TOKEN }}
    use-binary: true
    # binary-version 与 binary-sha256 自动从 @v1.1.0 推导

# 二进制模式 + 大版本 tag 自动解析（@v1 取最新 v1.x）
- uses: acg-q/userscript-console@v1
  with:
    command: build
    github-token: ${{ secrets.GITHUB_TOKEN }}
    use-binary: true

# 显式 pin sha + 显式二进制版本（内容仓安全惯例）
- uses: acg-q/userscript-console@<40位sha>
  with:
    command: build
    github-token: ${{ secrets.GITHUB_TOKEN }}
    use-binary: true
    binary-version: '1.1.0'
    binary-sha256: '<64位十六进制>'
```

### 3.2 其余三条（骨架一致，仅 `command` 与提交路径不同）

> 表中 workflow 名为**逻辑名**（文件名保持现状：`deploy-pages.yml`、`sync-scheduled.yml`、`cleanup-panel.yml`，见 SPEC-WORKFLOWS 头注）；`gh workflow run` 一律用实际文件名。

| workflow | steps | 提交路径 |
|---|---|---|
| `deploy.yml` | `command: build` → `_site` 组装 → `actions/deploy-pages` | 无提交（HTML 不入库） |
| `sync.yml` | `command: run-command`（`comment-body: /sync-all`、`comment-user: ${{ github.repository_owner }}`）→ `project` → commit | `registry.json scripts dist` |
| `cleanup.yml` | `command: cleanup`（`apply: true`、`keep: 10`）→ commit → `gh workflow run deploy-pages.yml` | `archive`（即 `archive/commands.json`，白名单口径见 SPEC-WORKFLOWS §4） |

> ⚠️ **必须保留的三个细节**：`concurrency` 串行组、`timeout-minutes`、`GITHUB_TOKEN` 的 push 不触发 workflow 所以要 `gh workflow run` 显式派发（沿用现 `.github/workflows/*` 的注释约定）。

---

## 4. 安全要求（逐条硬性）

1. **二进制校验**：v1 下载必须 `sha256sum -c`。校验和来源：
   - 调用方显式传 `binary-sha256`（内容仓 sha-pin 惯例，收紧信任链）；
   - **缺省时自动从同 release 的 `checksums.txt` 取 `usm-linux-amd64` 校验和**（与二进制同源，防篡改/传输损坏）。
   校验失败 → `exit 1`，提示从 release 资产更新值。
2. **下载 URL 固定仓库**：`https://github.com/ACG-Q/userscript-console/...`（不使用 `${{ github.repository_owner }}`，后者为调用方 owner，外部调用必 404）。
3. **不内插用户输入进 shell**：`comment-body` 只经 `env`（现 Python 设计已如此，见 `docs/design.md` §安全，**不得回归**）。
4. **token 最小权限**：Action 不自声明 `permissions`（composite 无法声明），调用方声明；README 明确写出每条 workflow 需要的最小集合。
5. **`secrets` 不落盘**：`GITHUB_TOKEN` 只进 `env`，不写进任何输出文件；`--json` 输出里不得包含 token。
6. **`cleanup` 默认 dry-run**：`apply` 必须显式 `true`，防止误删 Issue 评论。
7. **pin 惯例**：本仓内部对第三方 action（`actions/checkout`、`setup-python`）按 commit-sha pin（沿用内容仓既有约定）。

---

## 5. 版本与发布

| 机制 | 规则 |
|---|---|
| 语义化 tag | `vMAJOR.MINOR.PATCH`；**不兼容的 inputs/outputs 变更 → MAJOR** |
| 移动大版本 tag | `v1` 每次 release 后 `git tag -f v1 && git push -f origin v1`（release CI 自动做） |
| 调用方 pin | **内容仓用 commit-sha**（安全惯例，与仓内 `actions/checkout@<sha>` 一致）；外部用户可用 `@v1` 或 `@vX.Y.Z`（零手填二进制） |
| 二进制版本自动推导（v1.1.0+） | 1) `binary-version` 显式值 → 用之<br>2) ACTION_REF 精确 `vX.Y.Z` → 去 v 使用<br>3) ACTION_REF 大版本 `vN` → GitHub API 取最新 `vN.x` release<br>4) 其他（sha/分支/本地） → 明确报错提示显式传版本 |
| 二进制校验和自动推导（v1.1.0+） | 1) `binary-sha256` 显式值 → 用之<br>2) 缺省时从同 release `checksums.txt` 取 `usm-linux-amd64` 校验和 |
| usm 版本解析链（设计 D3） | `-ldflags` 注入 > env `USM_VERSION`（= `github.action_ref`，仅精确 `vX.Y.Z` 匹配语义化才生效）> 内嵌 `VERSION` 文件 > `"dev"`；pin sha / `@v1` 源码模式显示的是「该 commit 时点最近一次发版」的 VERSION 值 |
| 自测 | 本仓 CI 用 `uses: ./` 跑 5 个 command 的冒烟（无需发布） |
| 内置版本 | `action.yml` 的 `inputs.version`/`binary-version`/`binary-sha256` 默认均为空，**不再回填**；缺省靠上述规则推导 |

**接口变更流程**：改 `inputs/outputs` → 先改本文件 §1（作为契约评审）→ 同 PR 改 `userscripts/SPEC-WORKFLOWS.md` 对应片段 → 两仓测试绿才合并.

---

## 6. 验收清单（每个 Action 版本发版前）

- [ ] `uses: ./` 冒烟：5 个 command 各一次，outputs 齐全
- [ ] `comment-body` 含反引号/`$(...)`/换行时**不被 shell 解释**（注入回归用例）
- [ ] `registry-schema-version: 99` → 失败且信息含 `registry schema`
- [ ] `cleanup` 不传 `apply` → 无任何删除（dry-run 验证）
- [ ] `result` 多行内容经 `$GITHUB_OUTPUT` heredoc 传递后仍完整（含 emoji 与中文）
- [ ] `comment-id`/`post-reply`：未授权删评一次（删除失败仅 warning）、`post-reply: 'false'` 零回帖、非法布尔 env → exit 2
- [ ] `pages-out`：搬移成功后 dist 清空且 `*.user.js` 落 `<out>/<dist 段>/`；空值纯 build；搬移失败 → warnings 追加且 exit 1
- [ ] v1：`sha256sum -c` 故意改坏 → 必须失败
