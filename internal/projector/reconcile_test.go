package projector

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/acg-q/userscript-console/internal/registry"
)

// backfillScript 构造无 Issue 关联的回填测试脚本。
func backfillScript() *registry.Script {
	return &registry.Script{
		ID: "new01", Name: "新脚本", Version: "1.0.0",
		Type: registry.TypeSelf, Enabled: true, Deleted: false,
		Changelog: []registry.ChangelogEntry{{Version: "1.0.0", Date: "2026-01-01", Note: "init"}},
	}
}

const (
	emptyListResp = `{"data":{"repository":{"issues":{"totalCount":0,"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[]}}}}`
	repoIDResp    = `{"data":{"repository":{"id":"R_1","nameWithOwner":"o/r","issue":null}}}`
	createOKResp  = `{"data":{"createIssue":{"issue":{"id":"I_new","number":42,"state":"OPEN","title":"T","body":"B","createdAt":"2026-01-01T00:00:00Z"}}}}`
)

// TestBackfillCreatesWhenNoMatch 未命中 → CreateIssue + Created++ + 写 ref + bump UpdatedAt。
func TestBackfillCreatesWhenNoMatch(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{emptyListResp, repoIDResp, createOKResp}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", PagesBase: "https://test.github.io/repo", GHClient: newTestGHClient(t, d)}
	s := backfillScript()
	var res Result
	if err := backfillIssue(context.Background(), env, s, allIssuesLoader(env), &res); err != nil {
		t.Fatalf("backfillIssue 应成功: %v (query=%q)", err, d.lastQuery)
	}
	if s.Issue == nil || s.Issue.Number != 42 || s.Issue.NodeID != "I_new" {
		t.Fatalf("Issue ref 未写入: %+v", s.Issue)
	}
	if s.Issue.URL != "https://github.com/o/r/issues/42" {
		t.Errorf("URL = %q", s.Issue.URL)
	}
	if res.Created != 1 || res.Updated != 0 || res.Reopened != 0 {
		t.Errorf("Created=%d Updated=%d Reopened=%d, want 1/0/0", res.Created, res.Updated, res.Reopened)
	}
	if s.UpdatedAt == "" {
		t.Error("回填成功应 bump UpdatedAt")
	}
	if !strings.Contains(d.lastQuery, "createIssue(input:") {
		t.Errorf("最后一次查询应为 CreateIssue, got %q", d.lastQuery)
	}
}

// TestBackfillURLFallsBackToRepoOwner RepoName 为空时 URL 回退 RepoOwner。
func TestBackfillURLFallsBackToRepoOwner(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		emptyListResp, repoIDResp,
		`{"data":{"createIssue":{"issue":{"id":"I_new","number":9,"state":"OPEN","title":"T","body":"B","createdAt":"2026-01-01T00:00:00Z"}}}}`,
	}}
	env := &Env{Root: "/tmp", RepoOwner: "o", GHClient: newTestGHClient(t, d)}
	s := backfillScript()
	var res Result
	if err := backfillIssue(context.Background(), env, s, allIssuesLoader(env), &res); err != nil {
		t.Fatalf("backfillIssue: %v", err)
	}
	if s.Issue.URL != "https://github.com/o/issues/9" {
		t.Errorf("RepoName 为空时应回退 RepoOwner: URL = %q", s.Issue.URL)
	}
}

// TestBackfillUsesALLStates 回填列表必须传 states=null（ALL），防重复建已关闭 Issue。
func TestBackfillUsesALLStates(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{emptyListResp, repoIDResp, createOKResp}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: newTestGHClient(t, d)}
	s := backfillScript()
	var res Result
	if err := backfillIssue(context.Background(), env, s, allIssuesLoader(env), &res); err != nil {
		t.Fatalf("backfillIssue: %v", err)
	}
	if v, ok := d.lastVars["states"]; ok && v != nil {
		t.Errorf("states 应为 null(ALL)，实际 %#v", v)
	}
}

