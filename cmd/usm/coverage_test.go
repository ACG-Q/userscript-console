package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/acg-q/userscript-console/internal/registry"
)

// TestRunCommandMissingFlags 三个必填参数缺失时各返回 2。
func TestRunCommandMissingFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"缺 comment-body", []string{"--comment-user=u", "--issue-number=1"}},
		{"缺 comment-user", []string{"--comment-body=/list", "--issue-number=1"}},
		{"缺 issue-number", []string{"--comment-body=/list", "--comment-user=u"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if code := runCommandRun(c.args); code != 2 {
				t.Errorf("%s 应返回 2, got %d", c.name, code)
			}
		})
	}
}

// TestRunCommandBadIssueNumber 非整数 issue-number 返回 2。
func TestRunCommandBadIssueNumber(t *testing.T) {
	if code := runCommandRun([]string{
		"--comment-body=/list", "--comment-user=u", "--issue-number=abc",
	}); code != 2 {
		t.Errorf("非整数 issue-number 应返回 2, got %d", code)
	}
}

// TestRunCommandBadComment 不以 / 开头的评论返回 1（解析失败）。
func TestRunCommandBadComment(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")

	if code := runCommandRun([]string{
		"--comment-body=hello", "--comment-user=u", "--issue-number=1",
	}); code != 1 {
		t.Errorf("无斜杠评论应返回 1, got %d", code)
	}
}

// TestRunCommandJSONMode --json 输出合法 JSON。
func TestRunCommandJSONMode(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")

	if code := runCommandRun([]string{
		"--comment-body=/list", "--comment-user=u", "--issue-number=1", "--json",
	}); code != 0 {
		t.Errorf("--json 模式应返回 0, got %d", code)
	}
}

// TestRunCommandResultFile --result-file 落盘回帖正文。
func TestRunCommandResultFile(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")

	out := filepath.Join(t.TempDir(), "result.txt")
	if code := runCommandRun([]string{
		"--comment-body=/list", "--comment-user=u", "--issue-number=1",
		"--result-file=" + out,
	}); code != 0 {
		t.Fatalf("写结果文件应返回 0, got %d", code)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("结果文件应存在: %v", err)
	}
	if len(data) == 0 {
		t.Error("结果文件不应为空")
	}
}

// TestRunCommandResultFileBadPath 结果文件路径不可写时返回 1。
func TestRunCommandResultFileBadPath(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")

	bad := filepath.Join(root, "registry.json", "nested", "result.txt")
	if code := runCommandRun([]string{
		"--comment-body=/list", "--comment-user=u", "--issue-number=1",
		"--result-file=" + bad,
	}); code != 1 {
		t.Errorf("不可写结果路径应返回 1, got %d", code)
	}
}

// TestRunCommandUnknownCommand 未注册命令返回 1。
func TestRunCommandUnknownCommand(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")

	if code := runCommandRun([]string{
		"--comment-body=/nosuchcmd", "--comment-user=u", "--issue-number=1",
	}); code != 1 {
		t.Errorf("未注册命令应返回 1, got %d", code)
	}
}

// TestRunEnvFallback 未显式传 repo-owner 时从 GH_REPO_OWNER 兜底。
func TestRunEnvFallback(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")
	os.Setenv("GH_REPO_OWNER", "env-owner")
	defer os.Unsetenv("GH_REPO_OWNER")
	os.Setenv("PAGES_BASE", "https://env.github.io/repo")
	defer os.Unsetenv("PAGES_BASE")
	os.Setenv("AUTHOR_NAME", "EnvAuthor")
	defer os.Unsetenv("AUTHOR_NAME")
	os.Setenv("AUTHOR_NAMESPACE", "env.ns")
	defer os.Unsetenv("AUTHOR_NAMESPACE")

	if code := runCommandRun([]string{
		"--comment-body=/list", "--comment-user=u", "--issue-number=1",
	}); code != 0 {
		t.Errorf("环境变量兜底应返回 0, got %d", code)
	}
}

// TestDoctorRunFlagErrors doctor 参数错误路径返回 2。
func TestDoctorRunFlagErrors(t *testing.T) {
	cases := [][]string{
		{"--bogus"}, // 未知参数
		{"--root"},  // --root 缺值
	}
	for _, args := range cases {
		if code := doctorRun(args); code != 2 {
			t.Errorf("doctor %v 应返回 2, got %d", args, code)
		}
	}
}

