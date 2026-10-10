// Command usm 是油猴脚本控制台 CLI（userscript-console 的层 2 入口）。
//
// 退出码契约（SPEC-CLI §0.2）：
//
//	0 = 成功（含业务失败的结果文本）
//	1 = 致命（数据损坏/网络不可恢复/参数必填缺失）
//	2 = 用法错误（未知子命令/缺参数）
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/acg-q/userscript-console/internal/buildinfo"
	"github.com/acg-q/userscript-console/internal/cli"
	"github.com/acg-q/userscript-console/internal/commands"
	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/parser"
	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/snapshot"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}

	// registry schema 跨仓防漂移（SPEC-CLI §0.4）：action.yml 用
	// USM_REGISTRY_SCHEMA 声明调用方依赖的 registry 格式，与本二进制内置
	// 版本不符即拒绝执行，避免用旧二进制读写新格式数据。
	// 只校验触碰 registry 的五个子命令；version/help/snapshot 不读写数据。
	switch args[0] {
	case "run-command", "project", "build", "cleanup", "doctor":
		if rc := checkRegistrySchema(args); rc != 0 {
			return rc
		}
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("usm %s\nregistry-schema-version %d\n", buildinfo.Version(), registry.SchemaVersion)
		return 0
	case "help", "--help", "-h":
		usage(os.Stdout)
		return 0
	case "snapshot":
		return snapshot.Run("tests/snapshot", args[1:])
	case "doctor":
		return doctorRun(args[1:])
	case "run-command":
		return runCommandRun(args[1:])
	case "project":
		return projectRun(args[1:])
	case "build":
		return buildRun(args[1:])
	case "cleanup":
		return cleanupRun(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "ERROR: 未知子命令 %q\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

// hasJSONFlag 判断参数里是否带 --json。
// action.yml 对**所有**子命令都追加 --json 并用 jq 解析 stdout，
// 因此每个子命令都必须自己识别它（见 emitResult）。
func hasJSONFlag(args []string) bool {
	for _, a := range args {
		if a == "--json" {
			return true
		}
	}
	return false
}

// checkRegistrySchema 校验 USM_REGISTRY_SCHEMA 与 registry.SchemaVersion 一致。
// 未设置 → 跳过（本地手跑不强制）；不匹配 → stderr ERROR + exit 1（SPEC-ARCH-TEST §6）。
// --json 时 stdout 仍须输出合法 JSON，否则 action.yml 的 jq/heredoc 会连带失败。
func checkRegistrySchema(args []string) int {
	want := strings.TrimSpace(cli.EnvOr("USM_REGISTRY_SCHEMA", ""))
	if want == "" {
		return 0
	}
	if got, err := strconv.Atoi(want); err != nil || got != registry.SchemaVersion {
		msg := fmt.Sprintf("registry schema 不匹配：调用方声明 %q，本二进制要求 %d", want, registry.SchemaVersion)
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", msg)
		if hasJSONFlag(args) {
			if err := writeJSON(os.Stdout, map[string]any{
				"authorized": false,
				"changed":    false,
				"result":     "❌ " + msg,
				"warnings":   []string{},
				"version":    buildinfo.Version(),
			}); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: 写 JSON 失败: %v\n", err)
			}
		}
		return 1
	}
	return 0
}

// ── doctor ──────────────────────────────────────────────────

func doctorRun(args []string) int {
	root, check, asJSON := ".", false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--check":
			check = true
		case "--json":
			asJSON = true
		case "--root":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "ERROR: --root 缺少参数")
				return 2
			}
			i++
			root = args[i]
		default:
			fmt.Fprintf(os.Stderr, "ERROR: doctor 未知参数 %q\n", args[i])
			return 2
		}
	}

	// action.yml 对所有子命令都追加 --json 并用 jq 解析 stdout，
	// 故 --json 时必须输出 {authorized, changed, result} 结构
	// （cli.RunDoctor 的 {problems, ok} 结构 jq 取不到 .changed）。
	if asJSON {
		problems, ok := cli.Check(root)
		result := "✅ doctor 检查通过"
		if !ok {
			result = "❌ doctor 检查未通过：\n" + strings.Join(problems, "\n")
		}
		payload := map[string]any{
			"authorized": true,
			"changed":    false,
			"result":     result,
			"version":    buildinfo.Version(),
		}
		if err := writeJSON(os.Stdout, payload); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: 写 JSON 失败: %v\n", err)
			return 1
		}
		if check && !ok {
			return 1
		}
		return 0
	}
	return cli.RunDoctor(root, check, false)
}

