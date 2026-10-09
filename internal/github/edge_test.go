package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ── 边缘分支素材 ────────────────────────────────────────────

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

type badReader struct{}

func (badReader) Read([]byte) (int, error) { return 0, errors.New("读流失败") }

// newCancelDuringSleep 构造一个「sleep 期间被 ctx 取消」的客户端：
// backoff 拉到 5s（远大于 50ms 的取消延迟），确保 doOnce 返回后
// ctx.Err() 仍为 nil、随后 sleep 被 ctx.Done 打断。
func newCancelDuringSleep(t *testing.T, d Doer) (*Client, context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	c := newTestClient(t, d, func(c *Client) { c.backoff = 5 * time.Second })
	return c, ctx, cancel
}

// ── Raw：传输与限速分支 ────────────────────────────────────

func TestRawNilContext(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"viewer": map[string]any{"login": "u"}}), nil}}}
	c := newTestClient(t, d)
	if err := c.Raw(context.TODO(), `query { viewer { login } }`, nil, nil); err != nil {
		t.Fatalf("nil ctx 应退化为 Background: %v", err)
	}
}

func TestRawTransportErrorRetriesExhausted(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{0, "", errors.New("connection refused")}}}
	c := newTestClient(t, d, WithRetry(0))
	if err := c.Raw(context.Background(), `query { viewer { login } }`, nil, nil); err == nil || !strings.Contains(err.Error(), "请求失败") {
		t.Fatalf("err = %v", err)
	}
}

func TestRawTransportErrorContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := doerFunc(func(*http.Request) (*http.Response, error) {
		cancel()
		return nil, errors.New("连接被重置")
	})
	c := newTestClient(t, d)
	if err := c.Raw(ctx, `query { viewer { login } }`, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRawSleepInterruptedOnTransportError(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{0, "", errors.New("connection refused")}}}
	c, ctx, cancel := newCancelDuringSleep(t, d)
	defer cancel()
	if err := c.Raw(ctx, `query { viewer { login } }`, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("退避等待应被 ctx 打断, err = %v", err)
	}
}

func TestRawSleepInterruptedOn5xx(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{500, "server error", nil}}}
	c, ctx, cancel := newCancelDuringSleep(t, d)
	defer cancel()
	if err := c.Raw(ctx, `query { viewer { login } }`, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRawSleepInterruptedOn4xxRateLimit(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{403, "API rate limit exceeded", nil}}}
	c, ctx, cancel := newCancelDuringSleep(t, d)
	defer cancel()
	if err := c.Raw(ctx, `query { viewer { login } }`, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRawSleepInterruptedOn200RateLimit(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, errBody("You have exceeded a secondary rate limit"), nil}}}
	c, ctx, cancel := newCancelDuringSleep(t, d)
	defer cancel()
	if err := c.Raw(ctx, `query { viewer { login } }`, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRawDataDecodeError(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{200, `{"data":"not-an-object"}`, nil}}}
	c := newTestClient(t, d)
	var out struct {
		X int `json:"x"`
	}
	if err := c.Raw(context.Background(), `query { viewer { login } }`, nil, &out); err == nil || !strings.Contains(err.Error(), "响应解码失败") {
		t.Fatalf("err = %v", err)
	}
}

func TestRaw4xxBodySnippetTruncated(t *testing.T) {
	long := strings.Repeat("x", 500)
	d := &fakeDoer{t: t, resps: []resp{{404, long, nil}}}
	c := newTestClient(t, d, WithRetry(0))
	err := c.Raw(context.Background(), `query { viewer { login } }`, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "…") {
		t.Fatalf("超长 body 应截断为 300 字符 + …, err = %v", err)
	}
}

func TestRawPayloadMarshalError(t *testing.T) {
	d := &fakeDoer{t: t}
	c := newTestClient(t, d, WithRetry(0))
	err := c.Raw(context.Background(), `query { viewer { login } }`, map[string]any{"bad": make(chan int)}, nil)
	if err == nil || !strings.Contains(err.Error(), "请求失败") {
		t.Fatalf("chan 不可序列化应报错, err = %v", err)
	}
}

func TestRawInvalidEndpoint(t *testing.T) {
	d := &fakeDoer{t: t}
	c := newTestClient(t, d, WithRetry(0), WithEndpoint("%"))
	if err := c.Raw(context.Background(), `query { viewer { login } }`, nil, nil); err == nil || !strings.Contains(err.Error(), "请求失败") {
		t.Fatalf("非法 endpoint 应报错, err = %v", err)
	}
}

func TestDoOnceBodyReadError(t *testing.T) {
	d := doerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(badReader{})}, nil
	})
	c := newTestClient(t, d, WithRetry(0))
	if err := c.Raw(context.Background(), `query { viewer { login } }`, nil, nil); err == nil || !strings.Contains(err.Error(), "请求失败") {
		t.Fatalf("读 body 失败应报错, err = %v", err)
	}
}

