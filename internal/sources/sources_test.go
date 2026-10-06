package sources

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// ── fake Doer：按 URL 返回 canned 响应（内联字符串，禁止真实网络） ──

type fakeRoute struct {
	status int
	body   string
	err    error // 模拟连接层失败
}

type fakeDoer struct {
	routes map[string]fakeRoute
	reqs   []*http.Request // 记录实际发出的请求，供断言
}

func newFake(routes map[string]fakeRoute) *fakeDoer {
	return &fakeDoer{routes: routes}
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.reqs = append(f.reqs, req)
	r, ok := f.routes[req.URL.String()]
	if !ok {
		return nil, fmt.Errorf("fakeDoer: 未登记的 URL %s", req.URL.String())
	}
	if r.err != nil {
		return nil, r.err
	}
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     http.Header{},
	}, nil
}

func ok(body string) fakeRoute  { return fakeRoute{status: 200, body: body} }
func code(status int) fakeRoute { return fakeRoute{status: status, body: "err page"} }

// 请求过的 URL 列表（按发出顺序）。
func (f *fakeDoer) urls() []string {
	out := make([]string, 0, len(f.reqs))
	for _, r := range f.reqs {
		out = append(out, r.URL.String())
	}
	return out
}

// ── MatchHostSuffix：4 类边界 ─────────────────────────────

func TestMatchHostSuffix(t *testing.T) {
	cases := []struct {
		name    string
		host    string
		pattern string
		want    bool
	}{
		// ① 完全相等
		{"完全相等", "greasyfork.org", "greasyfork.org", true},
		{"完全相等_大写归一", "GreasyFork.ORG", "greasyfork.org", true},
		// ② 子域命中
		{"子域命中", "www.greasyfork.org", "greasyfork.org", true},
		{"多级子域命中", "a.b.greasyfork.org", "greasyfork.org", true},
		// ③ 绕过负向
		{"前缀拼接不命中", "evilgreasyfork.org", "greasyfork.org", false},
		{"后缀挂域名不命中", "greasyfork.org.evil.com", "greasyfork.org", false},
		{"整体就是别的域", "example.com", "greasyfork.org", false},
		{"path 混入非 host", "evil.com/path/greasyfork.org", "greasyfork.org", false},
		{"点前缀拼接不命中", ".greasyfork.org.evil.com", "greasyfork.org", false},
		// ④ FQDN 尾点 / 大小写
		{"FQDN尾点相等", "greasyfork.org.", "greasyfork.org", true},
		{"FQDN尾点子域", "www.greasyfork.org.", "greasyfork.org", true},
		{"混合大小写与尾点", "WWW.GreasyFork.Org.", "greasyfork.org", true},
		// 空值防御
		{"host 为空", "", "greasyfork.org", false},
		{"pattern 为空", "greasyfork.org", "", false},
		{"两侧都空", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MatchHostSuffix(c.host, c.pattern); got != c.want {
				t.Fatalf("MatchHostSuffix(%q, %q) = %v, 期望 %v", c.host, c.pattern, got, c.want)
			}
		})
	}
}

// ── Detect：选型 / 非法 URL / 注册清单 ──────────────────────

func TestDetect_选型(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"greasyfork裸域", "https://greasyfork.org/scripts/1234", TypeGreasyFork},
		{"greasyforkwww子域", "https://www.greasyfork.org/zh-CN/scripts/1234?locale=en", TypeGreasyFork},
		{"sleazyfork白名单", "https://sleazyfork.org/scripts/9", TypeGreasyFork},
		{"userscriptzone", "https://userscript.zone/scripts/abc", TypeZone},
		{"gist页面", "https://gist.github.com/someone/deadbeef", TypeGist},
		{"gistAPI", "https://api.github.com/gists/deadbeef", TypeGist},
		{"http协议同样识别", "http://greasyfork.org/scripts/1", TypeGreasyFork},
		// 白名单绕过 → 一律走 direct 兜底
		{"path混入绕过", "https://evil.com/path/greasyfork.org", TypeDirect},
		{"前缀拼接绕过", "https://evilgreasyfork.org/scripts/1", TypeDirect},
		{"后缀挂域名绕过", "https://greasyfork.org.evil.com/x", TypeDirect},
		{"非白名单普通站", "https://example.com/foo.user.js", TypeDirect},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, err := Detect(c.url)
			if err != nil {
				t.Fatalf("Detect(%q) 报错: %v", c.url, err)
			}
			if a.Type() != c.want {
				t.Fatalf("Detect(%q) = %q, 期望 %q", c.url, a.Type(), c.want)
			}
		})
	}
}

func TestDetect_非法URL(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"空串", ""},
		{"无scheme", "greasyfork.org/scripts/1"},
		{"ftp协议", "ftp://greasyfork.org/scripts/1"},
		{"缺host", "http://"},
		{"坏URL解析失败", "http://exa mple.com/"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if a, err := Detect(c.url); err == nil {
				t.Fatalf("Detect(%q) 应报错，却返回适配器 %v", c.url, a)
			}
		})
	}
}

// 注册清单硬编码断言：忘注册必须让测试红（SPEC-ARCH-TEST §7 通用铁律）。
func TestDetect_注册清单(t *testing.T) {
	var got []string
	for _, a := range registry {
		got = append(got, a.Type())
	}
	sort.Strings(got)
	want := []string{TypeGist, TypeGreasyFork, TypeZone}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("适配器注册清单 = %v, 期望 %v（新增适配器请同步更新本断言）", got, want)
	}
	for _, ty := range got {
		if ty == TypeDirect {
			t.Fatal("direct 不得注册进 registry（MatchURL 恒真会吞掉所有路由）")
		}
	}
}

