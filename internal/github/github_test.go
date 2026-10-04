package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ── fake Doer：按调用序回放，记录请求结构 ─────────────────────

type reqRecord struct {
	Method string
	Auth   string
	CType  string
	Query  string
	Vars   map[string]any
}

type resp struct {
	status int
	body   string
	err    error
}

type fakeDoer struct {
	t     *testing.T
	resps []resp
	calls int
	recs  []reqRecord
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	b, _ := io.ReadAll(req.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	q, _ := m["query"].(string)
	vars, _ := m["variables"].(map[string]any)
	f.recs = append(f.recs, reqRecord{req.Method, req.Header.Get("Authorization"), req.Header.Get("Content-Type"), q, vars})

	idx := f.calls
	f.calls++
	if len(f.resps) == 0 {
		f.t.Errorf("第 %d 次调用无预置响应", idx)
		return nil, errors.New("无预置响应")
	}
	if idx >= len(f.resps) {
		idx = len(f.resps) - 1 // 序列用尽 → 重复回放最后一条（重试耗尽类用例）
	}
	r := f.resps[idx]
	if r.err != nil {
		return nil, r.err
	}
	return &http.Response{StatusCode: r.status, Body: io.NopCloser(strings.NewReader(r.body))}, nil
}

func okBody(t *testing.T, data any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func errBody(msgs ...string) string {
	es := make([]map[string]string, 0, len(msgs))
	for _, m := range msgs {
		es = append(es, map[string]string{"message": m})
	}
	b, _ := json.Marshal(map[string]any{"data": nil, "errors": es})
	return string(b)
}

func newTestClient(t *testing.T, d Doer, opts ...Option) *Client {
	t.Helper()
	c, err := New("tok123", "acg-q/userscript-manager", append([]Option{WithDoer(d), WithRetry(2), func(c *Client) { c.backoff = time.Millisecond }}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func gqlIssueJSON(id string, number int) map[string]any {
	return map[string]any{
		"id": id, "number": number, "title": "T", "body": "B", "state": "OPEN",
		"createdAt": "2026-10-01T00:00:00Z", "updatedAt": "2026-10-02T00:00:00Z",
		"author":   map[string]any{"login": "acg-q"},
		"comments": map[string]any{"totalCount": 3},
	}
}

// ── Client 构造与 Raw ───────────────────────────────────────

func TestNew参数校验(t *testing.T) {
	if _, err := New("", "a/b"); err == nil {
		t.Error("空 token 应报错")
	}
	if _, err := New("t", "noslash"); err == nil {
		t.Error("缺 / 应报错")
	}
	if _, err := New("t", "/b"); err == nil {
		t.Error("空 owner 应报错")
	}
	c, err := New("t", "o/r", WithRetry(-5))
	if err != nil {
		t.Fatal(err)
	}
	if c.retry != 0 {
		t.Errorf("负数 retry 应钳 0，实际 %d", c.retry)
	}
}

func TestRaw请求结构与owner注入(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"repository": map[string]any{"id": "R_1"}}), nil}}}
	c := newTestClient(t, d)
	var out map[string]any
	if err := c.Raw(context.Background(), REPO_QUERY, map[string]any{"number": 7}, &out); err != nil {
		t.Fatal(err)
	}
	rec := d.recs[0]
	if rec.Method != "POST" {
		t.Errorf("method = %s", rec.Method)
	}
	if rec.Auth != "Bearer tok123" {
		t.Errorf("auth = %q", rec.Auth)
	}
	if !strings.Contains(rec.CType, "application/json") {
		t.Errorf("content-type = %q", rec.CType)
	}
	if rec.Vars["owner"] != "acg-q" || rec.Vars["name"] != "userscript-manager" {
		t.Errorf("owner/name 未注入: %#v", rec.Vars)
	}
	if rec.Vars["number"] != float64(7) {
		t.Errorf("number = %#v", rec.Vars["number"])
	}
}

