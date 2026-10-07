package commands

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acg-q/userscript-console/internal/cleanup"
	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/registry"
)

// ── fake GH client for cleanup tests ──────────────────────────

type cleanupDoer struct {
	t     *testing.T
	calls int
	resps []string
}

func (d *cleanupDoer) Do(req *http.Request) (*http.Response, error) {
	idx := d.calls
	d.calls++
	if idx >= len(d.resps) {
		d.t.Fatalf("第 %d 次调用无预置响应 (calls=%d resps=%d)", idx, d.calls, len(d.resps))
	}
	resp := d.resps[idx]
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(resp))}, nil
}

func TestCleanupWithApplyAndDelete(t *testing.T) {
	root := t.TempDir()
	regPath := filepath.Join(root, "registry.json")
	reg := &registry.Registry{
		Schema: registry.SchemaVersion,
		Scripts: []registry.Script{
			{ID: "s1", Type: registry.TypeSelf, Name: "测试", Version: "1.0.0", Enabled: true, Deleted: false},
		},
	}
	data, _ := json.Marshal(reg)
	os.WriteFile(regPath, data, 0o644)

	// 写入旧归档（含 2 条过期评论）
	archiveDir := filepath.Join(root, "archive")
	os.MkdirAll(archiveDir, 0o755)
	archivePath := filepath.Join(archiveDir, "commands.json")
	oldArchive := &cleanup.Archive{
		Schema: 1,
		Commands: []cleanup.CommandKey{
			{Command: "add", Author: "u", CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				Results: []cleanup.Result{
					{ID: "old1", Author: "u", Body: "/add https://old.com", CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
					{ID: "old2", Author: "u", Body: "/add https://old2.com", CreatedAt: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)},
				}},
		},
	}
	cleanup.Save(archivePath, oldArchive)

	// 构造 fake GHClient：使用与 github 包测试完全相同的数据格式
	d := &cleanupDoer{t: t}
	comment := map[string]any{
		"id":        "new1",
		"author":    map[string]any{"login": "u1"},
		"body":      "/add https://new1.com\n/list\n/rm s1",
		"createdAt": "2026-10-01T00:00:00Z",
	}
	comment2 := map[string]any{
		"id":        "new2",
		"author":    map[string]any{"login": "u2"},
		"body":      "/add https://new2.com",
		"createdAt": "2026-10-02T00:00:00Z",
	}
	// PANEL_QUERY 响应
	d.resps = append(d.resps,
		func() string {
			b, _ := json.Marshal(map[string]any{
				"data": map[string]any{
					"repository": map[string]any{
						"issue": map[string]any{
							"comments": map[string]any{
								"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
								"nodes":    []any{comment, comment2},
							},
						},
					},
				},
			})
			return string(b)
		}(),
		// DELETE_COMMENT_MUTATION 响应（keep=1 仅删 new1 一条）
		`{"data":{"deleteComment":{"clientMutationId":"x"}}}`,
	)

	env := &Env{
		Root:        root,
		RepoOwner:   "test-owner",
		RepoName:    "test-owner/test-repo",
		IssueNumber: 1,
		GHClient:    newCleanupGHClient(t, d),
		Now:         time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
	}
	res, err := Execute("cleanup", env, "--apply --keep 1", nil)
	if err != nil {
		t.Fatalf("cleanup --apply 失败: %v", err)
	}
	if !strings.Contains(res.Text, "cleanup 完成") {
		t.Errorf("应有清理完成消息: %s", res.Text)
	}
	if !strings.Contains(res.Text, "已删除评论: 1") {
		t.Errorf("keep=1 时应删除 1 条（new1），输出: %s", res.Text)
	}

	// 回归：归档必须落在数据根 <root>/archive/commands.json，
	// 与 doctor（cli.doctorProblems）和 pages.renderCommands 的读取路径一致。
	if _, err := os.Stat(filepath.Join(root, "archive", "commands.json")); err != nil {
		t.Errorf("归档应写入 <root>/archive/commands.json: %v", err)
	}
}

func newCleanupGHClient(t *testing.T, d *cleanupDoer) *github.Client {
	c, err := github.New("tok", "test-owner/test-repo",
		github.WithDoer(d),
		github.WithRetry(0),
	)
	if err != nil {
		t.Fatalf("创建 GHClient 失败: %v", err)
	}
	return c
}

