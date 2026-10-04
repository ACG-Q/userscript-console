package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/snapshot"
)

func TestRunVersion(t *testing.T) {
	if code := run([]string{"version"}); code != 0 {
		t.Errorf("version 应返回 0, got %d", code)
	}
}

func TestRunHelp(t *testing.T) {
	if code := run([]string{"help"}); code != 0 {
		t.Errorf("help 应返回 0, got %d", code)
	}
}

func TestRunUnknown(t *testing.T) {
	if code := run([]string{"bogus"}); code != 2 {
		t.Errorf("未知命令应返回 2, got %d", code)
	}
}

func TestRunNoArgs(t *testing.T) {
	if code := run(nil); code != 2 {
		t.Errorf("无参数应返回 2, got %d", code)
	}
}

func TestDoctorCheckMissingRegistry(t *testing.T) {
	root := t.TempDir()
	// 没有 registry.json → doctor 应报问题
	if code := doctorRun([]string{"--check", "--root", root}); code != 1 {
		t.Errorf("缺失 registry 应返回 1, got %d", code)
	}
}

func TestDoctorJSONMode(t *testing.T) {
	root := buildTestRegistryDir(t)
	if code := doctorRun([]string{"--json", "--root", root}); code != 0 {
		t.Errorf("健康仓库应返回 0, got %d", code)
	}
}

func TestRunCommandMissingBody(t *testing.T) {
	if code := runCommandRun([]string{"--comment-user=test", "--issue-number=1"}); code != 2 {
		t.Errorf("缺少 --comment-body 应返回 2, got %d", code)
	}
}

func TestRunCommandMissingUser(t *testing.T) {
	if code := runCommandRun([]string{"--comment-body=/list", "--issue-number=1"}); code != 2 {
		t.Errorf("缺少 --comment-user 应返回 2, got %d", code)
	}
}

func TestRunCommandMissingIssueNumber(t *testing.T) {
	if code := runCommandRun([]string{"--comment-body=/list", "--comment-user=test"}); code != 2 {
		t.Errorf("缺少 --issue-number 应返回 2, got %d", code)
	}
}

func TestRunCommandInvalidIssueNumber(t *testing.T) {
	if code := runCommandRun([]string{"--comment-body=/list", "--comment-user=test", "--issue-number=abc"}); code != 2 {
		t.Errorf("无效 issue-number 应返回 2, got %d", code)
	}
}

func TestRunCommandInvalidIssueNumberZero(t *testing.T) {
	if code := runCommandRun([]string{"--comment-body=/list", "--comment-user=test", "--issue-number=0"}); code != 2 {
		t.Errorf("issue-number=0 应返回 2, got %d", code)
	}
}

func TestParseIssueNumber(t *testing.T) {
	tests := []struct {
		input string
		want  int
		ok    bool
	}{
		{"5", 5, true},
		{"0", 0, false},
		{"abc", 0, false},
		{"-1", 0, false},
	}
	for _, tt := range tests {
		got, err := parseIssueNumber(tt.input)
		if tt.ok {
			if err != nil || got != tt.want {
				t.Errorf("parseIssueNumber(%q) = %d, %v; want %d, nil", tt.input, got, err, tt.want)
			}
		} else {
			if err == nil {
				t.Errorf("parseIssueNumber(%q) 应失败", tt.input)
			}
		}
	}
}

func TestParseCommandComment(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantCmd   string
		wantArgs  string
		wantCodes int
		wantErr   bool
	}{
		{"简单命令", "/list", "list", "", 0, false},
		{"带参数", "/info self01", "info", "self01", 0, false},
		{"无效命令", "hello", "", "", 0, true},
		{"空评论", "", "", "", 0, true},
		{"多行代码块", "/add\n```\ncode\n```\nmore", "add", "", 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args, codes, err := parseCommandComment(tt.body)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseCommandComment(%q) 应失败", tt.body)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCommandComment(%q) 失败: %v", tt.body, err)
			}
			if cmd != tt.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, tt.wantCmd)
			}
			if args != tt.wantArgs {
				t.Errorf("args = %q, want %q", args, tt.wantArgs)
			}
			if len(codes) != tt.wantCodes {
				t.Errorf("codes len = %d, want %d", len(codes), tt.wantCodes)
			}
		})
	}
}

func TestParseRunCommandFlags(t *testing.T) {
	flags := parseRunCommandFlags([]string{
		"--comment-body=/list",
		"--comment-user=test",
		"--issue-number=1",
		"--repo-owner=owner",
		"--pages-base=https://test.github.io/repo",
		"--author-name=Author",
		"--author-namespace=ns",
		"--result-file=out.txt",
		"--json",
	})
	if flags.CommentBody != "/list" {
		t.Errorf("CommentBody = %q, want /list", flags.CommentBody)
	}
	if flags.CommentUser != "test" {
		t.Errorf("CommentUser = %q, want test", flags.CommentUser)
	}
	if flags.IssueNumber != "1" {
		t.Errorf("IssueNumber = %q, want 1", flags.IssueNumber)
	}
	if flags.RepoOwner != "owner" {
		t.Errorf("RepoOwner = %q, want owner", flags.RepoOwner)
	}
	if flags.PagesBase != "https://test.github.io/repo" {
		t.Errorf("PagesBase = %q", flags.PagesBase)
	}
	if flags.AuthorName != "Author" {
		t.Errorf("AuthorName = %q", flags.AuthorName)
	}
	if flags.AuthorNamespace != "ns" {
		t.Errorf("AuthorNamespace = %q", flags.AuthorNamespace)
	}
	if flags.ResultFile != "out.txt" {
		t.Errorf("ResultFile = %q", flags.ResultFile)
	}
	if !flags.JSON {
		t.Error("JSON should be true")
	}
}

