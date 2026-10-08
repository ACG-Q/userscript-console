package projector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/registry"
)

// ── fake doer ────────────────────────────────────────────────

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

// multiRespDoer 按调用序返回预置响应。
type multiRespDoer struct {
	t         *testing.T
	resps     []string
	calls     int
	lastQuery string
}

func (m *multiRespDoer) Do(req *http.Request) (*http.Response, error) {
	b, _ := io.ReadAll(req.Body)
	var m2 map[string]any
	_ = json.Unmarshal(b, &m2)
	q, _ := m2["query"].(string)
	m.lastQuery = q

	idx := m.calls
	m.calls++
	if idx >= len(m.resps) {
		m.t.Errorf("第 %d 次调用无预置响应 (calls=%d, resps=%d, query=%q)", idx, m.calls, len(m.resps), q)
		return nil, fmt.Errorf("无预置响应")
	}
	body := m.resps[idx]
	m.t.Logf("调用 %d: query=%q body=%s", idx, q, body[:min(len(body), 80)])
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
}

// ── GHClient helpers ──────────────────────────────────────────

func newTestGHClient(t *testing.T, doers ...interface{}) *github.Client {
	t.Helper()
	if len(doers) == 0 {
		return nil
	}
	first := doers[0]
	opts := []github.Option{github.WithRetry(0)}
	switch d := first.(type) {
	case *fakeGHDoer:
		opts = append(opts, github.WithDoer(d))
	case *multiRespDoer:
		opts = append(opts, github.WithDoer(d))
	}
	c, err := github.New("tok", "o/r", opts...)
	if err != nil {
		t.Fatalf("创建 GHClient 失败: %v", err)
	}
	return c
}

// ── TestProjectNoGHClient ─────────────────────────────────────

func TestProjectNoGHClient(t *testing.T) {
	r := &registry.Registry{
		Schema: registry.SchemaVersion,
		Scripts: []registry.Script{
			{ID: "s1", Type: registry.TypeSelf, Name: "测试", Version: "1.0.0", Enabled: true, Deleted: false},
		},
	}
	env := &Env{Root: "/tmp", RepoOwner: "o"}
	res, err := Project(context.Background(), env, r)
	if err != nil {
		t.Fatalf("Project 不应返回 error: %v", err)
	}
	if res.Active != 1 {
		t.Errorf("Active = %d, want 1", res.Active)
	}
	if res.Created != 0 {
		t.Errorf("Created = %d, want 0（无 GHClient）", res.Created)
	}
}

// ── TestProjectDeletedScript ──────────────────────────────────

func TestProjectDeletedScript(t *testing.T) {
	respBody := `{"data":{"updateIssue":{"issue":{"id":"I_del","number":9,"title":"T","body":"B","state":"CLOSED"}}}}`
	ghc := newTestGHClient(t, &fakeGHDoer{resp: respBody})
	r := &registry.Registry{
		Schema: registry.SchemaVersion,
		Scripts: []registry.Script{
			{
				ID: "del1", Type: registry.TypeSelf, Name: "已删", Version: "1.0.0", Enabled: true, Deleted: true,
				Issue: &registry.IssueRef{NodeID: "I_del"},
			},
		},
	}
	env := &Env{Root: "/tmp", RepoOwner: "o", GHClient: ghc}
	res, err := Project(context.Background(), env, r)
	if err != nil {
		t.Fatalf("Project 不应返回 error: %v", err)
	}
	if res.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1", res.Deleted)
	}
	if res.Updated != 1 {
		t.Errorf("Updated = %d, want 1", res.Updated)
	}
}

// ── TestProjectNilGHClient ────────────────────────────────────

func TestProjectNilGHClient(t *testing.T) {
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		{ID: "s1", Type: registry.TypeSelf, Name: "测试", Version: "1.0.0", Enabled: true, Deleted: false},
	}}
	env := &Env{Root: "/tmp", GHClient: nil}
	res, err := Project(context.Background(), env, r)
	if err != nil {
		t.Fatalf("Project 不应返回 error: %v", err)
	}
	if res.Active != 1 {
		t.Errorf("Active = %d, want 1", res.Active)
	}
}

// ── TestEnsureIssueCreate ─────────────────────────────────────

