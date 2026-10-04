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
	// 创建必要目录
	os.MkdirAll(filepath.Join(root, "scripts", "self", "self01"), 0o755)
	os.WriteFile(filepath.Join(root, "scripts", "self", "self01", "index.js"), []byte("// test"), 0o644)
	return root
}
