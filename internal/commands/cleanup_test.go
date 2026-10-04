package commands

import (
	"encoding/json"
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
	archiveDir := filepath.Join(root, "..", "archive")
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
		"body":      "/list\n/rm s2\n/info\n",
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
		// DELETE_COMMENT_MUTATION 响应 × 2
		`{"data":{"deleteComment":{"clientMutationId":"x"}}}`,
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
	if !strings.Contains(res.Text, "已删除评论: 2") {
		t.Logf("实际输出: %s", res.Text)
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