// ── run-command ─────────────────────────────────────────────

func runCommandRun(args []string) int {
	flags := parseRunCommandFlags(args)

	// flag > env > 默认（SPEC-CLI §0.4）。action.yml 只经 env 传参（防注入，
	// 见 SPEC-ACTION §4.2），不做 env 兜底 = 命令面板整体失效。
	if flags.CommentBody == "" {
		flags.CommentBody = cli.EnvOr("COMMENT_BODY", "")
	}
	if flags.CommentUser == "" {
		flags.CommentUser = cli.EnvOr("COMMENT_USER", "")
	}
	if flags.IssueNumber == "" {
		flags.IssueNumber = cli.EnvOr("ISSUE_NUMBER", "")
	}
	if flags.RepoOwner == "" {
		flags.RepoOwner = cli.EnvOr("REPO_OWNER", cli.EnvOr("GH_REPO_OWNER", ""))
	}
	if flags.PagesBase == "" {
		flags.PagesBase = cli.EnvOr("PAGES_BASE", "")
	}
	if flags.AuthorName == "" {
		flags.AuthorName = cli.EnvOr("AUTHOR_NAME", "usm")
	}
	if flags.AuthorNamespace == "" {
		flags.AuthorNamespace = cli.EnvOr("AUTHOR_NAMESPACE", "")
	}

	if flags.CommentBody == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --comment-body 必填")
		return 2
	}
	if flags.CommentUser == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --comment-user 必填")
		return 2
	}
	if flags.IssueNumber == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --issue-number 必填")
		return 2
	}

	issueNum, err := parseIssueNumber(flags.IssueNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: --issue-number 必须是整数: %v\n", err)
		return 2
	}

	// 门禁 1（SPEC-CLI §1 判定顺序）：非命令面板 Issue → 不执行，exit 0。
	if issueNum != controlIssueNumber {
		return emitCommandResult(commands.Result{
			Text: fmt.Sprintf("非命令面板 Issue #%d，忽略执行", issueNum),
		}, false, flags)
	}

	// 门禁 2：评论者必须是仓库所有者；owner 未配置时 fail-open（本地/无仓场景）。
	if flags.RepoOwner != "" && flags.CommentUser != flags.RepoOwner {
		return emitCommandResult(commands.Result{
			Text: fmt.Sprintf("权限不足：%s 不是仓库所有者 %s", flags.CommentUser, flags.RepoOwner),
		}, false, flags)
	}

	env := &commands.Env{
		Root:            cli.EnvOr("USM_ROOT", "."),
		RepoOwner:       flags.RepoOwner,
		RepoName:        cli.EnvOr("GITHUB_REPOSITORY", cli.EnvOr("GH_REPO", "")),
		CommentUser:     flags.CommentUser,
		IssueNumber:     issueNum,
		PagesBase:       flags.PagesBase,
		AuthorName:      flags.AuthorName,
		AuthorNamespace: flags.AuthorNamespace,
		// /add <URL>、/sync 走 fetchSource；不注入 = 生产环境必报
		// "网络客户端未配置"（测试用例不触发真实抓取，60s 与 fetchSource 的超时一致）。
		Doer:     &http.Client{Timeout: 60 * time.Second},
		GHClient: newGitHubClient(),
	}

	cmd, cmdArgs, codeBlocks, err := parseCommandComment(flags.CommentBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: 解析评论失败: %v\n", err)
		return 1
	}

	res, err := commands.Execute(cmd, env, cmdArgs, codeBlocks)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 1
	}

	return emitCommandResult(res, true, flags)
}

// controlIssueNumber 命令面板 Issue 号（SPEC-CLI §1 判定顺序第 1 步）。
const controlIssueNumber = 1

// emitCommandResult 输出 run-command 结果并落 --result-file 兼容层（SPEC-CLI §1）。
func emitCommandResult(res commands.Result, authorized bool, flags cli.RunCommandFlags) int {
	if rc := emitResultAuth(res, authorized, flags.JSON); rc != 0 {
		return rc
	}
	if flags.ResultFile != "" {
		if err := os.WriteFile(flags.ResultFile, []byte(res.Text), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: 写结果文件失败: %v\n", err)
			return 1
		}
	}
	return 0
}