func TestRaw标签变量不被注入误伤(t *testing.T) {
	// $labelName 不应触发 $name 注入（词边界）
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"createLabel": map[string]any{"label": map[string]any{"id": "L_1", "name": "bug"}}}), nil}}}
	c := newTestClient(t, d)
	if err := c.Raw(context.Background(), CREATE_LABEL_MUTATION, map[string]any{"labelName": "bug"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, injected := d.recs[0].Vars["name"]; injected {
		t.Errorf("$labelName 误触发 $name 注入: %#v", d.recs[0].Vars)
	}
	if d.recs[0].Vars["labelName"] != "bug" {
		t.Errorf("labelName 丢失: %#v", d.recs[0].Vars)
	}
}

func TestRaw重试矩阵(t *testing.T) {
	t.Run("500后成功", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{
			{500, "oops", nil},
			{200, okBody(t, map[string]any{"x": 1}), nil},
		}}
		c := newTestClient(t, d)
		if err := c.Raw(context.Background(), "query { viewer { id } }", nil, nil); err != nil {
			t.Fatalf("应重试成功: %v", err)
		}
		if d.calls != 2 {
			t.Errorf("calls = %d", d.calls)
		}
	})
	t.Run("429与502可重试", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{
			{429, "slow down", nil},
			{502, "bad gateway", nil},
			{200, "{}", nil},
		}}
		c := newTestClient(t, d)
		if err := c.Raw(context.Background(), "query { viewer { id } }", nil, nil); err != nil {
			t.Fatalf("应重试成功: %v", err)
		}
		if d.calls != 3 {
			t.Errorf("calls = %d", d.calls)
		}
	})
	t.Run("404不重试", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{404, "not found", nil}}}
		c := newTestClient(t, d)
		err := c.Raw(context.Background(), "query { viewer { id } }", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
			t.Fatalf("err = %v", err)
		}
		if d.calls != 1 {
			t.Errorf("4xx 不应重试, calls = %d", d.calls)
		}
	})
	t.Run("重试耗尽", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{500, "x", nil}}}
		c := newTestClient(t, d)
		if err := c.Raw(context.Background(), "query { viewer { id } }", nil, nil); err == nil {
			t.Fatal("应失败")
		}
		if d.calls != 3 { // 1 + retry(2)
			t.Errorf("calls = %d, want 3", d.calls)
		}
	})
	t.Run("传输错误可重试", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{
			{0, "", errors.New("连接被重置")},
			{200, "{}", nil},
		}}
		c := newTestClient(t, d)
		if err := c.Raw(context.Background(), "query { viewer { id } }", nil, nil); err != nil {
			t.Fatalf("传输错误应重试: %v", err)
		}
		if d.calls != 2 {
			t.Errorf("calls = %d", d.calls)
		}
	})
}

func TestRaw次级限速独立只重试一次(t *testing.T) {
	// retry=0 时普通预算只有 1 次，但限速路径独立 → 仍应重试一次（calls==2）
	d := &fakeDoer{t: t, resps: []resp{
		{200, errBody("You have exceeded a secondary rate limit"), nil},
		{200, "{}", nil},
	}}
	c := newTestClient(t, d, WithRetry(0))
	if err := c.Raw(context.Background(), "query { viewer { id } }", nil, nil); err != nil {
		t.Fatalf("限速后应重试成功: %v", err)
	}
	if d.calls != 2 {
		t.Errorf("calls = %d, want 2（限速只重试一次）", d.calls)
	}

	// 限速也只重试一次：第二次仍限速 → 失败
	d2 := &fakeDoer{t: t, resps: []resp{
		{403, "API rate limit exceeded", nil},
		{403, "API rate limit exceeded", nil},
	}}
	c2 := newTestClient(t, d2)
	if err := c2.Raw(context.Background(), "query { viewer { id } }", nil, nil); err == nil {
		t.Fatal("连续限速应失败")
	}
	if d2.calls != 2 {
		t.Errorf("calls = %d, want 2", d2.calls)
	}
}