func TestEnsureIssueCreate(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		// 1. ListRepoIssues（空列表）
		`{"data":{"repository":{"issues":{"totalCount":0,"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[]}}}}`,
		// 2. repoID（CreateIssue 内部调用）
		`{"data":{"repository":{"id":"R_1","nameWithOwner":"o/r","issue":null}}}`,
		// 3. CreateIssue
		`{"data":{"createIssue":{"issue":{"id":"I_new","number":42,"state":"OPEN","title":"T","body":"B","createdAt":"2026-01-01T00:00:00Z"}}}}`,
	}}
	ghc := newTestGHClient(t, d)
	r := &registry.Registry{Schema: registry.SchemaVersion}
	s := &registry.Script{
		ID: "new01", Name: "新脚本", Version: "1.0.0",
		Type: registry.TypeSelf, Enabled: true, Deleted: false,
		Changelog: []registry.ChangelogEntry{{Version: "1.0.0", Date: "2026-01-01", Note: "init"}},
	}
	env := &Env{Root: "/tmp", RepoOwner: "o", PagesBase: "https://test.github.io/repo", GHClient: ghc}
	err := EnsureIssue(context.Background(), env, r, s)
	if err != nil {
		t.Fatalf("EnsureIssue 应成功: %v (query=%q)", err, d.lastQuery)
	}
	if s.Issue == nil {
		t.Fatal("Issue 应被设置")
	}
	if s.Issue.Number != 42 {
		t.Errorf("Number = %d, want 42", s.Issue.Number)
	}
	if s.Issue.NodeID != "I_new" {
		t.Errorf("NodeID = %q, want I_new", s.Issue.NodeID)
	}
	if s.Issue.URL != "https://github.com/o/issues/42" {
		t.Errorf("URL = %q, want https://github.com/o/issues/42", s.Issue.URL)
	}
}

// ── TestEnsureIssueUpdate ─────────────────────────────────────

func TestEnsureIssueUpdate(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		// 1. ListRepoIssues（含匹配标题的 issue）
		`{"data":{"repository":{"issues":{"totalCount":1,"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[{"id":"I_old","number":7,"state":"OPEN","title":"[新脚本] v1.0.0","body":"B","createdAt":"2026-01-01T00:00:00Z"}]}}}}`,
		// 2. UpdateIssue
		`{"data":{"updateIssue":{"issue":{"id":"I_old","number":7,"state":"OPEN"}}}}`,
	}}
	ghc := newTestGHClient(t, d)
	r := &registry.Registry{Schema: registry.SchemaVersion}
	s := &registry.Script{
		ID: "new01", Name: "新脚本", Version: "1.1.0",
		Type: registry.TypeSelf, Enabled: true, Deleted: false,
		Changelog: []registry.ChangelogEntry{{Version: "1.1.0", Date: "2026-01-02", Note: "修复"}},
	}
	env := &Env{Root: "/tmp", RepoOwner: "o", PagesBase: "", GHClient: ghc}
	err := EnsureIssue(context.Background(), env, r, s)
	if err != nil {
		t.Fatalf("EnsureIssue 应成功: %v (query=%q)", err, d.lastQuery)
	}
	if s.Issue.Number != 7 {
		t.Errorf("Number = %d, want 7", s.Issue.Number)
	}
	if s.Issue.NodeID != "I_old" {
		t.Errorf("NodeID = %q, want I_old", s.Issue.NodeID)
	}
}

// ── TestEnsureIssueListError ──────────────────────────────────

func TestEnsureIssueListError(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		// 1. ListRepoIssues 失败
		`{"errors":[{"message":"forbidden"}]}`,
	}}
	ghc := newTestGHClient(t, d)
	r := &registry.Registry{Schema: registry.SchemaVersion}
	s := &registry.Script{ID: "s1", Name: "x", Version: "1.0.0", Type: registry.TypeSelf, Enabled: true, Deleted: false}
	env := &Env{Root: "/tmp", RepoOwner: "o", GHClient: ghc}
	err := EnsureIssue(context.Background(), env, r, s)
	if err == nil {
		t.Fatal("EnsureIssue 列表失败应返回 error")
	}
}

// ── TestEnsureIssueCreateError ────────────────────────────────

func TestEnsureIssueCreateError(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		// 1. ListRepoIssues 空
		`{"data":{"repository":{"issues":{"totalCount":0,"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[]}}}}`,
		// 2. repoID
		`{"data":{"repository":{"id":"R_1","nameWithOwner":"o/r","issue":null}}}`,
		// 3. CreateIssue 失败
		`{"errors":[{"message":"title required"}]}`,
	}}
	ghc := newTestGHClient(t, d)
	r := &registry.Registry{Schema: registry.SchemaVersion}
	s := &registry.Script{ID: "s1", Name: "x", Version: "1.0.0", Type: registry.TypeSelf, Enabled: true, Deleted: false}
	env := &Env{Root: "/tmp", RepoOwner: "o", GHClient: ghc}
	err := EnsureIssue(context.Background(), env, r, s)
	if err == nil {
		t.Fatal("EnsureIssue 创建失败应返回 error")
	}
}

// ── TestBuildIssueBody ────────────────────────────────────────

func TestBuildIssueBody(t *testing.T) {
	s := &registry.Script{
		ID:          "abc",
		Name:        "MyScript",
		Version:     "2.0.0",
		Type:        registry.TypeSelf,
		Enabled:     true,
		Deleted:     false,
		Description: "A test script",
		Match:       []string{"*://*/*"},
		Grant:       []string{"none"},
		Changelog:   []registry.ChangelogEntry{{Version: "2.0.0", Date: "2026-01-01", Note: "更新"}},
	}
	body := BuildIssueBody(s, "https://test.github.io/repo")
	if !strings.Contains(body, "MyScript v2.0.0") {
		t.Errorf("body 应含脚本名和版本: %s", body)
	}
	if !strings.Contains(body, "`abc`") {
		t.Errorf("body 应含 ID: %s", body)
	}
	if !strings.Contains(body, "https://test.github.io/repo/dist/abc.user.js") {
		t.Errorf("body 应含分发链接: %s", body)
	}
}