func TestCleanupDryRunNoGHClient(t *testing.T) {
	root := t.TempDir()
	regPath := filepath.Join(root, "registry.json")
	reg := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		{ID: "s1", Type: registry.TypeSelf, Name: "测试", Version: "1.0.0", Enabled: true, Deleted: false},
	}}
	data, _ := json.Marshal(reg)
	os.WriteFile(regPath, data, 0o644)
	env := &Env{Root: root, RepoOwner: "o", IssueNumber: 1}
	res, err := Execute("cleanup", env, "", nil)
	if err != nil {
		t.Fatalf("cleanup 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "dry-run") {
		t.Errorf("无 GHClient 应走 dry-run 路径: %s", res.Text)
	}
}

// TestCleanupSaveFailureSkipsDelete 归档保存失败时一条评论都不得删除（SPEC-DATA.md:103 先落盘再删）。
func TestCleanupSaveFailureSkipsDelete(t *testing.T) {
	root := t.TempDir()
	reg := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		{ID: "s1", Type: registry.TypeSelf, Name: "测试", Version: "1.0.0", Enabled: true},
	}}
	data, _ := json.Marshal(reg)
	os.WriteFile(filepath.Join(root, "registry.json"), data, 0o644)

	d := &cleanupDoer{t: t}
	comment := map[string]any{
		"id": "c1", "author": map[string]any{"login": "u1"},
		"body": "/add https://x.com", "createdAt": "2026-10-01T00:00:00Z",
	}
	list, _ := json.Marshal(map[string]any{
		"data": map[string]any{"repository": map[string]any{"issue": map[string]any{
			"comments": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				"nodes":    []any{comment},
			},
		}}},
	})
	// 只预置 1 条响应：若发生删除调用，doer 会因"第 2 次调用无预置响应"直接 Fatal
	d.resps = []string{string(list)}

	orig := saveArchive
	saveArchive = func(string, *cleanup.Archive) error { return errors.New("磁盘已满") }
	t.Cleanup(func() { saveArchive = orig })

	env := &Env{
		Root: root, RepoOwner: "o", RepoName: "o/r",
		IssueNumber: 1, GHClient: newCleanupGHClient(t, d),
		Now: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
	}
	_, err := Execute("cleanup", env, "--apply", nil)
	if err == nil || !strings.Contains(err.Error(), "保存归档失败") {
		t.Fatalf("应返回保存归档失败，实际: %v", err)
	}
	if d.calls != 1 {
		t.Errorf("保存失败时不得调用删除接口，实际 HTTP 调用 %d 次", d.calls)
	}
}

// TestCleanupDeleteFailureArchiveAlreadySaved 删除失败前归档必须已落盘，下轮只补删除。
func TestCleanupDeleteFailureArchiveAlreadySaved(t *testing.T) {
	root := t.TempDir()
	reg := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		{ID: "s1", Type: registry.TypeSelf, Name: "测试", Version: "1.0.0", Enabled: true},
	}}
	data, _ := json.Marshal(reg)
	os.WriteFile(filepath.Join(root, "registry.json"), data, 0o644)

	d := &cleanupDoer{t: t}
	c1 := map[string]any{"id": "c1", "author": map[string]any{"login": "u"},
		"body": "/add old", "createdAt": "2026-10-01T00:00:00Z"}
	c2 := map[string]any{"id": "c2", "author": map[string]any{"login": "u"},
		"body": "/add new", "createdAt": "2026-10-02T00:00:00Z"}
	list, _ := json.Marshal(map[string]any{
		"data": map[string]any{"repository": map[string]any{"issue": map[string]any{
			"comments": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				"nodes":    []any{c1, c2},
			},
		}}},
	})
	// keep=1 → 仅 c2 保留，删除 c1 时返回 GraphQL 错误
	d.resps = []string{string(list), `{"data":null,"errors":[{"message":"boom"}]}`}

	env := &Env{
		Root: root, RepoOwner: "o", RepoName: "o/r",
		IssueNumber: 1, GHClient: newCleanupGHClient(t, d),
		Now: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
	}
	_, err := Execute("cleanup", env, "--apply --keep 1", nil)
	if err == nil || !strings.Contains(err.Error(), "删除评论失败") {
		t.Fatalf("应返回删除评论失败，实际: %v", err)
	}
	archPath := filepath.Join(root, "archive", "commands.json")
	archData, rerr := os.ReadFile(archPath)
	if rerr != nil {
		t.Fatalf("删除失败前归档必须已保存: %v", rerr)
	}
	if !strings.Contains(string(archData), `"id": "c2"`) {
		t.Errorf("归档应含保留条目 c2，实际: %s", archData)
	}
}