func TestRaw图错误聚合(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, errBody("坏字段A", "坏字段B"), nil}}}
	c := newTestClient(t, d)
	err := c.Raw(context.Background(), "query { viewer { id } }", nil, nil)
	var gerr *GraphQLError
	if !errors.As(err, &gerr) {
		t.Fatalf("应为 GraphQLError: %v", err)
	}
	if len(gerr.Messages) != 2 {
		t.Errorf("messages = %v", gerr.Messages)
	}
	if d.calls != 1 {
		t.Errorf("普通图错误不应重试, calls = %d", d.calls)
	}
}

func TestRaw预取消零调用(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{}}
	c := newTestClient(t, d)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Raw(ctx, "query { viewer { id } }", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if d.calls != 0 {
		t.Errorf("不应发起请求, calls = %d", d.calls)
	}
}

// ── 查询文档回归锁 ──────────────────────────────────────────

func Test固定查询文档回归锁(t *testing.T) {
	consts := map[string]string{
		"REPO_QUERY":                  REPO_QUERY,
		"LABELS_QUERY":                LABELS_QUERY,
		"LIST_QUERY":                  LIST_QUERY,
		"CREATE_LABEL_MUTATION":       CREATE_LABEL_MUTATION,
		"CREATE_MUTATION":             CREATE_MUTATION,
		"UPDATE_MUTATION":             UPDATE_MUTATION,
		"CLOSE_MUTATION":              CLOSE_MUTATION,
		"REOPEN_MUTATION":             REOPEN_MUTATION,
		"DISCUSSION_CATEGORIES_QUERY": DISCUSSION_CATEGORIES_QUERY,
		"CREATE_DISCUSSION_MUTATION":  CREATE_DISCUSSION_MUTATION,
		"DISCUSSION_NODE_QUERY":       DISCUSSION_NODE_QUERY,
		"PANEL_QUERY":                 PANEL_QUERY,
		"DELETE_COMMENT_MUTATION":     DELETE_COMMENT_MUTATION,
	}
	for name, doc := range consts {
		if strings.TrimSpace(doc) == "" {
			t.Errorf("%s 为空", name)
		}
	}
	must := map[string][]string{
		"LIST_QUERY":              {"totalCount", "updatedAt", "pageInfo", "hasNextPage", "orderBy"},
		"PANEL_QUERY":             {"comments(first:", "after: $cursor"},
		"CREATE_MUTATION":         {"createIssue(input:"},
		"UPDATE_MUTATION":         {"updateIssue(input:"},
		"CLOSE_MUTATION":          {"closeIssue(input:"},
		"REOPEN_MUTATION":         {"reopenIssue(input:"},
		"DELETE_COMMENT_MUTATION": {"deleteComment(input:"},
	}
	for name, subs := range must {
		for _, s := range subs {
			if !strings.Contains(consts[name], s) {
				t.Errorf("%s 缺 %q", name, s)
			}
		}
	}
}

func Test讨论节点查询必须走node入口(t *testing.T) {
	if !strings.Contains(DISCUSSION_NODE_QUERY, "node(id: $id)") {
		t.Error("必须用 node(id:) 入口")
	}
	if strings.Contains(DISCUSSION_NODE_QUERY, "discussion(id:") {
		t.Error("禁止 discussion(id:)（线上事故回归锁）")
	}
	if !strings.Contains(DISCUSSION_NODE_QUERY, "... on Discussion") {
		t.Error("缺类型片段")
	}
}

func TestBuildStatsQuery结构与非法n(t *testing.T) {
	q, err := BuildStatsQuery(3)
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"i0:", "i1:", "i2:"} {
		if !strings.Contains(q, alias) {
			t.Errorf("缺别名 %s", alias)
		}
	}
	if strings.Count(q, "node(id: $id") != 3 {
		t.Errorf("node 数量错误:\n%s", q)
	}
	if !strings.Contains(q, "... on Issue") || !strings.Contains(q, "... on Discussion") {
		t.Error("缺类型片段")
	}
	if _, err := BuildStatsQuery(0); err == nil {
		t.Error("n=0 应报错")
	}
}