func TestDetect_兜底direct(t *testing.T) {
	a, err := Detect("https://example.com/plain.user.js")
	if err != nil {
		t.Fatalf("Detect 报错: %v", err)
	}
	if a.Type() != TypeDirect {
		t.Fatalf("兜底适配器 = %q, 期望 %q", a.Type(), TypeDirect)
	}
	if !a.MatchURL("https://anything.example/x") {
		t.Fatal("direct.MatchURL 应恒为 true")
	}
}

// ── HTTPGet ──────────────────────────────────────────────

func TestHTTPGet_成功与请求头(t *testing.T) {
	f := newFake(map[string]fakeRoute{"https://a.example/x": ok("hello-body")})
	got, err := HTTPGet(context.Background(), f, "https://a.example/x")
	if err != nil {
		t.Fatalf("HTTPGet 报错: %v", err)
	}
	if string(got) != "hello-body" {
		t.Fatalf("body = %q, 期望 %q", got, "hello-body")
	}
	if len(f.reqs) != 1 {
		t.Fatalf("请求数 = %d, 期望 1", len(f.reqs))
	}
	req := f.reqs[0]
	if req.Method != http.MethodGet {
		t.Fatalf("方法 = %s, 期望 GET", req.Method)
	}
	if !strings.Contains(req.Header.Get("User-Agent"), "Mozilla") {
		t.Fatalf("UA 缺少浏览器标识: %q", req.Header.Get("User-Agent"))
	}
	if req.Header.Get("Accept") == "" {
		t.Fatal("Accept 头缺失")
	}
	if req.Header.Get("Accept-Language") == "" {
		t.Fatal("Accept-Language 头缺失（GreasyFork 等站点会 403 拦截）")
	}
}

func TestHTTPGet_非2xx报错(t *testing.T) {
	cases := []int{404, 500, 301}
	for _, st := range cases {
		t.Run(fmt.Sprintf("状态%d", st), func(t *testing.T) {
			f := newFake(map[string]fakeRoute{"https://a.example/x": code(st)})
			_, err := HTTPGet(context.Background(), f, "https://a.example/x")
			if err == nil {
				t.Fatal("非 2xx 应报错")
			}
			if !strings.Contains(err.Error(), fmt.Sprint(st)) {
				t.Fatalf("错误未含状态码 %d: %v", st, err)
			}
			if !strings.Contains(err.Error(), "https://a.example/x") {
				t.Fatalf("错误未含 URL: %v", err)
			}
		})
	}
}

func TestHTTPGet_已取消ctx报错(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := newFake(map[string]fakeRoute{"https://a.example/x": ok("never")})
	_, err := HTTPGet(ctx, f, "https://a.example/x")
	if err == nil {
		t.Fatal("已取消 ctx 应报错")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("错误应可 errors.Is(context.Canceled): %v", err)
	}
	if len(f.reqs) != 0 {
		t.Fatal("ctx 已取消不应发出请求")
	}
}

func TestHTTPGet_连接层错误(t *testing.T) {
	f := newFake(map[string]fakeRoute{
		"https://a.example/x": {err: errors.New("connection refused")},
	})
	_, err := HTTPGet(context.Background(), f, "https://a.example/x")
	if err == nil {
		t.Fatal("Doer 报错应向上传播")
	}
	if !strings.Contains(err.Error(), "connection refused") ||
		!strings.Contains(err.Error(), "https://a.example/x") {
		t.Fatalf("错误缺上下文: %v", err)
	}
}

// ── extractUserScriptBlock ───────────────────────────────

func TestExtractUserScriptBlock(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "标准块与行内归一",
			in:   "<html>\n<pre>// ==UserScript==\n//         @name 示例\n//         @version 1.0\n// ==/UserScript==\n</pre></html>",
			want: "// ==UserScript==\n// @name 示例\n// @version 1.0\n// ==/UserScript==",
			ok:   true,
		},
		{
			name: "HTML实体反转义",
			in:   "<div>// ==UserScript==\n// @name A &amp; B\n// ==/UserScript==</div>",
			want: "// ==UserScript==\n// @name A & B\n// ==/UserScript==",
			ok:   true,
		},
		{
			name: "裸at行补注释前缀",
			in:   "// ==UserScript==\n@name 裸行\n// ==/UserScript==",
			want: "// ==UserScript==\n// @name 裸行\n// ==/UserScript==",
			ok:   true,
		},
		{
			name: "无开token",
			in:   "<p>只是普通页面 // @name x</p>",
			ok:   false,
		},
		{
			name: "有开无闭视为残块",
			in:   "// ==UserScript==\n// @name 残缺",
			ok:   false,
		},
		{
			name: "CRLF归一",
			in:   "// ==UserScript==\r\n// @name W\r\n// ==/UserScript==\r",
			want: "// ==UserScript==\n// @name W\n// ==/UserScript==",
			ok:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := extractUserScriptBlock(c.in)
			if ok != c.ok {
				t.Fatalf("ok = %v, 期望 %v（got=%q）", ok, c.ok, got)
			}
			if c.ok && got != c.want {
				t.Fatalf("块内容 =\n%q\n期望\n%q", got, c.want)
			}
		})
	}
}