// TestParseRootFlagVariants --root= 与 --root 两种写法都应解析。
func TestParseRootFlagVariants(t *testing.T) {
	os.Setenv("USM_ROOT", "/env/root")
	defer os.Unsetenv("USM_ROOT")

	if got := parseRootFlag([]string{"--root=/flag/root"}); got != "/flag/root" {
		t.Errorf("--root= 形式解析错误: %s", got)
	}
	if got := parseRootFlag([]string{"--root", "/sep/root"}); got != "/sep/root" {
		t.Errorf("--root 空格形式解析错误: %s", got)
	}
	if got := parseRootFlag(nil); got != "/env/root" {
		t.Errorf("无参数时应回落 USM_ROOT: %s", got)
	}
}

// TestParseIssueNumberErrors 非整数与 ≤0 都应报错。
func TestParseIssueNumberErrors(t *testing.T) {
	if _, err := parseIssueNumber("abc"); err == nil {
		t.Error("非整数应报错")
	}
	if _, err := parseIssueNumber("0"); err == nil {
		t.Error("0 应报错")
	}
	if _, err := parseIssueNumber("-1"); err == nil {
		t.Error("负数应报错")
	}
	n, err := parseIssueNumber("42")
	if err != nil || n != 42 {
		t.Errorf("合法 issue-number 应解析为 42, got %d/%v", n, err)
	}
}

// TestParseCommandCommentCodeBlocks 围栏代码块按内容提取（不含围栏行）。
func TestParseCommandCommentCodeBlocks(t *testing.T) {
	body := "/add https://example.com\n```js\nconst a = 1;\n```"
	cmd, args, codes, err := parseCommandComment(body)
	if err != nil {
		t.Fatalf("解析应成功: %v", err)
	}
	if cmd != "add" {
		t.Errorf("命令应为 add, got %s", cmd)
	}
	if args != "https://example.com" {
		t.Errorf("参数应有 URL, got %q", args)
	}
	// 旧实现按行匹配 ``` 前缀，把开闭标记行当成两个"代码块"；
	// parser 提取的是围栏内容 —— /add 正是靠它拿到源码。
	if len(codes) != 1 {
		t.Fatalf("应提取 1 个代码块, got %#v", codes)
	}
	if codes[0] != "const a = 1;" {
		t.Errorf("代码块内容应为源码, got %q", codes[0])
	}
}

// TestParseCommandCommentTildeFence parser 支持 ~~~ 围栏（旧实现不认）。
func TestParseCommandCommentTildeFence(t *testing.T) {
	body := "/add https://example.com\n~~~\nconst b = 2;\n~~~"
	_, _, codes, err := parseCommandComment(body)
	if err != nil {
		t.Fatalf("解析应成功: %v", err)
	}
	if len(codes) != 1 || codes[0] != "const b = 2;" {
		t.Errorf("~~~ 围栏应提取为一个代码块, got %#v", codes)
	}
}

// TestParseCommandCommentErrors 空体与非斜杠开头均报错。
func TestParseCommandCommentErrors(t *testing.T) {
	if _, _, _, err := parseCommandComment(""); err == nil {
		t.Error("空评论应报错")
	}
	if _, _, _, err := parseCommandComment("hello"); err == nil {
		t.Error("非斜杠开头应报错")
	}
}

// TestParseCommandCommentNoArgs 只有命令无参数时 args 为空。
func TestParseCommandCommentNoArgs(t *testing.T) {
	cmd, args, codes, err := parseCommandComment("/list")
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "list" || args != "" || len(codes) != 0 {
		t.Errorf("解析结果不符: cmd=%q args=%q codes=%#v", cmd, args, codes)
	}
}

// TestProjectRunSuccess project 在健康仓库应返回 0。
func TestProjectRunSuccess(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("GH_REPO_OWNER", "test-owner")
	defer os.Unsetenv("GH_REPO_OWNER")
	if code := projectRun([]string{"--root", root}); code != 0 {
		t.Errorf("project 应返回 0, got %d", code)
	}
}

// TestBuildRunSuccess build 带 PAGES_BASE 应返回 0。
func TestBuildRunSuccess(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("PAGES_BASE", "https://test.github.io/repo")
	defer os.Unsetenv("PAGES_BASE")
	if code := buildRun([]string{"--root", root}); code != 0 {
		t.Errorf("build 应返回 0, got %d", code)
	}
}