// ── typed helpers ───────────────────────────────────────────

func TestGetIssue解码与null(t *testing.T) {
	t.Run("成功", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
			"repository": map[string]any{"issue": gqlIssueJSON("I_1", 5)},
		}), nil}}}
		c := newTestClient(t, d)
		iss, err := c.GetIssue(context.Background(), 5)
		if err != nil {
			t.Fatal(err)
		}
		if iss.Number != 5 || iss.NodeID != "I_1" || iss.Author != "acg-q" || iss.Comments != 3 {
			t.Errorf("解码错误: %+v", iss)
		}
		if d.recs[0].Vars["number"] != float64(5) {
			t.Errorf("variables: %#v", d.recs[0].Vars)
		}
	})
	t.Run("null报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
			"repository": map[string]any{"issue": nil},
		}), nil}}}
		c := newTestClient(t, d)
		if _, err := c.GetIssue(context.Background(), 9); err == nil || !strings.Contains(err.Error(), "不存在") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestListRepoIssues分页合并(t *testing.T) {
	page1 := map[string]any{"repository": map[string]any{"issues": map[string]any{
		"totalCount": 2,
		"pageInfo":   map[string]any{"hasNextPage": true, "endCursor": "CUR1"},
		"nodes":      []any{gqlIssueJSON("I_1", 1)},
	}}}
	page2 := map[string]any{"repository": map[string]any{"issues": map[string]any{
		"totalCount": 2,
		"pageInfo":   map[string]any{"hasNextPage": false, "endCursor": ""},
		"nodes":      []any{gqlIssueJSON("I_2", 2)},
	}}}
	d := &fakeDoer{t: t, resps: []resp{
		{200, okBody(t, page1), nil},
		{200, okBody(t, page2), nil},
	}}
	c := newTestClient(t, d)
	issues, err := c.ListRepoIssues(context.Background(), "open")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 || issues[1].NodeID != "I_2" {
		t.Errorf("分页合并错误: %+v", issues)
	}
	// 第二页 cursor 应为首页 endCursor；states 应大写数组
	if d.recs[1].Vars["cursor"] != "CUR1" {
		t.Errorf("第二页 cursor = %#v", d.recs[1].Vars["cursor"])
	}
	st, _ := d.recs[0].Vars["states"].([]any)
	if len(st) != 1 || st[0] != "OPEN" {
		t.Errorf("states = %#v", d.recs[0].Vars["states"])
	}
	// ALL → null
	d2 := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"repository": map[string]any{"issues": map[string]any{
		"totalCount": 0, "pageInfo": map[string]any{"hasNextPage": false}, "nodes": []any{},
	}}}), nil}}}
	c2 := newTestClient(t, d2)
	if _, err := c2.ListRepoIssues(context.Background(), "ALL"); err != nil {
		t.Fatal(err)
	}
	if v, ok := d2.recs[0].Vars["states"]; ok && v != nil {
		t.Errorf("ALL 应为 null，实际 %#v", v)
	}
}

func TestCreateIssue先取repositoryId并缓存(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{
		{200, okBody(t, map[string]any{"repository": map[string]any{"id": "R_42"}}), nil},
		{200, okBody(t, map[string]any{"createIssue": map[string]any{"issue": gqlIssueJSON("I_9", 9)}}), nil},
	}}
	c := newTestClient(t, d)
	iss, err := c.CreateIssue(context.Background(), "标题", "正文")
	if err != nil {
		t.Fatal(err)
	}
	if iss.Number != 9 {
		t.Errorf("解码: %+v", iss)
	}
	if d.recs[1].Vars["repositoryId"] != "R_42" || d.recs[1].Vars["title"] != "标题" {
		t.Errorf("variables: %#v", d.recs[1].Vars)
	}
	// 第二次 helper 调用应命中缓存（不再发 REPO_QUERY）
	d.resps = append(d.resps, resp{200, okBody(t, map[string]any{"createIssue": map[string]any{"issue": gqlIssueJSON("I_10", 10)}}), nil})
	if _, err := c.CreateIssue(context.Background(), "t2", "b2"); err != nil {
		t.Fatal(err)
	}
	if d.calls != 3 {
		t.Errorf("repoID 应缓存, calls = %d", d.calls)
	}
}

