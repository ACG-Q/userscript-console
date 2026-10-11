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

	"github.com/acg-q/userscript-console/internal/cleanup"
	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/layout"
	"github.com/acg-q/userscript-console/internal/projector"
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
	if _, err := saveReg(env, reg); err != nil {
		t.Fatalf("saveReg: %v", err)
	}

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
	if _, err := saveReg(env, reg); err != nil {
		t.Fatalf("saveReg: %v", err)
	}

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
	arch := &cleanup.Archive{
		Schema: 1,
		Commands: []cleanup.CommandKey{
			{
				Command: "add", Author: "u1", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				Results: []cleanup.Result{{ID: "r1", Author: "u1", Body: "/add url", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}},
			},
		},
	}
	if err := cleanup.Save(path, arch); err != nil {
		t.Fatalf("saveArchive 失败: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取归档失败: %v", err)
	}
	var decoded cleanup.Archive
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
	arch, err := cleanup.Load(path)
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
	_, err := cleanup.Load(filepath.Join(t.TempDir(), "nope.json"))
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
	_, err := cleanup.Load(path)
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
	_ = os.MkdirAll(distDir, 0o755)
	_ = os.WriteFile(filepath.Join(distDir, "self01.user.js"), []byte("// test"), 0o644)
	_ = os.WriteFile(filepath.Join(distDir, "self02.user.js"), []byte("// test2"), 0o644)
	_ = os.MkdirAll(filepath.Join(distDir, "subdir"), 0o755)

	files, err := listDist(root, layout.Defaults())
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
var (
	_ = github.Issue{}
	_ = fmt.Errorf // 防止 unused import
)

// ── routeDoer 路由式 fake（任务 6：判等刷新 + 发帖集成） ────────

// routeRule 路由规则：qContains 匹配查询文档子串，varNumber>0 时校验 variables.number。
type routeRule struct {
	qContains string
	varNumber int
	resp      string
}

// routeDoer 按查询内容路由响应（project 集成测试需要 REPO_QUERY 被 GetIssue/repoID 双用）。
type routeDoer struct {
	t     *testing.T
	rules []routeRule
}

