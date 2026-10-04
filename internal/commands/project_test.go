package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/registry"
)

// ── runProject 集成测试 ────────────────────────────────────────

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

// TestRunProjectTombstone 测试投影含已删除脚本（触发 tombstoneIssue）。
func TestRunProjectTombstone(t *testing.T) {
	respBody := `{"data":{"updateIssue":{"issue":{"id":"I_del","number":9,"title":"T","body":"B","state":"OPEN"}}}}`
	ghc, err := github.New("tok", "o/r",
		github.WithDoer(&fakeGHDoer{resp: respBody}),
		github.WithRetry(0),
	)
	if err != nil {
		t.Fatalf("创建 GHClient 失败: %v", err)
	}
	env, _ := buildTestEnvWithSource(t)
	env.GHClient = ghc
	env.RepoOwner = "o"
	env.PagesBase = "https://test.github.io/repo"
	reg, _ := loadReg(env)
	for i := range reg.Scripts {
		if reg.Scripts[i].ID == "del01" && reg.Scripts[i].Issue == nil {
			reg.Scripts[i].Issue = &registry.IssueRef{Number: 9, NodeID: "I_del", URL: "https://gh.io/9"}
		}
	}
	saveReg(env, reg)

	res, err := Execute("project", env, "", nil)
	if err != nil {
		t.Fatalf("project 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "投影统计") {
		t.Errorf("project 应输出统计: %s", res.Text)
	}
}

// ── ensureIssue 集成测试（通过 runProject） ─────────────────────

// TestRunProjectCreateIssue 测试项目为无 Issue 的脚本创建 Issue。
func TestRunProjectCreateIssue(t *testing.T) {
	ghc, err := github.New("tok", "o/r",
		github.WithDoer(&fakeListIssuesDoer{issues: []github.Issue{}}),
		github.WithRetry(0),
	)
	if err != nil {
		t.Fatalf("创建 GHClient 失败: %v", err)
	}
	env, _ := buildTestEnvWithSource(t)
	env.GHClient = ghc
	env.RepoOwner = "o"
	env.PagesBase = "https://test.github.io/repo"

	// 让某个脚本没有 Issue
	reg, _ := loadReg(env)
	for i := range reg.Scripts {
		if reg.Scripts[i].ID == "self01" {
			reg.Scripts[i].Issue = nil
		}
	}
	saveReg(env, reg)

	res, err := Execute("project", env, "", nil)
	if err != nil {
		t.Fatalf("project 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "创建 Issue") {
		t.Errorf("project 应输出创建 Issue 信息: %s", res.Text)
	}
}

// ── archive 辅助测试 ─────────────────────────────────────────

func TestSaveArchive(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "archive", "commands.json")
	arch := &archive{
		Schema: 1,
		Commands: []commandItem{
			{Command: "add", Author: "u1", CreatedAt: "2026-01-01T00:00:00Z",
				Results: []resultItem{{ID: "r1", Author: "u1", Body: "/add url", CreatedAt: "2026-01-01T00:00:00Z"}}},
		},
	}
	if err := saveArchive(path, arch); err != nil {
		t.Fatalf("saveArchive 失败: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取归档失败: %v", err)
	}
	var decoded archive
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if decoded.Schema != 1 {
		t.Errorf("Schema = %d, want 1", decoded.Schema)
	}
	if len(decoded.Commands) != 1 {
		t.Errorf("Commands len = %d, want 1", len(decoded.Commands))
	}
}

func TestLoadArchive(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "archive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "commands.json")
	payload := `{"schema":1,"commands":[{"command":"add","author":"u","created_at":"2026-01-01T00:00:00Z","results":[]}]}`
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	arch, err := loadArchive(path)
	if err != nil {
		t.Fatalf("loadArchive 失败: %v", err)
	}
	if arch.Schema != 1 {
		t.Errorf("Schema = %d, want 1", arch.Schema)
	}
	if len(arch.Commands) != 1 {
		t.Errorf("Commands len = %d, want 1", len(arch.Commands))
	}
}

func TestLoadArchiveNotExist(t *testing.T) {
	_, err := loadArchive(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("loadArchive 不存在应返回 error")
	}
}

func TestLoadArchiveBadJSON(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadArchive(path)
	if err == nil {
		t.Fatal("loadArchive 坏 JSON 应返回 error")
	}
	if !strings.Contains(err.Error(), "解析归档") {
		t.Errorf("错误信息应含 '解析归档': %v", err)
	}
}

// ── distURL ───────────────────────────────────────────────────

func TestDistURL(t *testing.T) {
	if got := distURL(&Env{PagesBase: "https://test.github.io/repo"}, "self01"); got != "https://test.github.io/repo/dist/self01.user.js" {
		t.Errorf("distURL = %q, want https://test.github.io/repo/dist/self01.user.js", got)
	}
	if got := distURL(&Env{PagesBase: ""}, "self01"); got != "" {
		t.Errorf("distURL 空 PagesBase = %q, want empty", got)
	}
	// trailing slash 被正确修剪
	if got := distURL(&Env{PagesBase: "https://test.github.io/repo/"}, "self01"); got != "https://test.github.io/repo/dist/self01.user.js" {
		t.Errorf("distURL 修剪尾部斜杠: got %q, want https://test.github.io/repo/dist/self01.user.js", got)
	}
}

// ── listDist with files ───────────────────────────────────────

func TestListDistWithFiles(t *testing.T) {
	root := t.TempDir()
	distDir := filepath.Join(root, "dist")
	os.MkdirAll(distDir, 0o755)
	os.WriteFile(filepath.Join(distDir, "self01.user.js"), []byte("// test"), 0o644)
	os.WriteFile(filepath.Join(distDir, "self02.user.js"), []byte("// test2"), 0o644)
	os.MkdirAll(filepath.Join(distDir, "subdir"), 0o755)

	files, err := listDist(root)
	if err != nil {
		t.Fatalf("listDist 失败: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("listDist 应返回 2 个文件，实际 %d: %v", len(files), files)
	}
}

// ── loadReg / saveReg 错误路径 ─────────────────────────────────

func TestLoadRegEmptyRoot(t *testing.T) {
	env := &Env{Root: ""}
	_, err := loadReg(env)
	if err == nil {
		t.Fatal("loadReg 空 Root 应返回 error")
	}
	if !strings.Contains(err.Error(), "数据根") {
		t.Errorf("错误信息应含 '数据根': %v", err)
	}
}

func TestLoadRegMissingFile(t *testing.T) {
	root := t.TempDir()
	env := &Env{Root: root}
	_, err := loadReg(env)
	if err == nil {
		t.Fatal("loadReg 缺少 registry.json 应返回 error")
	}
	if !strings.Contains(err.Error(), "registry.json") {
		t.Errorf("错误信息应含 'registry.json': %v", err)
	}
}

func TestSaveRegSuccess(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)
	r, _ := loadReg(env)
	changed, err := saveReg(env, r)
	if err != nil {
		t.Fatalf("saveReg 不应失败: %v", err)
	}
	if !changed {
		t.Error("saveReg 成功应返回 changed=true")
	}
}

// ── nowOf 可注入 ──────────────────────────────────────────────

func TestNowOf(t *testing.T) {
	fixedTime := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	env := &Env{Now: fixedTime}
	if got := nowOf(env); !got.Equal(fixedTime) {
		t.Errorf("nowOf = %v, want %v", got, fixedTime)
	}
	// 零值 → time.Now()
	env2 := &Env{}
	got := nowOf(env2)
	if got.IsZero() {
		t.Error("nowOf 零值不应返回零时间")
	}
}

// ── deletedHint ───────────────────────────────────────────────

func TestDeletedHint(t *testing.T) {
	if got := deletedHint(0); got != "" {
		t.Errorf("deletedHint(0) = %q, want empty", got)
	}
	if got := deletedHint(3); got != "（另有 3 个已删除）" {
		t.Errorf("deletedHint(3) = %q", got)
	}
	if got := deletedHint(-1); got != "" {
		t.Errorf("deletedHint(-1) = %q, want empty", got)
	}
}

// ── Helper types ──────────────────────────────────────────────

type fakeGHDoer struct {
	resp string
	err  error
}

func (f *fakeGHDoer) Do(req *http.Request) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(f.resp))}, nil
}

type fakeListIssuesDoer struct {
	issues []github.Issue
	err    error
}

func (f *fakeListIssuesDoer) Do(req *http.Request) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	data, _ := json.Marshal(map[string]any{"data": map[string]any{
		"repository": map[string]any{"issues": map[string]any{
			"nodes": func() []any {
				nodes := make([]any, len(f.issues))
				for i, iss := range f.issues {
					nodes[i] = map[string]any{
						"id":     iss.NodeID,
						"number": iss.Number,
						"title":  iss.Title,
					}
				}
				return nodes
			}(),
		}},
	}})
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
}

// 引用 github 包避免 import 报错。
var _ = github.Issue{}
var _ = fmt.Errorf // 防止 unused import