func Test状态跃迁(t *testing.T) {
	t.Run("close成功", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
			"closeIssue": map[string]any{"issue": map[string]any{"id": "I_1", "number": 1, "state": "CLOSED"}},
		}), nil}}}
		c := newTestClient(t, d)
		if err := c.CloseIssue(context.Background(), "I_1"); err != nil {
			t.Fatal(err)
		}
		if d.recs[0].Vars["issueId"] != "I_1" {
			t.Errorf("variables: %#v", d.recs[0].Vars)
		}
	})
	t.Run("状态不符报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
			"reopenIssue": map[string]any{"issue": map[string]any{"id": "I_1", "number": 1, "state": "CLOSED"}},
		}), nil}}}
		c := newTestClient(t, d)
		if err := c.ReopenIssue(context.Background(), "I_1"); err == nil {
			t.Fatal("期望状态不符错误")
		}
	})
}

func TestCreateLabel已存在视为成功(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{
		{200, okBody(t, map[string]any{"repository": map[string]any{"id": "R_1"}}), nil},
		{200, errBody("Label already exists for this repository"), nil},
	}}
	c := newTestClient(t, d)
	if err := c.CreateLabel(context.Background(), "bug", "d73a49", "缺陷"); err != nil {
		t.Fatalf("已存在应 nil: %v", err)
	}
}

func TestCreateLabelNullReturn(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{
		{200, okBody(t, map[string]any{"repository": map[string]any{"id": "R_1"}}), nil},
		{200, okBody(t, map[string]any{"createLabel": map[string]any{"label": nil}}), nil},
	}}
	c := newTestClient(t, d)
	if err := c.CreateLabel(context.Background(), "feat", "4a9d2b", "功能"); err == nil {
		t.Fatal("CreateLabel null 返回应报错")
	}
}

func TestListIssueComments分页与null(t *testing.T) {
	comment := map[string]any{"id": "IC_1", "author": map[string]any{"login": "u"}, "body": "/list", "createdAt": "2026-10-01T00:00:00Z"}
	page := func(hasNext bool, cur string) map[string]any {
		return map[string]any{"repository": map[string]any{"issue": map[string]any{"comments": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": hasNext, "endCursor": cur},
			"nodes":    []any{comment},
		}}}}
	}
	d := &fakeDoer{t: t, resps: []resp{
		{200, okBody(t, page(true, "C1")), nil},
		{200, okBody(t, page(false, "")), nil},
	}}
	c := newTestClient(t, d)
	comments, err := c.ListIssueComments(context.Background(), 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 || comments[0].Author != "u" {
		t.Errorf("分页合并: %+v", comments)
	}
	if d.recs[1].Vars["cursor"] != "C1" {
		t.Errorf("cursor: %#v", d.recs[1].Vars)
	}

	d2 := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
		"repository": map[string]any{"issue": nil},
	}), nil}}}
	c2 := newTestClient(t, d2)
	if _, err := c2.ListIssueComments(context.Background(), 404, 100); err == nil {
		t.Error("issue null 应报错")
	}
}

func TestDeleteComment(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
		"deleteComment": map[string]any{"clientMutationId": nil},
	}), nil}}}
	c := newTestClient(t, d)
	if err := c.DeleteComment(context.Background(), "IC_1"); err != nil {
		t.Fatal(err)
	}
	if d.recs[0].Vars["commentId"] != "IC_1" {
		t.Errorf("variables: %#v", d.recs[0].Vars)
	}
}

