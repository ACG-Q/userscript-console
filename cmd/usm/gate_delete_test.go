package main

// 门禁删评（设计 D7）：未授权命令按 comment-id 删除触发评论。
// 契约：DELETE 恰一次、路径含该 id、输出 authorized:false；
// 失败只加 warning 不改退出码；comment-id 缺失 → 零调用。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acg-q/userscript-console/internal/github"
)

// stubGH 把 newGHClient 指向 ts 的客户端（REST endpoint = ts.URL）。
func stubGH(t *testing.T, ts *httptest.Server) {
	t.Helper()
	old := newGHClient
	newGHClient = func() *github.Client {
		c, err := github.New("tok", "o/r", github.WithRESTEndpoint(ts.URL))
		if err != nil {
			t.Fatalf("构造测试客户端失败: %v", err)
		}
		return c
	}
	t.Cleanup(func() { newGHClient = old })
}

type gatePayload struct {
	Authorized bool     `json:"authorized"`
	Changed    bool     `json:"changed"`
	Result     string   `json:"result"`
	Warnings   []string `json:"warnings"`
}

func decodeGatePayload(t *testing.T, out string) gatePayload {
	t.Helper()
	var p gatePayload
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, out)
	}
	return p
}

func gateArgs(commentID string) []string {
	args := []string{
		"run-command", "--json",
		"--comment-body=/list",
		"--comment-user=bob",
		"--issue-number=1",
		"--repo-owner=alice",
	}
	if commentID != "" {
		args = append(args, "--comment-id="+commentID)
	}
	return args
}

func Test门禁删评未授权删除一次(t *testing.T) {
	var deletes []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes = append(deletes, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	stubGH(t, ts)

	out := captureStdout(t, func() {
		if rc := run(gateArgs("42")); rc != 0 {
			t.Errorf("门禁未授权应 exit 0（结果文本承载信息）, got %d", rc)
		}
	})

	p := decodeGatePayload(t, out)
	if p.Authorized {
		t.Error("输出应为 authorized:false")
	}
	if !strings.Contains(p.Result, "权限不足") {
		t.Errorf("结果应含权限不足文案, got %q", p.Result)
	}
	if len(deletes) != 1 {
		t.Fatalf("DELETE 应恰一次, got %d 次: %v", len(deletes), deletes)
	}
	if !strings.Contains(deletes[0], "/issues/comments/42") {
		t.Errorf("DELETE 路径应含评论 id 42, got %s", deletes[0])
	}
}

func Test门禁删评commentID缺失零调用(t *testing.T) {
	t.Setenv("COMMENT_ID", "")
	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	stubGH(t, ts)

	out := captureStdout(t, func() {
		if rc := run(gateArgs("")); rc != 0 {
			t.Errorf("门禁未授权应 exit 0, got %d", rc)
		}
	})

	p := decodeGatePayload(t, out)
	if p.Authorized {
		t.Error("输出应为 authorized:false")
	}
	if calls != 0 {
		t.Errorf("comment-id 缺失应零网络调用, got %d", calls)
	}
}

func Test门禁删评失败只加warning不改退出码(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	defer ts.Close()
	stubGH(t, ts)

	out := captureStdout(t, func() {
		if rc := run(gateArgs("42")); rc != 0 {
			t.Errorf("删评失败不应改退出码, got %d", rc)
		}
	})

	p := decodeGatePayload(t, out)
	if p.Authorized {
		t.Error("输出应为 authorized:false")
	}
	found := false
	for _, w := range p.Warnings {
		if strings.Contains(w, "门禁评论删除失败") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings 应含「门禁评论删除失败」, got %v", p.Warnings)
	}
}