// ── project ─────────────────────────────────────────────────

func projectRun(args []string) int {
	root := parseRootFlag(args)
	env := &commands.Env{
		Root:      root,
		RepoOwner: cli.EnvOr("GH_REPO_OWNER", ""),
		RepoName:  cli.EnvOr("GITHUB_REPOSITORY", cli.EnvOr("GH_REPO", "")),
		PagesBase: cli.EnvOr("PAGES_BASE", ""),
		GHClient:  newGitHubClient(),
		Now:       time.Now(),
	}
	res, err := commands.Execute("project", env, "", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 1
	}
	return emitResult(res, hasJSONFlag(args))
}

func emitResult(res commands.Result, asJSON bool) int {
	return emitResultAuth(res, true, asJSON)
}

// emitResultAuth 输出命令结果：--json 时给 action.yml 用的 JSON，否则给人看的文本。
//
// action.yml 对**所有**子命令都追加 --json 并用 jq 解析 stdout，
// 所以任何子命令在 --json 下都必须输出合法 JSON —— 否则 jq parse error
// 会连带 $GITHUB_OUTPUT 的 heredoc 解析失败，报
// "Matching delimiter not found" + exit 5（真实事故，见 json_output_test.go）。
//
// authorized 只对 run-command 有意义（SPEC-CLI §1 步骤 2），其余子命令恒 true。
func emitResultAuth(res commands.Result, authorized bool, asJSON bool) int {
	if asJSON {
		if err := writeJSON(os.Stdout, map[string]any{
			"authorized": authorized,
			"changed":    res.Changed,
			"result":     res.Text,
			"warnings":   res.Warnings,
			"version":    buildinfo.Version(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: 写 JSON 失败: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Println(res.Text)
	return 0
}

// ── build ───────────────────────────────────────────────────

func buildRun(args []string) int {
	root := parseRootFlag(args)
	pagesBase := cli.EnvOr("PAGES_BASE", "")
	now := time.Now()
	gh := newGitHubClient()
	// 只在客户端真实存在时赋值：把 nil 的 *github.Client 塞进接口会让
	// siteBuilder 的判空永远为真（接口非 nil），进而 nil-deref。
	site := &siteBuilder{root: root, pagesBase: pagesBase, version: buildinfo.Version(), now: now}
	if gh != nil {
		site.gh = gh
	}
	env := &commands.Env{
		Root:      root,
		PagesBase: pagesBase,
		GHClient:  gh,
		Site:      site,
		Now:       now,
	}
	res, err := commands.Execute("build", env, "", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 1
	}
	return emitResult(res, hasJSONFlag(args))
}

// ── cleanup ─────────────────────────────────────────────────

func cleanupRun(args []string) int {
	root := parseRootFlag(args)
	env := &commands.Env{
		Root:     root,
		GHClient: newGitHubClient(),
		Now:      time.Now(),
	}
	res, err := commands.Execute("cleanup", env, cleanupArgs(args), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 1
	}
	return emitResult(res, hasJSONFlag(args))
}

// cleanupArgs 把 `usm cleanup` 的 flag 归一成 commands 能识别的参数串。
//
// 此前这里传的是 boolToString(apply)（"true"/"false"），parseCleanupFlags 找的是
// "--apply" 子串 → 永远匹配不上 → CLI 上的 --apply/--keep 从未生效（恒 dry-run）。
// action.yml 走 `cleanup --keep "$USM_KEEP" [--apply]`，两个 flag 都必须原样转发。
func cleanupArgs(args []string) string {
	apply := false
	keep := 0
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--apply":
			apply = true
		case strings.HasPrefix(args[i], "--keep="):
			if n, err := strconv.Atoi(strings.TrimPrefix(args[i], "--keep=")); err == nil && n > 0 {
				keep = n
			}
		case args[i] == "--keep" && i+1 < len(args):
			if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
				keep = n
			}
			i++
		}
	}
	// flag > env > 默认（SPEC-CLI §0.4）
	if !apply && strings.EqualFold(cli.EnvOr("USM_APPLY", ""), "true") {
		apply = true
	}
	if keep == 0 {
		if n, err := strconv.Atoi(cli.EnvOr("USM_KEEP", "")); err == nil && n > 0 {
			keep = n
		}
	}

	parts := make([]string, 0, 2)
	if apply {
		parts = append(parts, "--apply")
	}
	if keep > 0 {
		parts = append(parts, "--keep="+strconv.Itoa(keep))
	}
	return strings.Join(parts, " ")
}

// ── 辅助函数 ────────────────────────────────────────────────

// newGitHubClient 用 GITHUB_TOKEN + GITHUB_REPOSITORY 构造 GraphQL 客户端。
// 任一缺失或构造失败 → nil，各命令按「GitHub 未配置」降级（不中断、不报致命错）。
// 此前 commands.Env.GHClient 从未被赋值，project/cleanup/build 的 GitHub 分支
// 一直走 nil（stats 拉不到、评论拉不到、投影只出统计）。
func newGitHubClient() *github.Client {
	token := cli.EnvOr("GITHUB_TOKEN", "")
	repo := cli.EnvOr("GITHUB_REPOSITORY", "")
	if token == "" || repo == "" {
		return nil
	}
	c, err := github.New(token, repo)
	if err != nil {
		// 只报原因，绝不打印 token（SPEC-ARCH-TEST §6）。
		fmt.Fprintf(os.Stderr, "WARN: GitHub 客户端不可用: %v\n", err)
		return nil
	}
	return c
}

func parseRunCommandFlags(args []string) cli.RunCommandFlags {
	var f cli.RunCommandFlags
	for i := 0; i < len(args); i++ {
		switch {
		case strings.HasPrefix(args[i], "--comment-body="):
			f.CommentBody = args[i][len("--comment-body="):]
		case strings.HasPrefix(args[i], "--comment-user="):
			f.CommentUser = args[i][len("--comment-user="):]
		case strings.HasPrefix(args[i], "--issue-number="):
			f.IssueNumber = args[i][len("--issue-number="):]
		case strings.HasPrefix(args[i], "--repo-owner="):
			f.RepoOwner = args[i][len("--repo-owner="):]
		case strings.HasPrefix(args[i], "--pages-base="):
			f.PagesBase = args[i][len("--pages-base="):]
		case strings.HasPrefix(args[i], "--author-name="):
			f.AuthorName = args[i][len("--author-name="):]
		case strings.HasPrefix(args[i], "--author-namespace="):
			f.AuthorNamespace = args[i][len("--author-namespace="):]
		case strings.HasPrefix(args[i], "--result-file="):
			f.ResultFile = args[i][len("--result-file="):]
		case args[i] == "--json":
			f.JSON = true
		}
	}
	return f
}

func parseRootFlag(args []string) string {
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--root=") {
			return args[i][len("--root="):]
		}
		if args[i] == "--root" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return cli.EnvOr("USM_ROOT", ".")
}