func TestDiscussion系列(t *testing.T) {
	t.Run("分类列表", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
			"repository": map[string]any{"discussionCategories": map[string]any{
				"nodes": []any{map[string]any{"id": "DC_1", "name": "版本", "slug": "releases"}},
			}},
		}), nil}}}
		c := newTestClient(t, d)
		cats, err := c.DiscussionCategories(context.Background())
		if err != nil || len(cats) != 1 || cats[0].NodeID != "DC_1" {
			t.Fatalf("cats=%v err=%v", cats, err)
		}
	})
	t.Run("发布版本帖", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{
			{200, okBody(t, map[string]any{"repository": map[string]any{"id": "R_1"}}), nil},
			{200, okBody(t, map[string]any{"createDiscussion": map[string]any{"discussion": map[string]any{
				"id": "D_1", "number": 10, "title": "t", "body": "b",
				"url": "https://github.com/o/r/discussions/10", "createdAt": "2026-10-01T00:00:00Z",
			}}}), nil},
		}}
		c := newTestClient(t, d)
		disc, err := c.CreateDiscussion(context.Background(), "DC_1", "标题", "正文")
		if err != nil || disc.Number != 10 {
			t.Fatalf("disc=%+v err=%v", disc, err)
		}
	})
	t.Run("null 返回报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{
			{200, okBody(t, map[string]any{"repository": map[string]any{"id": "R_1"}}), nil},
			{200, okBody(t, map[string]any{"createDiscussion": map[string]any{"discussion": nil}}), nil},
		}}
		c := newTestClient(t, d)
		if _, err := c.CreateDiscussion(context.Background(), "DC_1", "t", "b"); err == nil {
			t.Fatal("CreateDiscussion null 返回应报错")
		}
	})
	t.Run("node入口取版本帖", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
			"node": map[string]any{
				"__typename": "Discussion", "id": "D_1", "number": 10,
				"title": "t", "body": "b", "url": "u", "createdAt": "c",
				"category": map[string]any{"id": "DC_1", "name": "版本", "slug": "releases"},
				"comments": map[string]any{"pageInfo": map[string]any{"hasNextPage": false}, "nodes": []any{}},
			},
		}), nil}}}
		c := newTestClient(t, d)
		disc, err := c.DiscussionByNode(context.Background(), "D_1")
		if err != nil || disc.CategoryID != "DC_1" {
			t.Fatalf("disc=%+v err=%v", disc, err)
		}
		if !strings.Contains(d.recs[0].Query, "node(id:") {
			t.Error("必须走 node(id:)")
		}
	})
	t.Run("非Discussion类型报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
			"node": map[string]any{"__typename": "Issue", "id": "I_1"},
		}), nil}}}
		c := newTestClient(t, d)
		if _, err := c.DiscussionByNode(context.Background(), "I_1"); err == nil {
			t.Error("typename 不符应报错")
		}
	})
	t.Run("null节点报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"node": nil}), nil}}}
		c := newTestClient(t, d)
		if _, err := c.DiscussionByNode(context.Background(), "GONE"); err == nil {
			t.Error("null 应报错")
		}
	})
	t.Run("评论分页", func(t *testing.T) {
		page := func(hasNext bool, cur string) map[string]any {
			return map[string]any{"node": map[string]any{
				"__typename": "Discussion", "id": "D_1",
				"comments": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": hasNext, "endCursor": cur},
					"nodes":    []any{map[string]any{"id": "DCM_1", "author": map[string]any{"login": "u"}, "body": "x", "createdAt": "c"}},
				},
			}}
		}
		d := &fakeDoer{t: t, resps: []resp{
			{200, okBody(t, page(true, "K1")), nil},
			{200, okBody(t, page(false, "")), nil},
		}}
		c := newTestClient(t, d)
		comments, err := c.DiscussionComments(context.Background(), "D_1")
		if err != nil || len(comments) != 2 {
			t.Fatalf("comments=%v err=%v", comments, err)
		}
		if d.recs[1].Vars["cursor"] != "K1" {
			t.Errorf("cursor: %#v", d.recs[1].Vars)
		}
	})
}