func TestBuildIssueBodyNoPagesBase(t *testing.T) {
	s := &registry.Script{ID: "abc", Name: "X", Version: "1.0.0", Type: registry.TypeSelf, Enabled: true, Deleted: false}
	body := BuildIssueBody(s, "")
	if strings.Contains(body, "分发") {
		t.Error("无 PagesBase 时不应含分发链接")
	}
}

func TestBuildIssueBodyDeleted(t *testing.T) {
	s := &registry.Script{
		ID:      "del01",
		Name:    "已删除脚本",
		Type:    registry.TypeSelf,
		Version: "1.0.0",
		Enabled: false,
		Deleted: true,
	}
	body := BuildIssueBody(s, "")
	if !strings.Contains(body, "🗑️ 已删除") {
		t.Errorf("已删除脚本应标记为已删除: %s", body)
	}
}

func TestBuildIssueBodySynced(t *testing.T) {
	s := &registry.Script{
		ID:        "sync01",
		Name:      "同步脚本",
		Type:      registry.TypeSynced,
		Version:   "1.0.0",
		Enabled:   true,
		SourceURL: strPtr("https://example.com/script.user.js"),
	}
	body := BuildIssueBody(s, "")
	if !strings.Contains(body, "synced") {
		t.Errorf("synced 类型应包含 synced 标识: %s", body)
	}
	if !strings.Contains(body, "https://example.com/script.user.js") {
		t.Errorf("应包含来源链接: %s", body)
	}
}

// ── TestTombstoneIssue ────────────────────────────────────────

func TestTombstoneIssueSuccess(t *testing.T) {
	respBody := `{"data":{"updateIssue":{"issue":{"id":"I_1","number":1,"state":"OPEN"}}}}`
	ghc := newTestGHClient(t, &fakeGHDoer{resp: respBody})
	ctx := context.Background()
	if err := TombstoneIssue(ctx, ghc, "I_1"); err != nil {
		t.Fatalf("tombstoneIssue 成功应返回 nil: %v", err)
	}
}

func TestTombstoneIssueError(t *testing.T) {
	ghc := newTestGHClient(t, &fakeGHDoer{err: fmt.Errorf("network")})
	ctx := context.Background()
	err := TombstoneIssue(ctx, ghc, "I_1")
	if err == nil {
		t.Fatal("tombstoneIssue 网络错误应返回 error")
	}
	if !strings.Contains(err.Error(), "network") {
		t.Errorf("错误信息应含 'network': %v", err)
	}
}

func TestTombstoneIssueNilClient(t *testing.T) {
	err := TombstoneIssue(context.Background(), nil, "I_1")
	if err == nil {
		t.Fatal("TombstoneIssue nil client 应返回 error")
	}
}

// ── TestStatusLabel ───────────────────────────────────────────

func TestStatusLabel(t *testing.T) {
	if got := statusLabel(true, false); got != "✅ 启用" {
		t.Errorf("statusLabel(enabled) = %q", got)
	}
	if got := statusLabel(false, true); got != "🗑️ 已删除" {
		t.Errorf("statusLabel(deleted) = %q", got)
	}
	if got := statusLabel(false, false); got != "⏸️ 停用" {
		t.Errorf("statusLabel(stopped) = %q", got)
	}
}

// ── TestScriptTypeLabel ───────────────────────────────────────

func TestScriptTypeLabel(t *testing.T) {
	if got := scriptTypeLabel("self"); got != "self（自写脚本）" {
		t.Errorf("scriptTypeLabel(self) = %q", got)
	}
	if got := scriptTypeLabel("synced"); got != "synced（同步脚本）" {
		t.Errorf("scriptTypeLabel(synced) = %q", got)
	}
	if got := scriptTypeLabel("unknown"); got != "unknown" {
		t.Errorf("scriptTypeLabel(unknown) = %q", got)
	}
}

// ── TestOrDash ────────────────────────────────────────────────

func TestOrDash(t *testing.T) {
	if got := orDash(""); got != "—" {
		t.Errorf("orDash(\"\") = %q, want —", got)
	}
	if got := orDash("   "); got != "—" {
		t.Errorf("orDash(\"   \") = %q, want —", got)
	}
	if got := orDash("test"); got != "test" {
		t.Errorf("orDash(\"test\") = %q, want test", got)
	}
}

// ── TestNowRFC3339 ────────────────────────────────────────────

func TestNowRFC3339(t *testing.T) {
	s := nowRFC3339()
	_, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("nowRFC3339 输出不是合法 RFC3339: %q", s)
	}
}

// ── helper ────────────────────────────────────────────────────

func strPtr(s string) *string { return &s }
