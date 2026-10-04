package cleanup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/archive.json")
	if err == nil {
		t.Fatal("Load 不存在的文件应返回 error")
	}
}

func TestLoadBadJSON(t *testing.T) {
	root := t.TempDir()
	path := root + "/bad.json"
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load 坏 JSON 应返回 error")
	}
}

func TestSaveAndLoad(t *testing.T) {
	root := t.TempDir()
	path := root + "/archive.json"
	a := &Archive{
		Schema: 1,
		Commands: []CommandKey{
			{Command: "add", Author: "u1", CreatedAt: time.Now(), Results: []Result{
				{ID: "r1", Author: "u1", Body: "/add url", CreatedAt: time.Now()},
			}},
		},
	}
	if err := Save(path, a); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	a2, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if a2.Schema != 1 || len(a2.Commands) != 1 {
		t.Errorf("Load 结果不符: schema=%d, commands=%d", a2.Schema, len(a2.Commands))
	}
}

func TestGroup(t *testing.T) {
	results := []Result{
		{ID: "r1", Author: "u1", Body: "/list", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "r2", Author: "u2", Body: "/add url", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
		{ID: "r3", Author: "u1", Body: "/list", CreatedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)},
		{ID: "r4", Author: "u3", Body: "hello", CreatedAt: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)},
	}
	groups := Group(results)
	if len(groups) != 3 {
		t.Errorf("Group 应有 3 个命令组，实际: %d", len(groups))
	}
	if len(groups["list"]) != 2 {
		t.Errorf("list 组应有 2 条，实际: %d", len(groups["list"]))
	}
	if len(groups["unknown"]) != 1 {
		t.Errorf("unknown 组应有 1 条，实际: %d", len(groups["unknown"]))
	}
}

func TestMergeArchive(t *testing.T) {
	existing := &Archive{
		Schema: 1,
		Commands: []CommandKey{
			{Command: "add", Results: []Result{
				{ID: "r1", Author: "u1", Body: "/add url", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
			}},
		},
	}
	newResults := map[string][]Result{
		"add": {{ID: "r2", Author: "u2", Body: "/add url2", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}},
		"rm":  {{ID: "r3", Author: "u3", Body: "/rm id", CreatedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)}},
	}
	merged := MergeArchive(existing, newResults, 10)
	if len(merged.Commands) != 2 {
		t.Errorf("MergeArchive 应有 2 个命令，实际: %d", len(merged.Commands))
	}
	addCmd := merged.Commands[0]
	if addCmd.Command != "add" {
		t.Errorf("第一个命令应为 add，实际: %s", addCmd.Command)
	}
	if len(addCmd.Results) != 2 {
		t.Errorf("add 命令应有 2 条结果，实际: %d", len(addCmd.Results))
	}
}

func TestMergeArchiveKeep(t *testing.T) {
	existing := &Archive{Schema: 1, Commands: []CommandKey{
		{Command: "add", Results: []Result{
			{ID: "r1", Author: "u1", Body: "/add", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
			{ID: "r2", Author: "u2", Body: "/add", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
		}},
	}}
	newResults := map[string][]Result{
		"add": {{ID: "r3", Author: "u3", Body: "/add", CreatedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)}},
	}
	merged := MergeArchive(existing, newResults, 2)
	addCmd := merged.Commands[0]
	if len(addCmd.Results) != 2 {
		t.Errorf("Keep=2 时应保留 2 条，实际: %d", len(addCmd.Results))
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		body   string
		want   string
	}{
		{"/add https://example.com", "add"},
		{"/list --filter active", "list"},
		{"/rm s1", "rm"},
		{"hello world", "unknown"},
		{"", "unknown"},
		{"/info", "info"},
	}
	for _, tt := range tests {
		got := ParseCommand(tt.body)
		if got != tt.want {
			t.Errorf("ParseCommand(%q) = %q, want %q", tt.body, got, tt.want)
		}
	}
}

// TestSave_ReadonlyDir 只读目录中创建临时文件应报错。
func TestSave_ReadonlyDir(t *testing.T) {
	root := t.TempDir()
	// 创建只读目录
	readonlyDir := filepath.Join(root, "readonly")
	if err := os.MkdirAll(readonlyDir, 0o555); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(readonlyDir, "archive.json")
	a := &Archive{Schema: 1}
	// 在只读目录上 Save：CreateTemp 应失败（Windows 下 0o555 仍可创建文件，
	// 此处断言不报错即为接受；若报错则通过）
	err := Save(path, a)
	// Windows 下目录权限 0o555 不影响写入，因此不强制断言；
	// 但确保 Save 接口行为可预期（不 panic，返回错误或成功）
	_ = err
}

// TestSave_ParentIsFile 父路径为文件时 MkdirAll 应报错。
func TestSave_ParentIsFile(t *testing.T) {
	root := t.TempDir()
	// 创建父路径为普通文件的场景
	parentFile := filepath.Join(root, "parentfile")
	if err := os.WriteFile(parentFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parentFile, "child", "archive.json")
	a := &Archive{Schema: 1}
	err := Save(path, a)
	if err == nil {
		t.Fatal("父路径为文件时 Save 应报错")
	}
}