// TestCleanupRunApplyFlag --apply 走真实删除分支入口。
func TestCleanupRunApplyFlag(t *testing.T) {
	root := buildTestRegistryDir(t)
	if code := cleanupRun([]string{"--root", root, "--apply"}); code != 0 {
		t.Errorf("cleanup --apply 应返回 0, got %d", code)
	}
}

// TestCleanupRunApplyNotDryRun 回归：--apply 曾被折叠成 "true" 传参，
// parseCleanupFlags 找 "--apply" 子串恒失败 → 永远 dry-run。
func TestCleanupRunApplyNotDryRun(t *testing.T) {
	t.Setenv("USM_APPLY", "")
	t.Setenv("USM_KEEP", "")
	root := buildTestRegistryDir(t)

	dry := captureStdout(t, func() {
		if code := cleanupRun([]string{"--root", root}); code != 0 {
			t.Errorf("cleanup 默认应返回 0, got %d", code)
		}
	})
	if !strings.Contains(dry, "dry-run") {
		t.Errorf("默认应为 dry-run: %s", dry)
	}

	applied := captureStdout(t, func() {
		if code := cleanupRun([]string{"--root", root, "--apply"}); code != 0 {
			t.Errorf("cleanup --apply 应返回 0, got %d", code)
		}
	})
	if strings.Contains(applied, "dry-run") {
		t.Errorf("--apply 不应再提示 dry-run: %s", applied)
	}
	if !strings.Contains(applied, "cleanup 完成") {
		t.Errorf("--apply 应输出清理完成: %s", applied)
	}
}