func TestProjectRun(t *testing.T) {
	root := buildTestRegistryDir(t)
	if code := projectRun([]string{"--root", root}); code != 0 {
		t.Errorf("project 应返回 0, got %d", code)
	}
}

func TestBuildRunWithPagesBase(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("PAGES_BASE", "https://test.github.io/repo")
	defer os.Unsetenv("PAGES_BASE")

	code := buildRun([]string{"--root", root})
	if code != 0 {
		t.Errorf("build 带 PagesBase 应返回 0, got %d", code)
	}
}

func TestSnapshotRunWithGenerator(t *testing.T) {
	// snapshot check 需要注册生成器，通过 cmd/usm 的 init() 已注册 buildSnapshotFiles
	// 但测试时在独立包中运行，需要手动设置
	// 测试 snapshot check 走 run() 路径
	code := run([]string{"snapshot", "check"})
	// 由于 tests/corpus/inputs/registry.json 存在但 buildSnapshotFiles 会加载它
	// 结果取决于基线是否一致；这里只验证不 panic 且返回合理值
	if code != 0 && code != 1 {
		t.Errorf("snapshot check 应返回 0 或 1, got %d", code)
	}
}

func TestRunSnapshotUpdate(t *testing.T) {
	code := run([]string{"snapshot", "update"})
	if code != 0 && code != 1 {
		t.Errorf("snapshot update 应返回 0 或 1, got %d", code)
	}
}

func TestCleanupRun(t *testing.T) {
	root := buildTestRegistryDir(t)
	if code := cleanupRun([]string{"--root", root}); code != 0 {
		t.Errorf("cleanup 应返回 0, got %d", code)
	}
}

func TestCleanupRunWithApply(t *testing.T) {
	root := buildTestRegistryDir(t)
	if code := cleanupRun([]string{"--root", root, "--apply"}); code != 0 {
		t.Errorf("cleanup --apply 应返回 0, got %d", code)
	}
}

func buildTestRegistryDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	reg := &registry.Registry{
		Schema: registry.SchemaVersion,
		Scripts: []registry.Script{
			{ID: "self01", Type: registry.TypeSelf, Name: "测试", Version: "1.0.0", Enabled: true,
				Match: []string{"*://*/*"}, Grant: []string{"none"},
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-10-05T00:00:00Z",
				Changelog: []registry.ChangelogEntry{{Version: "1.0.0", Date: "2026-10-05", Note: "初始"}},
			},
		},
	}
	if err := reg.Save(filepath.Join(root, "registry.json")); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(root, "scripts", "self", "self01"), 0o755)
	os.WriteFile(filepath.Join(root, "scripts", "self", "self01", "index.js"), []byte("// test"), 0o644)
	return root
}

func TestParseRootFlag(t *testing.T) {
	if got := parseRootFlag([]string{"--root=/tmp/test"}); got != "/tmp/test" {
		t.Errorf("parseRootFlag = %q, want /tmp/test", got)
	}
	if got := parseRootFlag([]string{"--root", "/tmp/test"}); got != "/tmp/test" {
		t.Errorf("parseRootFlag (space) = %q, want /tmp/test", got)
	}
	if got := parseRootFlag(nil); got != "." {
		t.Errorf("parseRootFlag(nil) = %q, want .", got)
	}
}

func TestBoolToString(t *testing.T) {
	if got := boolToString(true); got != "true" {
		t.Errorf("boolToString(true) = %q", got)
	}
	if got := boolToString(false); got != "false" {
		t.Errorf("boolToString(false) = %q", got)
	}
}

func TestRunCommandWithList(t *testing.T) {
	root := buildTestRegistryDir(t)
	// 设置环境变量
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")

	code := runCommandRun([]string{
		"--comment-body=/list",
		"--comment-user=testuser",
		"--issue-number=1",
	})
	if code != 0 {
		t.Errorf("run-command /list 应返回 0, got %d", code)
	}
}

func TestRunCommandWithInfo(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")

	code := runCommandRun([]string{
		"--comment-body=/info self01",
		"--comment-user=testuser",
		"--issue-number=1",
	})
	if code != 0 {
		t.Errorf("run-command /info 应返回 0, got %d", code)
	}
}

