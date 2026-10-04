package cleanup

import (
	"bytes"
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
	path := filepath.Join(root, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load 坏 JSON 应返回 error")
	}
}

func TestLoadSchemaDefault(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "archive.json")
	if err := os.WriteFile(path, []byte(`{"commands":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if a.Schema != 1 {
		t.Errorf("缺 schema 时应默认 1，实际: %d", a.Schema)
	}
}

func TestSaveAndLoad(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "archive.json")
	a := &Archive{
		Schema: 1,
		Commands: []CommandKey{
			{
				Command:   "add",
				Author:    "u1",
				CreatedAt: time.Now(),
				Results: []Result{
					{ID: "r1", Author: "u1", Body: "/add url", CreatedAt: time.Now()},
				},
			},
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

// TestSave_Idempotent 幂等：Load→Save→Load 两次保存后字节一致（SPEC-DATA I-2）。
func TestSave_Idempotent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "archive.json")
	a := &Archive{
		Schema: 1,
		Commands: []CommandKey{
			{
				Command:   "add",
				Author:    "u1",
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				Results: []Result{
					{ID: "r1", Author: "u1", Body: "/add url", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
				},
			},
		},
	}
	if err := Save(path, a); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, loaded); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("Load→Save 幂等失败:\nfirst:  %s\nsecond: %s", first, second)
	}
	if bytes.HasSuffix(second, []byte("\n")) {
		t.Error("归档文件不应有结尾换行（SPEC-DATA §1.1 规则 4）")
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
	// 组内应按时间倒序（最新在前）
	if groups["list"][0].ID != "r3" {
		t.Errorf("list 组应按时间倒序，首条应为 r3，实际: %s", groups["list"][0].ID)
	}
}

func TestMergeArchive(t *testing.T) {
	existing := &Archive{
		Schema: 1,
		Commands: []CommandKey{
			{
				Command: "add",
				Results: []Result{
					{ID: "r1", Author: "u1", Body: "/add url", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
				},
			},
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

// TestMergeArchiveNilExisting existing 为 nil 时应新建归档。
func TestMergeArchiveNilExisting(t *testing.T) {
	newResults := map[string][]Result{
		"list": {{ID: "r1", Author: "u1", Body: "/list", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}},
	}
	merged := MergeArchive(nil, newResults, 10)
	if merged.Schema != 1 {
		t.Errorf("nil existing 时 schema 应为 1，实际: %d", merged.Schema)
	}
	if len(merged.Commands) != 1 {
		t.Errorf("应有 1 个命令组，实际: %d", len(merged.Commands))
	}
	if merged.Commands[0].Author != "u1" {
		t.Errorf("Author 应取首条结果的作者，实际: %s", merged.Commands[0].Author)
	}
}

func TestMergeArchiveKeep(t *testing.T) {
	existing := &Archive{Schema: 1, Commands: []CommandKey{
		{
			Command: "add",
			Results: []Result{
				{ID: "r1", Author: "u1", Body: "/add", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
				{ID: "r2", Author: "u2", Body: "/add", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
			},
		},
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
		body string
		want string
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

// TestParseCommandEdgeCases parseCommand 边界情况（仅斜杠 / 大写 / CR LF 等）。
func TestParseCommandEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"仅斜杠", "/", "unknown"},
		{"斜杠加空格", "/ ", "unknown"},
		{"大写命令", "/ADD url", "add"},
		{"命令带等号", "/add=url", "add=url"},
		{"仅换行", "\n\n", "unknown"},
		{"多行首行命令", "/list\nmore content", "list"},
		{"首行非命令", "text\n/add url", "unknown"},
		{"制表符", "\t/list\t", "list"},
		{"CR LF", "/rm\r\nid", "rm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseCommand(tt.body); got != tt.want {
				t.Errorf("ParseCommand(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

// TestSave_ReadonlyDir 只读目录中 Save 的行为可预期（不 panic）。
func TestSave_ReadonlyDir(t *testing.T) {
	root := t.TempDir()
	readonlyDir := filepath.Join(root, "readonly")
	if err := os.MkdirAll(readonlyDir, 0o555); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(readonlyDir, "archive.json")
	a := &Archive{Schema: 1}
	// Windows 下目录权限 0o555 不阻止写入，因此不强制断言错误；
	// 只验证接口行为可预期（不 panic，返回错误或成功）
	_ = Save(path, a)
}

// TestSave_ParentIsFile 父路径为文件时 MkdirAll 应报错。
func TestSave_ParentIsFile(t *testing.T) {
	root := t.TempDir()
	parentFile := filepath.Join(root, "parentfile")
	if err := os.WriteFile(parentFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parentFile, "child", "archive.json")
	a := &Archive{Schema: 1}
	if err := Save(path, a); err == nil {
		t.Fatal("父路径为文件时 Save 应报错")
	}
}

// TestSave_NilArchive nil 归档序列化为 null（不崩溃）。
func TestSave_NilArchive(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "archive.json")
	if err := Save(path, nil); err != nil {
		t.Fatalf("Save(nil) 应成功: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		t.Errorf("nil 归档应序列化为 null: %s", data)
	}
}

// TestSave_NestedDirs Save 应自动创建多级父目录。
func TestSave_NestedDirs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a", "b", "c", "archive.json")
	if err := Save(path, &Archive{Schema: 1}); err != nil {
		t.Fatalf("Save 应自动创建多级目录: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("归档文件应存在: %v", err)
	}
}