func parseIssueNumber(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, fmt.Errorf("issue-number 必须 > 0")
	}
	return n, nil
}

// parseCommandComment 解析命令面板评论，委托 internal/parser（SPEC-CLI §1 判定顺序第 3 步）。
// 未识别命令（空体 / 不以 / 开头 / 首 token 非法）→ error，由调用方映射解析失败（exit 1）。
//
// 不再手搓解析：旧实现把围栏行本身当成代码块内容（"```js" → "js"），
// 导致 /add 拿到的永远是标记而不是源码，且不认 ~~~ 围栏与首行前的空行。
func parseCommandComment(body string) (cmd string, args string, codes []string, err error) {
	c := parser.Parse(body)
	if !c.Ok {
		return "", "", nil, fmt.Errorf("未识别命令（评论需以 /command 开头）")
	}
	return c.Command, c.Args, c.Code, nil
}

func writeJSON(w *os.File, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func now() time.Time {
	return time.Now()
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func usage(w *os.File) {
	_, _ = fmt.Fprint(w, `usm —— 油猴脚本控制台

用法: usm <command> [flags]

命令:
  run-command    执行命令面板评论中的 /command（回帖语义，见 SPEC-CLI §1）
  project        registry → Issues/版本帖 对账投影
  build          构建站点产物（dist/）
  cleanup        归档并清理命令面板历史评论（默认 dry-run，--apply 才执行）
  doctor         数据一致性自检（--check 供 CI）
  snapshot       快照基线 <check|update>
  version        打印版本与 registry schema 版本
  help           本帮助

全局约定: 默认工作目录即数据根（含 registry.json）；--root 可显式指定。
`)
}