func TestSleepBackoffClampAndCancel(t *testing.T) {
	c := newTestClient(t, &fakeDoer{t: t})
	c.backoff = 0 // d=0 → 钳到 30s 上限
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.sleep(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// ── repoID ────────────────────────────────────────────────

func TestRepoIDEmptyAndError(t *testing.T) {
	t.Run("id为空报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"repository": map[string]any{"id": ""}}), nil}}}
		c := newTestClient(t, d)
		if _, err := c.repoID(context.Background()); err == nil || !strings.Contains(err.Error(), "repository.id 为空") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("查询失败包装报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{404, "not found", nil}}}
		c := newTestClient(t, d, WithRetry(0))
		if _, err := c.repoID(context.Background()); err == nil || !strings.Contains(err.Error(), "取 repositoryId") {
			t.Fatalf("err = %v", err)
		}
	})
}

// ── helpers 错误分支 ───────────────────────────────────────

func TestListRepoIssuesErrorBranches(t *testing.T) {
	t.Run("网络错误包装", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{404, "x", nil}}}
		c := newTestClient(t, d, WithRetry(0))
		if _, err := c.ListRepoIssues(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "ListRepoIssues:") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("节点缺id报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"repository": map[string]any{"issues": map[string]any{
			"totalCount": 1, "pageInfo": map[string]any{"hasNextPage": false}, "nodes": []any{map[string]any{"number": 1}},
		}}}), nil}}}
		c := newTestClient(t, d)
		if _, err := c.ListRepoIssues(context.Background(), ""); err == nil {
			t.Fatal("缺 id 应报错")
		}
	})
	t.Run("endCursor为空报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"repository": map[string]any{"issues": map[string]any{
			"totalCount": 1, "pageInfo": map[string]any{"hasNextPage": true, "endCursor": ""}, "nodes": []any{},
		}}}), nil}}}
		c := newTestClient(t, d)
		if _, err := c.ListRepoIssues(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "endCursor 为空") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("超过50页报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"repository": map[string]any{"issues": map[string]any{
			"totalCount": 99, "pageInfo": map[string]any{"hasNextPage": true, "endCursor": "C"}, "nodes": []any{},
		}}}), nil}}}
		c := newTestClient(t, d)
		if _, err := c.ListRepoIssues(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "超过 50 页") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestGetIssueRawError(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{404, "x", nil}}}
	c := newTestClient(t, d, WithRetry(0))
	if _, err := c.GetIssue(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "GetIssue:") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateIssueErrorBranches(t *testing.T) {
	t.Run("返回null报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{
			{200, okBody(t, map[string]any{"repository": map[string]any{"id": "R_1"}}), nil},
			{200, okBody(t, map[string]any{"createIssue": map[string]any{"issue": nil}}), nil},
		}}
		c := newTestClient(t, d)
		if _, err := c.CreateIssue(context.Background(), "t", "b"); err == nil {
			t.Fatal("null issue 应报错")
		}
	})
	t.Run("缺字段报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{
			{200, okBody(t, map[string]any{"repository": map[string]any{"id": "R_1"}}), nil},
			{200, okBody(t, map[string]any{"createIssue": map[string]any{"issue": map[string]any{"number": 0}}}), nil},
		}}
		c := newTestClient(t, d)
		if _, err := c.CreateIssue(context.Background(), "t", "b"); err == nil || !strings.Contains(err.Error(), "缺字段") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestTransitionErrorBranches(t *testing.T) {
	t.Run("网络错误包装", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{404, "x", nil}}}
		c := newTestClient(t, d, WithRetry(0))
		if err := c.CloseIssue(context.Background(), "I_1"); err == nil || !strings.Contains(err.Error(), "CloseIssue:") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("payload缺失报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{}), nil}}}
		c := newTestClient(t, d)
		if err := c.ReopenIssue(context.Background(), "I_1"); err == nil || !strings.Contains(err.Error(), "返回 issue 为 null") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestCreateLabelRepoIDErrorAndSuccess(t *testing.T) {
	t.Run("repositoryId失败", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{404, "x", nil}}}
		c := newTestClient(t, d, WithRetry(0))
		if err := c.CreateLabel(context.Background(), "bug", "ff0000", ""); err == nil {
			t.Fatal("repositoryId 失败应报错")
		}
	})
	t.Run("成功", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{
			{200, okBody(t, map[string]any{"repository": map[string]any{"id": "R_1"}}), nil},
			{200, okBody(t, map[string]any{"createLabel": map[string]any{"label": map[string]any{"id": "L_1", "name": "bug"}}}), nil},
		}}
		c := newTestClient(t, d)
		if err := c.CreateLabel(context.Background(), "bug", "ff0000", "缺陷"); err != nil {
			t.Fatalf("CreateLabel 应成功: %v", err)
		}
	})
}

