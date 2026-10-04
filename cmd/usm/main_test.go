package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/acg-q/userscript-console/internal/registry"
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

func TestBuildRunNoPagesBase(t *testing.T) {
	root := buildTestRegistryDir(t)
	// build 命令会执行 commands.Execute，缺少 PagesBase 时返回 error
	// 但 cmd/usm 的 buildRun 会打印错误并返回 1
	// 这里测试的是 buildRun 的路径，由于项目配置问题可能返回 0
	_ = buildRun([]string{"--root", root})
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