// TestBackfillMatchOpenReusesIssue 命中且开着 → UpdateIssue + Updated++（不 Reopen、不 Created）。
func TestBackfillMatchOpenReusesIssue(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		`{"data":{"repository":{"issues":{"totalCount":1,"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[{"id":"I_old","number":7,"state":"OPEN","title":"[新脚本] v1.0.0","body":"B","createdAt":"2026-01-01T00:00:00Z"}]}}}}`,
		`{"data":{"updateIssue":{"issue":{"id":"I_old","number":7,"state":"OPEN"}}}}`,
	}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: newTestGHClient(t, d)}
	s := backfillScript()
	s.Version = "1.1.0"
	var res Result
	if err := backfillIssue(context.Background(), env, s, allIssuesLoader(env), &res); err != nil {
		t.Fatalf("backfillIssue: %v", err)
	}
	if s.Issue == nil || s.Issue.Number != 7 || s.Issue.NodeID != "I_old" {
		t.Fatalf("应复用命中 Issue: %+v", s.Issue)
	}
	if res.Updated != 1 || res.Created != 0 || res.Reopened != 0 {
		t.Errorf("Created=%d Updated=%d Reopened=%d, want 0/1/0", res.Created, res.Updated, res.Reopened)
	}
}

// TestBackfillClosedReopensThenUpdates 命中且已关闭 → Reopen 先于 Update + Reopened++/Updated++。
func TestBackfillClosedReopensThenUpdates(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		`{"data":{"repository":{"issues":{"totalCount":1,"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[{"id":"I_old","number":7,"state":"CLOSED","title":"[新脚本] v1.0.0","body":"B","createdAt":"2026-01-01T00:00:00Z"}]}}}}`,
		`{"data":{"reopenIssue":{"issue":{"id":"I_old","number":7,"state":"OPEN"}}}}`,
		`{"data":{"updateIssue":{"issue":{"id":"I_old","number":7,"state":"OPEN"}}}}`,
	}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: newTestGHClient(t, d)}
	s := backfillScript()
	var res Result
	if err := backfillIssue(context.Background(), env, s, allIssuesLoader(env), &res); err != nil {
		t.Fatalf("backfillIssue: %v", err)
	}
	if len(d.queries) != 3 {
		t.Fatalf("queries = %d, want 3: %v", len(d.queries), d.queries)
	}
	if !strings.Contains(d.queries[1], "reopenIssue(input:") {
		t.Errorf("第 2 次应为 ReopenIssue: %q", d.queries[1])
	}
	if !strings.Contains(d.queries[2], "updateIssue(input:") {
		t.Errorf("第 3 次应为 UpdateIssue: %q", d.queries[2])
	}
	if res.Reopened != 1 || res.Updated != 1 || res.Created != 0 {
		t.Errorf("Reopened=%d Updated=%d Created=%d, want 1/1/0", res.Reopened, res.Updated, res.Created)
	}
}

// TestBackfillListErrorCountsError 列表失败 → 返回 error，不写 ref。
func TestBackfillListErrorCountsError(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{`{"errors":[{"message":"forbidden"}]}`}}
	env := &Env{Root: "/tmp", RepoOwner: "o", GHClient: newTestGHClient(t, d)}
	s := backfillScript()
	var res Result
	if err := backfillIssue(context.Background(), env, s, allIssuesLoader(env), &res); err == nil {
		t.Fatal("列表失败应返回 error")
	}
	if s.Issue != nil {
		t.Error("失败不应写 ref")
	}
}

// TestBackfillCreateErrorCountsError 创建失败 → 返回 error。
func TestBackfillCreateErrorCountsError(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		emptyListResp, repoIDResp,
		`{"errors":[{"message":"title required"}]}`,
	}}
	env := &Env{Root: "/tmp", RepoOwner: "o", GHClient: newTestGHClient(t, d)}
	s := backfillScript()
	var res Result
	if err := backfillIssue(context.Background(), env, s, allIssuesLoader(env), &res); err == nil {
		t.Fatal("创建失败应返回 error")
	}
	if s.Issue != nil {
		t.Error("失败不应写 ref")
	}
}