func TestIssueStats解码与跳过null(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
		"i0": map[string]any{"id": "I_1", "comments": map[string]any{"totalCount": 7}, "createdAt": "c1"},
		"i1": nil,
	}), nil}}}
	c := newTestClient(t, d)
	stats, err := c.IssueStats(context.Background(), []string{"I_1", "GONE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].Comments != 7 {
		t.Errorf("stats = %+v", stats)
	}
	if d.recs[0].Vars["id0"] != "I_1" || d.recs[0].Vars["id1"] != "GONE" {
		t.Errorf("variables: %#v", d.recs[0].Vars)
	}
	if empty, err := c.IssueStats(context.Background(), nil); err != nil || empty != nil {
		t.Errorf("空输入应 nil, %v %v", empty, err)
	}
}

// ── WithEndpoint ──────────────────────────────────────────────

func TestWithEndpoint(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
		"repository": map[string]any{"id": "R_1"},
	}), nil}}}
	c := newTestClient(t, d, WithEndpoint("https://custom.github.example.com/graphql"))
	var out map[string]any
	if err := c.Raw(context.Background(), REPO_QUERY, map[string]any{"number": 1}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.recs[0].Query, "repository") {
		t.Errorf("query 应含 repository: %q", d.recs[0].Query)
	}
}

// ── UpdateIssue ───────────────────────────────────────────────

func TestUpdateIssue(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
		"updateIssue": map[string]any{"issue": gqlIssueJSON("I_1", 1)},
	}), nil}}}
	c := newTestClient(t, d)
	if err := c.UpdateIssue(context.Background(), "I_1", "新标题", "新正文"); err != nil {
		t.Fatalf("UpdateIssue 失败: %v", err)
	}
	if d.recs[0].Vars["issueId"] != "I_1" {
		t.Errorf("variables: %#v", d.recs[0].Vars)
	}
	if d.recs[0].Vars["title"] != "新标题" {
		t.Errorf("title variable = %v, want 新标题", d.recs[0].Vars["title"])
	}
}

func TestUpdateIssueNullReturn(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
		"updateIssue": map[string]any{"issue": nil},
	}), nil}}}
	c := newTestClient(t, d)
	if err := c.UpdateIssue(context.Background(), "I_1", "t", "b"); err == nil {
		t.Fatal("UpdateIssue null 返回应报错")
	}
}

func TestUpdateIssueRawError(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, errBody("Something went wrong"), nil}}}
	c := newTestClient(t, d)
	if err := c.UpdateIssue(context.Background(), "I_1", "t", "b"); err == nil {
		t.Fatal("UpdateIssue GraphQL error 应返回 error")
	}
}

func TestCreateIssueNullReturn(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{
		{200, okBody(t, map[string]any{"repository": map[string]any{"id": "R_1"}}), nil},
		{200, okBody(t, map[string]any{"createIssue": map[string]any{"issue": nil}}), nil},
	}}
	c := newTestClient(t, d)
	if _, err := c.CreateIssue(context.Background(), "t", "b"); err == nil {
		t.Fatal("CreateIssue null 返回应报错")
	}
}

func TestGetIssueNullReturn(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
		"repository": map[string]any{"issue": nil},
	}), nil}}}
	c := newTestClient(t, d)
	if _, err := c.GetIssue(context.Background(), 99); err == nil {
		t.Fatal("GetIssue null 返回应报错")
	}
}

func TestDiscussionCommentsEndCursorEmpty(t *testing.T) {
	node := map[string]any{
		"id": "D_1",
		"comments": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": true, "endCursor": ""},
			"nodes":    []any{},
		},
	}
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"node": node}), nil}}}
	c := newTestClient(t, d)
	if _, err := c.DiscussionComments(context.Background(), "D_1"); err == nil {
		t.Fatal("DiscussionComments endCursor 空应报错")
	}
}

