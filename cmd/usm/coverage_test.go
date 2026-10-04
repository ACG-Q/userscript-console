package main

import (
	"os"
	"path/filepath"
	"testing"
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

// TestParseCommandCommentCodeBlocks 代码块标记应被提取。
func TestParseCommandCommentCodeBlocks(t *testing.T) {
	body := "/add https://example.com\n```js\n```"
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
	// 实现按行匹配 ``` 前缀，开闭标记各产生一条
	if len(codes) != 2 {
		t.Errorf("应提取 2 个代码块标记行, got %#v", codes)
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