// TestReconcileSingleListForManyScripts 多个无 Issue 脚本只拉一次列表（N+1 防回归）。
func TestReconcileSingleListForManyScripts(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		emptyListResp, repoIDResp,
		createOKResp,
		`{"data":{"createIssue":{"issue":{"id":"I_b","number":43,"state":"OPEN","title":"T","body":"B","createdAt":"2026-01-01T00:00:00Z"}}}}`,
	}}
	ghc := newTestGHClient(t, d)
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		{ID: "a", Name: "脚本A", Version: "1.0.0", Type: registry.TypeSelf, Enabled: true},
		{ID: "b", Name: "脚本B", Version: "1.0.0", Type: registry.TypeSelf, Enabled: true},
	}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: ghc}
	res := reconcileIssues(context.Background(), env, r)
	listCalls := 0
	for _, q := range d.queries {
		if strings.Contains(q, "issues(first") {
			listCalls++
		}
	}
	if listCalls != 1 {
		t.Errorf("ListRepoIssues 应只调 1 次，实际 %d（queries=%v）", listCalls, d.queries)
	}
	if res.Created != 2 || res.Errors != 0 {
		t.Errorf("Created=%d Errors=%d, want 2/0", res.Created, res.Errors)
	}
}

// TestReconcileAPIErrorContinues 每步 API 错误 → Errors++ 并继续下一脚本。
func TestReconcileAPIErrorContinues(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		`{"errors":[{"message":"boom"}]}`, // 脚本 a 列表失败
	}}
	ghc := newTestGHClient(t, d)
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		{ID: "a", Name: "脚本A", Version: "1.0.0", Type: registry.TypeSelf, Enabled: true},
		{ID: "b", Name: "脚本B", Version: "1.0.0", Type: registry.TypeSelf, Enabled: true},
	}}
	env := &Env{Root: "/tmp", RepoOwner: "o", GHClient: ghc}
	res := reconcileIssues(context.Background(), env, r)
	if res.Errors < 1 {
		t.Errorf("Errors = %d, want >=1", res.Errors)
	}
	if res.Active != 2 {
		t.Errorf("Active = %d, want 2", res.Active)
	}
}

// ── 判等刷新（任务 4） ────────────────────────────────────────