func TestListIssueCommentsNullIssue(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
		"repository": map[string]any{"issue": nil},
	}), nil}}}
	c := newTestClient(t, d)
	if _, err := c.ListIssueComments(context.Background(), 5, 100); err == nil {
		t.Fatal("ListIssueComments null issue 应报错")
	}
}

func TestTransitionWrongState(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
		"closeIssue": map[string]any{"issue": map[string]any{"id": "I_1", "number": 1, "state": "OPEN"}},
	}), nil}}}
	c := newTestClient(t, d)
	if err := c.CloseIssue(context.Background(), "I_1"); err == nil {
		t.Fatal("CloseIssue 状态不符应报错")
	}
}

func TestCreateLabelNetworkError(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{
		{200, okBody(t, map[string]any{"repository": map[string]any{"id": "R_1"}}), nil},
		{200, errBody("GraphQL error"), nil},
	}}
	c := newTestClient(t, d)
	if err := c.CreateLabel(context.Background(), "bug", "ff0000", "缺陷"); err == nil {
		t.Fatal("CreateLabel GraphQL 错误应返回 error")
	}
}

func TestDeleteCommentSuccess(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
		"deleteComment": map[string]any{"clientMutationId": "x"},
	}), nil}}}
	c := newTestClient(t, d)
	if err := c.DeleteComment(context.Background(), "IC_1"); err != nil {
		t.Fatalf("DeleteComment 应成功: %v", err)
	}
}

func TestDiscussionCategoriesEmpty(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
		"repository": map[string]any{"discussionCategories": map[string]any{"nodes": []any{}}},
	}), nil}}}
	c := newTestClient(t, d)
	if _, err := c.DiscussionCategories(context.Background()); err == nil {
		t.Fatal("DiscussionCategories 空分类应报错")
	}
}

func TestGetIssueSuccess(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
		"repository": map[string]any{"issue": gqlIssueJSON("I_42", 42)},
	}), nil}}}
	c := newTestClient(t, d)
	iss, err := c.GetIssue(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetIssue 应成功: %v", err)
	}
	if iss.Number != 42 {
		t.Errorf("Number = %d, want 42", iss.Number)
	}
}

func TestCreateIssueSuccess(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{
		{200, okBody(t, map[string]any{"repository": map[string]any{"id": "R_1"}}), nil},
		{200, okBody(t, map[string]any{"createIssue": map[string]any{"issue": gqlIssueJSON("I_5", 5)}}), nil},
	}}
	c := newTestClient(t, d)
	iss, err := c.CreateIssue(context.Background(), "标题", "正文")
	if err != nil {
		t.Fatalf("CreateIssue 应成功: %v", err)
	}
	if iss.Number != 5 {
		t.Errorf("Number = %d, want 5", iss.Number)
	}
}

func TestTransitionSuccess(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{
		"closeIssue": map[string]any{"issue": map[string]any{"id": "I_1", "number": 1, "state": "CLOSED"}},
	}), nil}}}
	c := newTestClient(t, d)
	if err := c.CloseIssue(context.Background(), "I_1"); err != nil {
		t.Fatalf("CloseIssue 应成功: %v", err)
	}
}

func TestDeleteCommentRawError(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, errBody("GraphQL error"), nil}}}
	c := newTestClient(t, d)
	if err := c.DeleteComment(context.Background(), "IC_1"); err == nil {
		t.Fatal("DeleteComment GraphQL 错误应返回 error")
	}
}

func TestListIssueCommentsSuccess(t *testing.T) {
	comment := map[string]any{"id": "IC_1", "author": map[string]any{"login": "alice"}, "body": "ok", "createdAt": "2026-01-01T00:00:00Z"}
	page := map[string]any{
		"repository": map[string]any{"issue": map[string]any{"comments": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
			"nodes":    []any{comment},
		}}},
	}
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, page), nil}}}
	c := newTestClient(t, d)
	comments, err := c.ListIssueComments(context.Background(), 7, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].Author != "alice" {
		t.Errorf("结果不符: %+v", comments)
	}
}