func TestListIssueCommentsErrorBranches(t *testing.T) {
	page := func(hasNext bool, cur string, nodes ...any) map[string]any {
		return map[string]any{"repository": map[string]any{"issue": map[string]any{"comments": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": hasNext, "endCursor": cur},
			"nodes":    nodes,
		}}}}
	}
	t.Run("pageSize钳制", func(t *testing.T) {
		for _, size := range []int{0, 500} {
			d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, page(false, "")), nil}}}
			c := newTestClient(t, d)
			if _, err := c.ListIssueComments(context.Background(), 1, size); err != nil {
				t.Fatalf("pageSize=%d 应成功: %v", size, err)
			}
		}
	})
	t.Run("网络错误包装", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{404, "x", nil}}}
		c := newTestClient(t, d, WithRetry(0))
		if _, err := c.ListIssueComments(context.Background(), 1, 100); err == nil || !strings.Contains(err.Error(), "ListIssueComments:") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("评论缺id报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, page(false, "", map[string]any{"body": "x"})), nil}}}
		c := newTestClient(t, d)
		if _, err := c.ListIssueComments(context.Background(), 1, 100); err == nil || !strings.Contains(err.Error(), "缺 id") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("endCursor为空报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, page(true, "")), nil}}}
		c := newTestClient(t, d)
		if _, err := c.ListIssueComments(context.Background(), 1, 100); err == nil || !strings.Contains(err.Error(), "endCursor 为空") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("超过50页报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, page(true, "C")), nil}}}
		c := newTestClient(t, d)
		if _, err := c.ListIssueComments(context.Background(), 1, 100); err == nil || !strings.Contains(err.Error(), "超过 50 页") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDiscussionErrorBranches(t *testing.T) {
	t.Run("分类查询网络错误", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{404, "x", nil}}}
		c := newTestClient(t, d, WithRetry(0))
		if _, err := c.DiscussionCategories(context.Background()); err == nil || !strings.Contains(err.Error(), "DiscussionCategories:") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("发布时repositoryId失败", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{404, "x", nil}}}
		c := newTestClient(t, d, WithRetry(0))
		if _, err := c.CreateDiscussion(context.Background(), "DC_1", "t", "b"); err == nil {
			t.Fatal("repositoryId 失败应报错")
		}
	})
	t.Run("发布网络错误包装", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{
			{200, okBody(t, map[string]any{"repository": map[string]any{"id": "R_1"}}), nil},
			{404, "x", nil},
		}}
		c := newTestClient(t, d, WithRetry(0))
		if _, err := c.CreateDiscussion(context.Background(), "DC_1", "t", "b"); err == nil || !strings.Contains(err.Error(), "CreateDiscussion:") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("评论查询网络错误包装", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{404, "x", nil}}}
		c := newTestClient(t, d, WithRetry(0))
		if _, err := c.DiscThread(context.Background(), "D_1"); err == nil || !strings.Contains(err.Error(), "DiscThread:") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("node非对象解码报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"node": "not-an-object"}), nil}}}
		c := newTestClient(t, d)
		if _, err := c.DiscThread(context.Background(), "D_1"); err == nil {
			t.Fatal("node 非对象应解码报错")
		}
	})
	t.Run("node缺id报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"node": map[string]any{"comments": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": false}, "nodes": []any{},
		}}}), nil}}}
		c := newTestClient(t, d)
		if _, err := c.DiscThread(context.Background(), "D_1"); err == nil || !strings.Contains(err.Error(), "缺 id") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("评论缺id报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"node": map[string]any{
			"id": "D_1",
			"comments": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false}, "nodes": []any{map[string]any{"body": "x"}},
			},
		}}), nil}}}
		c := newTestClient(t, d)
		if _, err := c.DiscThread(context.Background(), "D_1"); err == nil || !strings.Contains(err.Error(), "缺 id") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("超过50页报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"node": map[string]any{
			"id": "D_1",
			"comments": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "K"}, "nodes": []any{},
			},
		}}), nil}}}
		c := newTestClient(t, d)
		if _, err := c.DiscThread(context.Background(), "D_1"); err == nil || !strings.Contains(err.Error(), "超过 50 页") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestIssueStatsRawError(t *testing.T) {
	d := &fakeDoer{t: t, resps: []resp{{404, "x", nil}}}
	c := newTestClient(t, d, WithRetry(0))
	if _, err := c.IssueStats(context.Background(), []string{"I_1"}); err == nil || !strings.Contains(err.Error(), "IssueStats:") {
		t.Fatalf("err = %v", err)
	}
}