// TestCleanupArgsForwardsFlags cleanupArgs 必须原样转发 --apply/--keep，
// 并丢弃与 parseCleanupFlags 无关的 --root/--json。
func TestCleanupArgsForwardsFlags(t *testing.T) {
	t.Setenv("USM_APPLY", "")
	t.Setenv("USM_KEEP", "")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"无 flag", []string{"--root", "/x", "--json"}, ""},
		{"--apply", []string{"--root", "/x", "--apply"}, "--apply"},
		{"--keep 空格", []string{"--keep", "3", "--json"}, "--keep=3"},
		{"--keep 等号", []string{"--keep=7"}, "--keep=7"},
		{"组合", []string{"--root", "/x", "--keep", "5", "--apply", "--json"}, "--apply --keep=5"},
		{"非法 keep", []string{"--keep=abc"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cleanupArgs(c.args); got != c.want {
				t.Errorf("cleanupArgs(%v) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

// TestCleanupArgsEnvFallback flag 缺失时回落 USM_APPLY/USM_KEEP。
func TestCleanupArgsEnvFallback(t *testing.T) {
	t.Setenv("USM_APPLY", "true")
	t.Setenv("USM_KEEP", "4")
	if got := cleanupArgs([]string{"--json"}); got != "--apply --keep=4" {
		t.Errorf("env 兜底应为 --apply --keep=4, got %q", got)
	}
	if got := cleanupArgs([]string{"--apply", "--keep=9"}); got != "--apply --keep=9" {
		t.Errorf("flag 应优先于 env, got %q", got)
	}
}

// TestNewGitHubClient token/repo 齐备才返回客户端，缺任一或格式非法都降级 nil。
func TestNewGitHubClient(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	if c := newGitHubClient(); c != nil {
		t.Error("双缺失时应返回 nil")
	}

	t.Setenv("GITHUB_TOKEN", "tok")
	if c := newGitHubClient(); c != nil {
		t.Error("缺 GITHUB_REPOSITORY 时应返回 nil")
	}

	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	if c := newGitHubClient(); c == nil {
		t.Error("token+repo 齐备时应返回客户端")
	}

	t.Setenv("GITHUB_REPOSITORY", "not-a-repo")
	if c := newGitHubClient(); c != nil {
		t.Error("repo 缺少 owner/name 分隔符时应返回 nil")
	}
}

// TestRunCommandEnvOnly action.yml 只经 env 传参（防注入）：
// 不给任何 flag 时必须能取到 COMMENT_BODY/COMMENT_USER/ISSUE_NUMBER。
func TestRunCommandEnvOnly(t *testing.T) {
	root := buildTestRegistryDir(t)
	t.Setenv("USM_ROOT", root)
	t.Setenv("COMMENT_BODY", "/list")
	t.Setenv("COMMENT_USER", "testuser")
	t.Setenv("ISSUE_NUMBER", "1")
	t.Setenv("REPO_OWNER", "")

	out := captureStdout(t, func() {
		if code := runCommandRun(nil); code != 0 {
			t.Errorf("env-only run-command 应返回 0, got %d", code)
		}
	})
	if !strings.Contains(out, "测试") && !strings.Contains(out, "list") {
		t.Errorf("env-only 应执行 /list 并回帖: %s", out)
	}
}

// TestRunCommandGateNonPanelIssue 门禁 1：非命令面板 Issue 不执行、exit 0、
// authorized=false（SPEC-CLI §1 判定顺序第 1 步）。
func TestRunCommandGateNonPanelIssue(t *testing.T) {
	root := buildTestRegistryDir(t)
	t.Setenv("USM_ROOT", root)

	var payload map[string]any
	out := captureStdout(t, func() {
		code := runCommandRun([]string{
			"--comment-body=/list", "--comment-user=u", "--issue-number=7", "--json",
		})
		if code != 0 {
			t.Errorf("非面板 Issue 应 exit 0, got %d", code)
		}
	})
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("门禁输出必须是合法 JSON: %v\n%q", err, out)
	}
	if payload["authorized"] != false {
		t.Errorf("authorized 应为 false, got %v", payload["authorized"])
	}
	if payload["changed"] != false {
		t.Errorf("changed 应为 false, got %v", payload["changed"])
	}
	if res, _ := payload["result"].(string); !strings.Contains(res, "非命令面板 Issue #7") {
		t.Errorf("回帖应说明非面板 Issue: %q", res)
	}
}

// TestRunCommandGateNotOwner 门禁 2：评论者不是仓库所有者 → 不执行、
// exit 0、authorized=false。
func TestRunCommandGateNotOwner(t *testing.T) {
	root := buildTestRegistryDir(t)
	t.Setenv("USM_ROOT", root)

	var payload map[string]any
	out := captureStdout(t, func() {
		code := runCommandRun([]string{
			"--comment-body=/list", "--comment-user=someone",
			"--issue-number=1", "--repo-owner=the-owner", "--json",
		})
		if code != 0 {
			t.Errorf("权限不足应 exit 0, got %d", code)
		}
	})
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("门禁输出必须是合法 JSON: %v\n%q", err, out)
	}
	if payload["authorized"] != false {
		t.Errorf("authorized 应为 false, got %v", payload["authorized"])
	}
	res, _ := payload["result"].(string)
	if !strings.Contains(res, "权限不足") || !strings.Contains(res, "the-owner") {
		t.Errorf("回帖应说明权限不足与所有者: %q", res)
	}
}

// TestRunCommandOwnerAuthorized 门禁通过时 authorized=true 且命令真的执行。
func TestRunCommandOwnerAuthorized(t *testing.T) {
	root := buildTestRegistryDir(t)
	t.Setenv("USM_ROOT", root)

	var payload map[string]any
	out := captureStdout(t, func() {
		code := runCommandRun([]string{
			"--comment-body=/list", "--comment-user=the-owner",
			"--issue-number=1", "--repo-owner=the-owner", "--json",
		})
		if code != 0 {
			t.Errorf("所有者评论应 exit 0, got %d", code)
		}
	})
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("输出必须是合法 JSON: %v\n%q", err, out)
	}
	if payload["authorized"] != true {
		t.Errorf("authorized 应为 true, got %v", payload["authorized"])
	}
	res, _ := payload["result"].(string)
	if strings.Contains(res, "权限不足") {
		t.Errorf("所有者不应被拦: %q", res)
	}
}

// TestRunSchemaGuard USM_REGISTRY_SCHEMA 只校验触碰 registry 的子命令：
// 匹配/未设置 → 放行；不匹配或非数字 → exit 1；version 不受校验。
func TestRunSchemaGuard(t *testing.T) {
	clearCommentEnv := func(t *testing.T) {
		t.Helper()
		t.Setenv("COMMENT_BODY", "")
		t.Setenv("COMMENT_USER", "")
		t.Setenv("ISSUE_NUMBER", "")
	}

	t.Run("匹配则放行", func(t *testing.T) {
		clearCommentEnv(t)
		t.Setenv("USM_REGISTRY_SCHEMA", strconv.Itoa(registry.SchemaVersion))
		if code := run([]string{"run-command"}); code != 2 {
			t.Errorf("schema 匹配时应走到参数校验 exit 2, got %d", code)
		}
	})

	t.Run("未设置则跳过", func(t *testing.T) {
		clearCommentEnv(t)
		t.Setenv("USM_REGISTRY_SCHEMA", "")
		if code := run([]string{"run-command"}); code != 2 {
			t.Errorf("未设置 schema 应跳过校验, got %d", code)
		}
	})

	t.Run("版本不匹配 exit 1", func(t *testing.T) {
		clearCommentEnv(t)
		t.Setenv("USM_REGISTRY_SCHEMA", strconv.Itoa(registry.SchemaVersion+1))
		if code := run([]string{"run-command"}); code != 1 {
			t.Errorf("schema 不匹配应 exit 1, got %d", code)
		}
	})

	t.Run("非数字拒绝", func(t *testing.T) {
		clearCommentEnv(t)
		t.Setenv("USM_REGISTRY_SCHEMA", "abc")
		if code := run([]string{"project"}); code != 1 {
			t.Errorf("非数字 schema 应 exit 1, got %d", code)
		}
	})

	t.Run("version 不受校验", func(t *testing.T) {
		t.Setenv("USM_REGISTRY_SCHEMA", "999")
		if code := run([]string{"version"}); code != 0 {
			t.Errorf("version 应 exit 0, got %d", code)
		}
	})
}

// TestRunSchemaMismatchJSON --json 下 schema 不匹配仍须输出合法 JSON，
// 否则 action.yml 的 jq/heredoc 会连带失败。
func TestRunSchemaMismatchJSON(t *testing.T) {
	t.Setenv("USM_REGISTRY_SCHEMA", "999")

	var payload map[string]any
	out := captureStdout(t, func() {
		if code := run([]string{"doctor", "--json"}); code != 1 {
			t.Errorf("schema 不匹配应 exit 1, got %d", code)
		}
	})
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("schema 错误必须输出合法 JSON: %v\n%q", err, out)
	}
	if payload["authorized"] != false {
		t.Errorf("authorized 应为 false, got %v", payload["authorized"])
	}
	res, _ := payload["result"].(string)
	if !strings.Contains(res, "registry schema 不匹配") {
		t.Errorf("回帖应说明 schema 不匹配: %q", res)
	}
}