// repoIssueResp 构造 REPO_QUERY（GetIssue combined）响应：repository.id + issue 全字段。
func repoIssueResp(t *testing.T, number int, title, body, state string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{
		"id": "R_1", "nameWithOwner": "o/r",
		"issue": map[string]any{
			"id": "I_1", "number": number, "title": title, "body": body, "state": state,
			"createdAt": "2026-01-01T00:00:00Z", "updatedAt": "2026-01-01T00:00:00Z",
			"author":   map[string]any{"login": "bot"},
			"comments": map[string]any{"totalCount": 0},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// refreshScript 构造已有关联 Issue 的测试脚本。
func refreshScript() *registry.Script {
	return &registry.Script{
		ID: "r1", Name: "测试", Version: "1.2.0", Type: registry.TypeSelf, Enabled: true,
		UpdatedAt: "2026-01-01T00:00:00Z",
		Issue:     &registry.IssueRef{Number: 5, NodeID: "I_5", URL: "https://github.com/o/r/issues/5"},
	}
}

// refreshEnv 返回已关联 Issue 脚本的环境（d 为 nil 时 GHClient 留空，由调用方赋值）。
func refreshEnv(t *testing.T, d *multiRespDoer) (*Env, *registry.Script) {
	t.Helper()
	env := &Env{
		Root: "/tmp", RepoOwner: "o", RepoName: "o/r",
		PagesBase: "https://test.github.io/repo",
	}
	if d != nil {
		env.GHClient = newTestGHClient(t, d)
	}
	return env, refreshScript()
}

// TestRefreshIssueEqualSkipsUpdate 标题/正文一致 → 只发 1 次 GetIssue，不写、不 bump UpdatedAt。
func TestRefreshIssueEqualSkipsUpdate(t *testing.T) {
	env, s := refreshEnv(t, nil)
	title := "[测试] v1.2.0"
	body := BuildIssueBody(s, env.PagesBase, env.distSeg())
	d := &multiRespDoer{t: t, resps: []string{repoIssueResp(t, 5, title, body, "OPEN")}}
	env.GHClient = newTestGHClient(t, d)

	var res Result
	if err := refreshIssue(context.Background(), env, s, &res); err != nil {
		t.Fatalf("refreshIssue: %v", err)
	}
	if res.Updated != 0 || res.Reopened != 0 {
		t.Errorf("Updated=%d Reopened=%d, want 0/0", res.Updated, res.Reopened)
	}
	if s.UpdatedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("相等刷新不应 bump UpdatedAt: %s", s.UpdatedAt)
	}
	if d.calls != 1 {
		t.Errorf("calls = %d, want 1（仅 GetIssue）", d.calls)
	}
}

// TestRefreshIssueTitleMismatchUpdates 标题不符 → UpdateIssue + Updated++。
func TestRefreshIssueTitleMismatchUpdates(t *testing.T) {
	env, s := refreshEnv(t, nil)
	d := &multiRespDoer{t: t, resps: []string{
		repoIssueResp(t, 5, "[测试] v0.9.0", BuildIssueBody(s, env.PagesBase, env.distSeg()), "OPEN"),
		`{"data":{"updateIssue":{"issue":{"id":"I_5","number":5,"state":"OPEN"}}}}`,
	}}
	env.GHClient = newTestGHClient(t, d)

	var res Result
	if err := refreshIssue(context.Background(), env, s, &res); err != nil {
		t.Fatalf("refreshIssue: %v", err)
	}
	if res.Updated != 1 || res.Reopened != 0 {
		t.Errorf("Updated=%d Reopened=%d, want 1/0", res.Updated, res.Reopened)
	}
	if len(d.queries) != 2 || !strings.Contains(d.queries[1], "updateIssue(input:") {
		t.Errorf("第 2 次应为 UpdateIssue: %v", d.queries)
	}
	if s.UpdatedAt == "2026-01-01T00:00:00Z" {
		t.Errorf("有变更刷新应 bump UpdatedAt: %s", s.UpdatedAt)
	}
}

// TestRefreshIssueClosedReopensFirst 远端已关闭 → Reopen 先于 Update + Reopened++。
func TestRefreshIssueClosedReopensFirst(t *testing.T) {
	env, s := refreshEnv(t, nil)
	d := &multiRespDoer{t: t, resps: []string{
		repoIssueResp(t, 5, "[测试] v0.9.0", "旧内容", "CLOSED"),
		`{"data":{"reopenIssue":{"issue":{"id":"I_5","number":5,"state":"OPEN"}}}}`,
		`{"data":{"updateIssue":{"issue":{"id":"I_5","number":5,"state":"OPEN"}}}}`,
	}}
	env.GHClient = newTestGHClient(t, d)

	var res Result
	if err := refreshIssue(context.Background(), env, s, &res); err != nil {
		t.Fatalf("refreshIssue: %v", err)
	}
	if res.Reopened != 1 || res.Updated != 1 {
		t.Errorf("Reopened=%d Updated=%d, want 1/1", res.Reopened, res.Updated)
	}
	if !strings.Contains(d.queries[1], "reopenIssue(input:") || !strings.Contains(d.queries[2], "updateIssue(input:") {
		t.Errorf("顺序应为 Get→Reopen→Update: %v", d.queries)
	}
}

// TestReconcileRefreshBranch 状态正确 Issue 的脚本走判等刷新；失败 → Errors++ 且不计 NoIssue。
func TestReconcileRefreshBranch(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		`{"errors":[{"message":"boom"}]}`, // GetIssue 失败
	}}
	env, _ := refreshEnv(t, d)
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{*refreshScript()}}
	res := reconcileIssues(context.Background(), env, r)
	if res.Errors != 1 || res.Active != 1 {
		t.Errorf("Errors=%d Active=%d, want 1/1", res.Errors, res.Active)
	}
	if res.NoIssue != 0 || res.Created != 0 {
		t.Errorf("NoIssue=%d Created=%d, want 0/0（有 Issue 不应回填）", res.NoIssue, res.Created)
	}
}
