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
	"os"
	"strings"

	"github.com/acg-q/userscript-console/internal/cli"
	"github.com/acg-q/userscript-console/internal/commands"
	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/snapshot"
)

// version 由 -ldflags "-X main.version=x.y.z" 注入。
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("usm %s\nregistry-schema-version %d\n", version, registry.SchemaVersion)
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

// doctorRun 解析 `doctor [--check] [--json] [--root path]`（SPEC-CLI §5）。
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
	return cli.RunDoctor(root, check, asJSON)
}

// runCommandRun 解析 `run-command --comment-body=... --comment-user=... --issue-number=... [--json] [--result-file=...]`。
func runCommandRun(args []string) int {
	flags := parseRunCommandFlags(args)
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
	if flags.RepoOwner == "" {
		flags.RepoOwner = cli.EnvOr("GH_REPO_OWNER", "")
	}

	// 解析 issue number
	issueNum, err := parseIssueNumber(flags.IssueNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: --issue-number 必须是整数: %v\n", err)
		return 2
	}

	env := &commands.Env{
		Root:            cli.EnvOr("USM_ROOT", "."),
		RepoOwner:       flags.RepoOwner,
		CommentUser:     flags.CommentUser,
		IssueNumber:     issueNum,
		PagesBase:       cli.EnvOr("PAGES_BASE", ""),
		AuthorName:      cli.EnvOr("AUTHOR_NAME", "usm"),
		AuthorNamespace: cli.EnvOr("AUTHOR_NAMESPACE", ""),
	}

	// 执行命令解析
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

	// 输出结果
	if flags.JSON {
		out := map[string]any{"authorized": true, "changed": res.Changed, "result": res.Text}
		if err := writeJSON(os.Stdout, out); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: 写 JSON 失败: %v\n", err)
			return 1
		}
	} else {
		fmt.Println(res.Text)
	}

	if flags.ResultFile != "" {
		if err := os.WriteFile(flags.ResultFile, []byte(res.Text), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: 写结果文件失败: %v\n", err)
			return 1
		}
	}

	return 0
}

// projectRun 解析 `project [--root path] [--dry-run]`。
func projectRun(args []string) int {
	// TODO: 实现投影逻辑
	fmt.Fprintln(os.Stderr, "ERROR: project 子命令尚未完整实现")
	return 1
}

// buildRun 解析 `build [--out dist] [--pages-base url]`。
func buildRun(args []string) int {
	// TODO: 实现构建逻辑
	fmt.Fprintln(os.Stderr, "ERROR: build 子命令尚未完整实现")
	return 1
}

// cleanupRun 解析 `cleanup [--keep N] [--apply] [--json]`。
func cleanupRun(args []string) int {
	// TODO: 实现清理逻辑
	fmt.Fprintln(os.Stderr, "ERROR: cleanup 子命令尚未完整实现")
	return 1
}

// ── 辅助函数 ───────────────────────────────────────────────

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

func parseCommandComment(body string) (cmd string, args string, codes []string, err error) {
	// 简单解析：第一行是命令，后续是代码块
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) == 0 {
		return "", "", nil, fmt.Errorf("评论体为空")
	}
	// 第一行应该是 /command args
	first := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(first, "/") {
		return "", "", nil, fmt.Errorf("评论必须以 / 开头")
	}
	parts := strings.SplitN(first[1:], " ", 2)
	cmd = parts[0]
	if len(parts) > 1 {
		args = parts[1]
	}
	// 提取代码块（```...```）
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "```") {
			// 开始/结束代码块
			content := strings.TrimPrefix(line, "```")
			content = strings.TrimSuffix(content, "```")
			codes = append(codes, content)
		}
	}
	return cmd, args, codes, nil
}

func writeJSON(w *os.File, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func usage(w *os.File) {
	fmt.Fprint(w, `usm —— 油猴脚本控制台

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