func (d *routeDoer) Do(req *http.Request) (*http.Response, error) {
	b, _ := io.ReadAll(req.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	q, _ := m["query"].(string)
	vars, _ := m["variables"].(map[string]any)
	for _, r := range d.rules {
		if !strings.Contains(q, r.qContains) {
			continue
		}
		if r.varNumber > 0 {
			n, _ := vars["number"].(float64)
			if int(n) != r.varNumber {
				continue
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(r.resp))}, nil
	}
	d.t.Errorf("routeDoer 未匹配: query=%q vars=%v", q, vars)
	return nil, fmt.Errorf("routeDoer 未匹配")
}

// getIssueRule 匹配 REPO_QUERY issue 查询（按 variables.number 区分脚本）。
func getIssueRule(n int, resp string) routeRule {
	return routeRule{qContains: "issue(number: $number)", varNumber: n, resp: resp}
}

// issueCombinedResp 构造 REPO_QUERY combined 响应。
func issueCombinedResp(t *testing.T, number int, title, body, state string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{
		"id": "R_1", "nameWithOwner": "o/r",
		"issue": map[string]any{
			"id": "I_x", "number": number, "title": title, "body": body, "state": state,
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

// TestRunProjectRefreshReopenPublish self01 已关闭+内容过期 → Reopen+Update；
// 发帖账本空 → 发帖并记账，Changed=true。
func TestRunProjectRefreshReopenPublish(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	env.RepoOwner = "o"
	env.RepoName = "o/r"
	env.PagesBase = "https://test.github.io/repo"

	reg, _ := loadReg(env)
	var rules []routeRule
	rules = append(rules, getIssueRule(5, issueCombinedResp(t, 5, "[测试脚本] v0.0.1", "过期内容", "CLOSED")))
	num := 6
	for i := range reg.Scripts {
		s := &reg.Scripts[i]
		if s.Deleted {
			continue
		}
		if s.ID == "self01" {
			s.Issue = &registry.IssueRef{Number: 5, NodeID: "I_5", URL: "https://github.com/o/r/issues/5"}
			s.Discussions = nil
			continue
		}
		s.Issue = &registry.IssueRef{Number: num, NodeID: fmt.Sprintf("I_%d", num), URL: fmt.Sprintf("https://github.com/o/r/issues/%d", num)}
		s.Discussions = []registry.DiscussionEntry{{Version: s.Version, Number: 1, NodeID: "D_1"}}
		title := fmt.Sprintf("[%s] v%s", s.Name, s.Version)
		rules = append(rules, getIssueRule(num, issueCombinedResp(t, num, title, projector.BuildIssueBody(s, env.PagesBase, env.layout().DistSeg()), "OPEN")))
		num++
	}
	if _, err := saveReg(env, reg); err != nil {
		t.Fatalf("saveReg: %v", err)
	}

	rules = append(rules,
		routeRule{qContains: "reopenIssue(input:", resp: `{"data":{"reopenIssue":{"issue":{"id":"I_5","number":5,"state":"OPEN"}}}}`},
		routeRule{qContains: "updateIssue(input:", resp: `{"data":{"updateIssue":{"issue":{"id":"I_5","number":5,"state":"OPEN"}}}}`},
		routeRule{qContains: "discussionCategories", resp: `{"data":{"repository":{"discussionCategories":{"nodes":[{"id":"DC_1","name":"Q&A","slug":"q-a"}]}}}}`},
		routeRule{qContains: "issue(number: $number)", varNumber: 1, resp: `{"data":{"repository":{"id":"R_1","nameWithOwner":"o/r","issue":null}}}`},
		routeRule{qContains: "createDiscussion(input:", resp: `{"data":{"createDiscussion":{"discussion":{"id":"D_9","number":9,"title":"T","body":"B","url":"https://github.com/o/r/discussions/9","createdAt":"2026-10-10T00:00:00Z"}}}}`},
	)
	ghc, err := github.New("tok", "o/r", github.WithDoer(&routeDoer{t: t, rules: rules}), github.WithRetry(0))
	if err != nil {
		t.Fatalf("GHClient: %v", err)
	}
	env.GHClient = ghc

	res, err := Execute("project", env, "", nil)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if !res.Changed {
		t.Error("有更新+重开+发帖时 Changed 应为 true")
	}
	for _, want := range []string{"更新 Issue: 1", "重开 Issue: 1", "发版本帖: 1", "错误: 0"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("回帖缺少 %q:\n%s", want, res.Text)
		}
	}
	reg2, _ := loadReg(env)
	for _, s := range reg2.Scripts {
		if s.ID == "self01" {
			if len(s.Discussions) != 1 || s.Discussions[0].Number != 9 {
				t.Errorf("self01 发牌应追加 9 号帖: %+v", s.Discussions)
			}
		}
	}
}

// TestRunProjectIdempotentNoChange 判等全相等+发牌均当前 → 零写、Changed=false、显示跳过数。
func TestRunProjectIdempotentNoChange(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	env.RepoOwner = "o"
	env.RepoName = "o/r"
	env.PagesBase = "https://test.github.io/repo"

	reg, _ := loadReg(env)
	var rules []routeRule
	num := 5
	for i := range reg.Scripts {
		s := &reg.Scripts[i]
		if s.Deleted {
			continue
		}
		s.Issue = &registry.IssueRef{Number: num, NodeID: fmt.Sprintf("I_%d", num), URL: fmt.Sprintf("https://github.com/o/r/issues/%d", num)}
		s.Discussions = []registry.DiscussionEntry{{Version: s.Version, Number: 1, NodeID: "D_1"}}
		title := fmt.Sprintf("[%s] v%s", s.Name, s.Version)
		body := projector.BuildIssueBody(s, env.PagesBase, env.layout().DistSeg())
		rules = append(rules, getIssueRule(num, issueCombinedResp(t, num, title, body, "OPEN")))
		num++
	}
	if _, err := saveReg(env, reg); err != nil {
		t.Fatalf("saveReg: %v", err)
	}

	ghc, err := github.New("tok", "o/r", github.WithDoer(&routeDoer{t: t, rules: rules}), github.WithRetry(0))
	if err != nil {
		t.Fatalf("GHClient: %v", err)
	}
	env.GHClient = ghc

	res, err := Execute("project", env, "", nil)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if res.Changed {
		t.Error("全相等+发牌均当前时 Changed 应为 false")
	}
	if !strings.Contains(res.Text, "更新 Issue: 0") || !strings.Contains(res.Text, "发版本帖: 0") {
		t.Errorf("统计计数应全 0:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "跳过: ") {
		t.Errorf("跳过数>0 时应展示发牌跳过数:\n%s", res.Text)
	}
}