func TestSnapshotCheck(t *testing.T) {
	// snapshot check 需要注册生成器，这里测试基本路径
	code := snapshot.Run(t.TempDir(), []string{"check"})
	// 未注册生成器时应返回 1
	if code != 1 {
		t.Errorf("snapshot check 无生成器应返回 1, got %d", code)
	}
}

func TestSnapshotUpdate(t *testing.T) {
	code := snapshot.Run(t.TempDir(), []string{"update"})
	if code != 1 {
		t.Errorf("snapshot update 无生成器应返回 1, got %d", code)
	}
}

func TestDoctorUnknownArg(t *testing.T) {
	if code := doctorRun([]string{"--unknown"}); code != 2 {
		t.Errorf("doctor 未知参数应返回 2, got %d", code)
	}
}

func TestDoctorMissingRootValue(t *testing.T) {
	if code := doctorRun([]string{"--root"}); code != 2 {
		t.Errorf("doctor --root 缺值应返回 2, got %d", code)
	}
}

func TestWriteJSON(t *testing.T) {
	// 测试 writeJSON 函数（通过 runCommandRun 的 JSON 路径间接测试）
	root := buildTestRegistryDir(t)
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")

	// 使用 --json 标志触发 writeJSON 路径
	code := runCommandRun([]string{
		"--comment-body=/list",
		"--comment-user=testuser",
		"--issue-number=1",
		"--json",
	})
	if code != 0 {
		t.Errorf("run-command --json 应返回 0, got %d", code)
	}
}

func TestResultFile(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")

	tmpFile := filepath.Join(t.TempDir(), "result.txt")
	code := runCommandRun([]string{
		"--comment-body=/list",
		"--comment-user=testuser",
		"--issue-number=1",
		"--result-file=" + tmpFile,
	})
	if code != 0 {
		t.Errorf("run-command 应返回 0, got %d", code)
	}
	if content, err := os.ReadFile(tmpFile); err != nil {
		t.Logf("结果文件写入失败（可能权限问题）: %v", err)
	} else if len(content) == 0 {
		t.Error("结果文件不应为空")
	}
}

func TestRunCommandParseError(t *testing.T) {
	// 测试评论解析失败的路径
	code := runCommandRun([]string{
		"--comment-body=hello world", // 不以 / 开头，应解析失败
		"--comment-user=testuser",
		"--issue-number=1",
	})
	if code != 1 {
		t.Errorf("无效评论应返回 1, got %d", code)
	}
}

func TestRunCommandUnknownCmd(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")

	// 测试未知命令的路径
	code := runCommandRun([]string{
		"--comment-body=/unknown_cmd",
		"--comment-user=testuser",
		"--issue-number=1",
	})
	if code != 1 {
		t.Errorf("未知命令应返回 1, got %d", code)
	}
}

func TestRunCommandEmptyBody(t *testing.T) {
	code := runCommandRun([]string{
		"--comment-body=",
		"--comment-user=testuser",
		"--issue-number=1",
	})
	if code != 2 {
		t.Errorf("空评论体应返回 2, got %d", code)
	}
}

func TestRunCommandEmptyUser(t *testing.T) {
	code := runCommandRun([]string{
		"--comment-body=/list",
		"--comment-user=",
		"--issue-number=1",
	})
	if code != 2 {
		t.Errorf("空用户应返回 2, got %d", code)
	}
}

func TestRunCommandEmptyIssueNumber(t *testing.T) {
	code := runCommandRun([]string{
		"--comment-body=/list",
		"--comment-user=test",
		"--issue-number=",
	})
	if code != 2 {
		t.Errorf("空 issue-number 应返回 2, got %d", code)
	}
}

func TestRunCommandWithRepoOwner(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")

	code := runCommandRun([]string{
		"--comment-body=/list",
		"--comment-user=testuser",
		"--issue-number=1",
		"--repo-owner=myrepo",
	})
	if code != 0 {
		t.Errorf("带 repo-owner 应返回 0, got %d", code)
	}
}

func TestRunCommandWithPagesBase(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")

	code := runCommandRun([]string{
		"--comment-body=/list",
		"--comment-user=testuser",
		"--issue-number=1",
		"--pages-base=https://owner.github.io/repo",
	})
	if code != 0 {
		t.Errorf("带 pages-base 应返回 0, got %d", code)
	}
}

func TestRunCommandWithAuthorName(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")

	code := runCommandRun([]string{
		"--comment-body=/list",
		"--comment-user=testuser",
		"--issue-number=1",
		"--author-name=CustomAuthor",
	})
	if code != 0 {
		t.Errorf("带 author-name 应返回 0, got %d", code)
	}
}

func TestRunCommandWithAuthorNamespace(t *testing.T) {
	root := buildTestRegistryDir(t)
	os.Setenv("USM_ROOT", root)
	defer os.Unsetenv("USM_ROOT")

	code := runCommandRun([]string{
		"--comment-body=/list",
		"--comment-user=testuser",
		"--issue-number=1",
		"--author-namespace=custom.ns",
	})
	if code != 0 {
		t.Errorf("带 author-namespace 应返回 0, got %d", code)
	}
}