// TestRunDispatches run() 应正确分发各子命令。
func TestRunDispatches(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"version", []string{"version"}, 0},
		{"doctor", []string{"doctor", "--root", t.TempDir()}, 0},
		{"cleanup", []string{"cleanup", "--root", buildTestRegistryDir(t)}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if code := run(c.args); code != c.want {
				t.Errorf("%s 应返回 %d, got %d", c.name, c.want, code)
			}
		})
	}
}

// TestSubcommandErrorPaths registry 损坏时 project/build/cleanup 应返回 1。
func TestSubcommandErrorPaths(t *testing.T) {
	badRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(badRoot, "registry.json"), []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Setenv("GH_REPO_OWNER", "test-owner")
	defer os.Unsetenv("GH_REPO_OWNER")

	if code := projectRun([]string{"--root", badRoot}); code != 1 {
		t.Errorf("project 遇坏 registry 应返回 1, got %d", code)
	}
	os.Setenv("PAGES_BASE", "https://test.github.io/repo")
	defer os.Unsetenv("PAGES_BASE")
	if code := buildRun([]string{"--root", badRoot}); code != 1 {
		t.Errorf("build 遇坏 registry 应返回 1, got %d", code)
	}
	if code := cleanupRun([]string{"--root", badRoot, "--apply"}); code != 1 {
		t.Errorf("cleanup 遇坏 registry 应返回 1, got %d", code)
	}
}

// TestProjectRunNoOwner 未配置 repo owner 时 project 软失败（退出码 0，回帖带错误文本）。
func TestProjectRunNoOwner(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Unsetenv("GH_REPO_OWNER")
	os.Unsetenv("GH_CLIENT")
	if code := projectRun([]string{"--root", root}); code != 0 {
		t.Errorf("project 无 repo owner 应软失败返回 0, got %d", code)
	}
}

// TestBuildRunNoPagesBase 缺 PAGES_BASE 时 build 软失败（退出码 0，回帖带错误文本）。
func TestBuildRunNoPagesBase(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Unsetenv("PAGES_BASE")
	if code := buildRun([]string{"--root", root}); code != 0 {
		t.Errorf("build 缺 PAGES_BASE 应软失败返回 0, got %d", code)
	}
}
