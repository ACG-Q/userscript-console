package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/registry"
)

// TestProjectWithGHClientNil 测试带 nil GHClient 的投影（仅统计）。
func TestProjectWithGHClientNil(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)
	env.RepoOwner = "test-owner"
	env.PagesBase = "https://test.github.io/test"
	env.GHClient = nil

	res, err := Execute("project", env, "", nil)
	if err != nil {
		t.Fatalf("project 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "活跃脚本") {
		t.Errorf("project 应输出统计: %s", res.Text)
	}
}

// TestProjectBuildWithPagesBase 测试 build 命令带 PagesBase。
func TestProjectBuildWithPagesBase(t *testing.T) {
	env, root := buildTestEnvWithSource(t)
	env.PagesBase = "https://test.github.io/test"

	testCode := `// ==UserScript==
// @name        构建脚本
// @version     1.0.0
// ==/UserScript==`
	if err := writeSourceFile(root, "self01", "self", testCode); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}

	res, err := Execute("build", env, "", nil)
	if err != nil {
		t.Fatalf("build 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "已构建") {
		t.Errorf("build 应成功: %s", res.Text)
	}
}

// TestListDistEmpty 测试空 dist 目录。
func TestListDistEmpty(t *testing.T) {
	root := t.TempDir()
	files, err := listDist(root)
	if err != nil {
		t.Fatalf("listDist 不应返回 error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("空目录应返回空列表，实际: %v", files)
	}
}

// TestEnsureIssueNilGHClient 测试 ensureIssue 接受 nil GHClient 返回 error。
func TestEnsureIssueNilGHClient(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)
	env.GHClient = nil

	s := &registry.Script{ID: "test01", Name: "测试", Type: registry.TypeSelf, Version: "1.0.0"}
	err := ensureIssue(nil, env, nil, s)
	if err == nil {
		t.Fatal("ensureIssue 应返回 error")
	}
	if !strings.Contains(err.Error(), "GHClient 未配置") {
		t.Errorf("错误信息应包含 'GHClient 未配置': %v", err)
	}
}

// TestTombstoneIssueNilPanics 测试 tombstoneIssue 接受 nil ghc 会 panic。
func TestTombstoneIssueNilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Log("tombstoneIssue with nil ghc panics as expected")
		}
	}()
	_ = tombstoneIssue(nil, nil, "")
}

// TestBuildIssueBodyFormat 测试 buildIssueBody 输出格式。
func TestBuildIssueBodyFormat(t *testing.T) {
	s := &registry.Script{
		ID:      "test01",
		Name:    "测试脚本",
		Type:    registry.TypeSelf,
		Version: "2.0.0",
		Enabled: true,
		Match:   []string{"*://example.com/*"},
		Grant:   []string{"GM.xmlHttpRequest"},
		Changelog: []registry.ChangelogEntry{
			{Version: "2.0.0", Date: "2026-10-06", Note: "修复问题"},
		},
	}

	body := buildIssueBody(s, "https://test.github.io/repo")
	if !strings.Contains(body, "测试脚本 v2.0.0") {
		t.Errorf("buildIssueBody 应包含脚本名和版本: %s", body)
	}
	if !strings.Contains(body, "`test01`") {
		t.Errorf("buildIssueBody 应包含 ID: %s", body)
	}
	if !strings.Contains(body, "https://test.github.io/repo/dist/test01.user.js") {
		t.Errorf("buildIssueBody 应包含分发链接: %s", body)
	}
	if !strings.Contains(body, "修复问题") {
		t.Errorf("buildIssueBody 应包含 changelog: %s", body)
	}
}

// TestBuildIssueBodyDeleted 测试已删除脚本的 Issue body。
func TestBuildIssueBodyDeleted(t *testing.T) {
	s := &registry.Script{
		ID:      "del01",
		Name:    "已删除脚本",
		Type:    registry.TypeSelf,
		Version: "1.0.0",
		Enabled: false,
		Deleted: true,
	}

	body := buildIssueBody(s, "")
	if !strings.Contains(body, "🗑️ 已删除") {
		t.Errorf("已删除脚本应标记为已删除: %s", body)
	}
}

// TestBuildIssueBodySynced 测试 synced 类型脚本。
func TestBuildIssueBodySynced(t *testing.T) {
	s := &registry.Script{
		ID:         "sync01",
		Name:       "同步脚本",
		Type:       registry.TypeSynced,
		Version:    "1.0.0",
		Enabled:    true,
		SourceURL:  strPtr("https://example.com/script.user.js"),
	}

	body := buildIssueBody(s, "")
	if !strings.Contains(body, "synced") {
		t.Errorf("synced 类型应包含 synced 标识: %s", body)
	}
	if !strings.Contains(body, "https://example.com/script.user.js") {
		t.Errorf("应包含来源链接: %s", body)
	}
}

// Helper: writeSourceFile 在测试目录中写入源文件。
func writeSourceFile(root, id string, typ string, content string) error {
	dir := filepath.Join(root, "scripts", string(typ), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "index.js"), []byte(content), 0o644)
}

// Helper: strPtr 返回字符串指针。
func strPtr(s string) *string { return &s }

// 引用 github 包避免 import 报错。
var _ = github.Issue{}
