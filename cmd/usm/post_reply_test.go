package main

// 执行回帖（设计 D7）：postReply && authorized && 结果非空 && GitHub 已配置
// → POST **执行结果：** + 正文；失败只记 warning 不改退出码；
// post-reply 关闭 / GitHub 未配置 / 未授权 → 零调用。

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type replyRec struct {
	Method string
	Path   string
	Body   string
}

// stubReply 记录所有打到 ts 的请求，并把 newGHClient 指向 ts。
func stubReply(t *testing.T, status int) func() []replyRec {
	t.Helper()
	recs := []replyRec{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		recs = append(recs, replyRec{r.Method, r.URL.Path, string(b)})
		w.WriteHeader(status)
	}))
	t.Cleanup(ts.Close)
	stubGH(t, ts)
	return func() []replyRec {
		out := make([]replyRec, len(recs))
		copy(out, recs)
		return out
	}
}

// authorizedArgs 授权路径的最小参数集（comment-user == repo-owner）。
func authorizedArgs(extra ...string) []string {
	args := []string{
		"run-command", "--json",
		"--comment-body=/list",
		"--comment-user=alice",
		"--issue-number=1",
		"--repo-owner=alice",
	}
	return append(args, extra...)
}

func replyBody(t *testing.T, raw string) string {
	t.Helper()
	var body struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("回帖请求体不是合法 JSON: %v\n%s", err, raw)
	}
	return body.Body
}

func Test回帖成功执行结果(t *testing.T) {
	t.Setenv("USM_ROOT", buildTestRegistryDir(t))
	recs := stubReply(t, http.StatusCreated)

	out := captureStdout(t, func() {
		if rc := run(authorizedArgs()); rc != 0 {
			t.Errorf("成功执行应 exit 0, got %d", rc)
		}
	})

	p := decodeGatePayload(t, out)
	if !p.Authorized {
		t.Error("授权路径应为 authorized:true")
	}
	if p.Result == "" {
		t.Fatal("结果文本不应为空（回帖前置条件）")
	}
	got := recs()
	if len(got) != 1 {
		t.Fatalf("应恰一次 POST, got %d: %+v", len(got), got)
	}
	if got[0].Method != http.MethodPost {
		t.Errorf("方法应为 POST, got %s", got[0].Method)
	}
	if !strings.HasSuffix(got[0].Path, "/issues/1/comments") {
		t.Errorf("路径应落在 /issues/1/comments, got %s", got[0].Path)
	}
	body := replyBody(t, got[0].Body)
	if !strings.HasPrefix(body, "**执行结果：**") {
		t.Errorf("回帖应以 **执行结果：** 开头, got %q", body)
	}
	if !strings.Contains(body, p.Result) {
		t.Errorf("回帖应含 result 正文\nresult: %q\nbody: %q", p.Result, body)
	}
	if len(p.Warnings) != 0 {
		t.Errorf("回帖成功不应有 warning, got %v", p.Warnings)
	}
}

func Test回帖rc1且结果非空也回帖(t *testing.T) {
	t.Setenv("USM_ROOT", buildTestRegistryDir(t))
	recs := stubReply(t, http.StatusCreated)
	badFile := filepath.Join(t.TempDir(), "no-such-dir", "out.txt")

	out := captureStdout(t, func() {
		if rc := run(authorizedArgs("--result-file=" + badFile)); rc != 1 {
			t.Errorf("结果文件写失败应 exit 1, got %d", rc)
		}
	})

	p := decodeGatePayload(t, out)
	if p.Result == "" {
		t.Fatal("结果文本不应为空")
	}
	if got := recs(); len(got) != 1 {
		t.Errorf("rc=1 但结果非空仍应回帖一次, got %d", len(got))
	}
}

func Test回帖失败只加warning不改退出码(t *testing.T) {
	t.Setenv("USM_ROOT", buildTestRegistryDir(t))
	recs := stubReply(t, http.StatusInternalServerError)

	out := captureStdout(t, func() {
		if rc := run(authorizedArgs()); rc != 0 {
			t.Errorf("回帖失败不应改退出码, got %d", rc)
		}
	})

	p := decodeGatePayload(t, out)
	if len(recs()) != 1 {
		t.Error("应尝试回帖一次")
	}
	found := false
	for _, w := range p.Warnings {
		if strings.Contains(w, "回帖失败") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings 应含「回帖失败」, got %v", p.Warnings)
	}
}

func Test回帖关闭时零调用(t *testing.T) {
	t.Run("flag=false", func(t *testing.T) {
		t.Setenv("USM_ROOT", buildTestRegistryDir(t))
		recs := stubReply(t, http.StatusCreated)
		captureStdout(t, func() {
			if rc := run(authorizedArgs("--post-reply=false")); rc != 0 {
				t.Errorf("应 exit 0, got %d", rc)
			}
		})
		if got := recs(); len(got) != 0 {
			t.Errorf("--post-reply=false 应零调用, got %d", len(got))
		}
	})

	t.Run("env=false", func(t *testing.T) {
		t.Setenv("USM_ROOT", buildTestRegistryDir(t))
		t.Setenv("POST_REPLY", "false")
		recs := stubReply(t, http.StatusCreated)
		captureStdout(t, func() {
			if rc := run(authorizedArgs()); rc != 0 {
				t.Errorf("应 exit 0, got %d", rc)
			}
		})
		if got := recs(); len(got) != 0 {
			t.Errorf("POST_REPLY=false 应零调用, got %d", len(got))
		}
	})

	// GitHub 未配置（TestMain 已清 GITHUB_TOKEN）→ 客户端 nil → 零调用且无 warning。
	t.Run("ghc=nil", func(t *testing.T) {
		t.Setenv("USM_ROOT", buildTestRegistryDir(t))
		out := captureStdout(t, func() {
			if rc := run(authorizedArgs()); rc != 0 {
				t.Errorf("应 exit 0, got %d", rc)
			}
		})
		p := decodeGatePayload(t, out)
		if len(p.Warnings) != 0 {
			t.Errorf("GitHub 未配置应静默跳过回帖, got %v", p.Warnings)
		}
	})
}

func Test回帖未授权零调用(t *testing.T) {
	recs := stubReply(t, http.StatusCreated)

	out := captureStdout(t, func() {
		if rc := run(gateArgs("42")); rc != 0 {
			t.Errorf("门禁未授权应 exit 0, got %d", rc)
		}
	})

	p := decodeGatePayload(t, out)
	if p.Authorized {
		t.Error("应为 authorized:false")
	}
	posts, deletes := 0, 0
	for _, r := range recs() {
		switch r.Method {
		case http.MethodPost:
			posts++
		case http.MethodDelete:
			deletes++
		}
	}
	if posts != 0 {
		t.Errorf("未授权不得回帖, got %d 次 POST", posts)
	}
	if deletes != 1 {
		t.Errorf("未授权应删评一次, got %d 次 DELETE", deletes)
	}
}

func TestPostReply非法env退出2(t *testing.T) {
	t.Setenv("POST_REPLY", "maybe")
	if rc := run(authorizedArgs()); rc != 2 {
		t.Errorf("POST_REPLY 非法应 exit 2, got %d", rc)
	}
}
