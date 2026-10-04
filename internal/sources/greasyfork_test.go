package sources

import (
	"context"
	"strings"
	"testing"
)

// ── fixtures（内联字符串） ─────────────────────────────────

const gfScript = `// ==UserScript==
// @name 一级脚本
// @version 2.0.0
// @description 一级描述
// @author 一级作者
// @match *://*.example.com/*
// @grant GM_xmlhttpRequest
// ==/UserScript==
console.log("一级");
`

// 二级页面：内嵌头块（缩进归一 + HTML 实体），并带 meta/title 兜底值。
const gfPageWithBlock = `<!doctype html>
<html><head>
<meta name="description" content="页面兜底描述">
<meta name="author" content="页面兜底作者">
<title>页面兜底名 | Greasy Fork</title>
</head><body><pre class="prettyprint">
// ==UserScript==
//          @name 内嵌脚本
//          @version 3.1.4
//          @description 内嵌描述
//          @author 内嵌作者
//          @match *://*.example.com/*
//          @grant GM_setValue
// ==/UserScript==
</pre></body></html>
`

// 二级页面：无内嵌块，只能靠 meta/title。
const gfPageNoBlock = `<!doctype html>
<html><head>
<meta name="description" content="页面兜底描述">
<meta name="author" content="页面兜底作者">
<title>页面兜底名 | Greasy Fork</title>
</head><body><p>脚本说明段落，但没有源码头。</p></body></html>
`

// ── MatchURL 白名单 ──────────────────────────────────────

func TestGreasyfork_MatchURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"裸域", "https://greasyfork.org/scripts/1", true},
		{"www子域", "https://www.greasyfork.org/scripts/1", true},
		{"sleazyfork", "https://sleazyfork.org/scripts/1", true},
		{"http协议", "http://www.greasyfork.org/scripts/1", true},
		{"FQDN尾点", "https://www.greasyfork.org./scripts/1", true},
		{"前缀拼接绕过", "https://evilgreasyfork.org/scripts/1", false},
		{"后缀挂域名绕过", "https://greasyfork.org.evil.com/scripts/1", false},
		{"path混入绕过", "https://evil.com/path/greasyfork.org/scripts/1", false},
		{"ftp协议拒绝", "ftp://greasyfork.org/scripts/1", false},
		{"别的白名单站", "https://userscript.zone/x", false},
		{"坏URL", "://bad", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (greasyforkAdapter{}).MatchURL(c.url); got != c.want {
				t.Fatalf("MatchURL(%q) = %v, 期望 %v", c.url, got, c.want)
			}
		})
	}
}

// ── 一级：.code.user.js 直链成功（含 locale 查询去除断言） ──

func TestGreasyfork_一级成功与locale去除(t *testing.T) {
	pageURL := "https://www.greasyfork.org/zh-CN/scripts/418600-example?locale=zh-CN"
	codeURL := "https://www.greasyfork.org/scripts/418600.code.user.js"
	f := newFake(map[string]fakeRoute{
		codeURL: ok(gfScript),
		pageURL: ok(gfPageWithBlock), // 不该被请求，登记仅为防御
	})
	a, err := Detect(pageURL)
	if err != nil || a.Type() != TypeGreasyFork {
		t.Fatalf("Detect = %v, %v", a, err)
	}
	res, err := a.Fetch(context.Background(), f, pageURL)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	// locale 查询与语言路径前缀必须被丢弃
	if len(f.reqs) != 1 || f.reqs[0].URL.String() != codeURL {
		t.Fatalf("请求 = %v, 期望只请求 %s", f.urls(), codeURL)
	}
	if q := f.reqs[0].URL.RawQuery; q != "" {
		t.Fatalf("一级直链不得携带查询（实际 %q）", q)
	}
	if res.Name != "一级脚本" || res.Version != "2.0.0" ||
		res.Description != "一级描述" || res.Author != "一级作者" {
		t.Fatalf("元数据不符: %+v", res)
	}
	if len(res.Match) != 1 || res.Match[0] != "*://*.example.com/*" {
		t.Fatalf("Match = %v", res.Match)
	}
	if len(res.Grant) != 1 || res.Grant[0] != "GM_xmlhttpRequest" {
		t.Fatalf("Grant = %v", res.Grant)
	}
	if res.Code != gfScript || res.SourceType != TypeGreasyFork {
		t.Fatalf("Code/SourceType 不符: %q / %q", res.Code, res.SourceType)
	}
}

// ── 一级 404 → 二级页面内嵌头块回退 ─────────────────────────

func TestGreasyfork_一级404回退二级内嵌块(t *testing.T) {
	pageURL := "https://greasyfork.org/scripts/777-demo"
	codeURL := "https://greasyfork.org/scripts/777.code.user.js"
	f := newFake(map[string]fakeRoute{
		codeURL: code(404),
		pageURL: ok(gfPageWithBlock),
	})
	res, err := (greasyforkAdapter{}).Fetch(context.Background(), f, pageURL)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	if len(f.reqs) != 2 {
		t.Fatalf("请求 = %v, 期望先一级后二级共 2 次", f.urls())
	}
	if res.Name != "内嵌脚本" || res.Version != "3.1.4" ||
		res.Description != "内嵌描述" || res.Author != "内嵌作者" {
		t.Fatalf("二级内嵌块元数据不符: %+v", res)
	}
	if len(res.Match) != 1 || res.Match[0] != "*://*.example.com/*" {
		t.Fatalf("Match = %v", res.Match)
	}
	if len(res.Grant) != 1 || res.Grant[0] != "GM_setValue" {
		t.Fatalf("Grant = %v", res.Grant)
	}
	if !strings.Contains(res.Code, "==UserScript==") || res.SourceType != TypeGreasyFork {
		t.Fatalf("Code/SourceType 不符: %q / %q", res.Code, res.SourceType)
	}
}

// ── 一级 200 但无头 → 同样回退二级 ──────────────────────────

func TestGreasyfork_一级无头回退二级(t *testing.T) {
	pageURL := "https://greasyfork.org/scripts/888-demo"
	codeURL := "https://greasyfork.org/scripts/888.code.user.js"
	f := newFake(map[string]fakeRoute{
		codeURL: ok("<html>反爬挑战页，不是脚本</html>"),
		pageURL: ok(gfPageWithBlock),
	})
	res, err := (greasyforkAdapter{}).Fetch(context.Background(), f, pageURL)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	if res.Name != "内嵌脚本" {
		t.Fatalf("应回退到二级内嵌块, 实际 %+v", res)
	}
}

// ── 一级 404 + 二级 500 → 组合错误（含两级上下文与 %w 链） ──

func TestGreasyfork_两级失败组合错误(t *testing.T) {
	pageURL := "https://greasyfork.org/scripts/999-dead"
	codeURL := "https://greasyfork.org/scripts/999.code.user.js"
	f := newFake(map[string]fakeRoute{
		codeURL: code(404),
		pageURL: code(500),
	})
	_, err := (greasyforkAdapter{}).Fetch(context.Background(), f, pageURL)
	if err == nil {
		t.Fatal("两级都失败应报错")
	}
	msg := err.Error()
	for _, want := range []string{codeURL, pageURL, "404", "500"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("组合错误缺少 %q: %v", want, err)
		}
	}
}

// ── 无数字 id → 只走二级 ─────────────────────────────────

func TestGreasyfork_无id仅二级(t *testing.T) {
	pageURL := "https://www.sleazyfork.org/scripts/this-script-has-no-numeric-id"
	f := newFake(map[string]fakeRoute{
		pageURL: ok(gfPageWithBlock),
	})
	res, err := (greasyforkAdapter{}).Fetch(context.Background(), f, pageURL)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	if len(f.reqs) != 1 || f.reqs[0].URL.String() != pageURL {
		t.Fatalf("请求 = %v, 期望只请求页面一次", f.urls())
	}
	if res.Name != "内嵌脚本" || res.SourceType != TypeGreasyFork {
		t.Fatalf("结果不符: %+v", res)
	}
}

// ── 二级页面无内嵌块 → meta/title 兜底（去站点后缀） ──────────

func TestGreasyfork_页面meta兜底(t *testing.T) {
	pageURL := "https://greasyfork.org/scripts/1000-metabase"
	codeURL := "https://greasyfork.org/scripts/1000.code.user.js"
	f := newFake(map[string]fakeRoute{
		codeURL: code(404),
		pageURL: ok(gfPageNoBlock),
	})
	res, err := (greasyforkAdapter{}).Fetch(context.Background(), f, pageURL)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	if res.Name != "页面兜底名" {
		t.Fatalf("Name 应去掉 | Greasy Fork 后缀, 实际 %q", res.Name)
	}
	if res.Description != "页面兜底描述" || res.Author != "页面兜底作者" {
		t.Fatalf("meta 兜底不符: %+v", res)
	}
	if res.Code != "" || res.SourceType != TypeGreasyFork {
		t.Fatalf("无源码时 Code 应为空: %+v", res)
	}
}

// ── 二级也失败 → 无 id 时报二级上下文；有 id 时报组合 ─────────

func TestGreasyfork_二级失败无id(t *testing.T) {
	pageURL := "https://greasyfork.org/scripts/not-numeric"
	f := newFake(map[string]fakeRoute{
		pageURL: code(404),
	})
	_, err := (greasyforkAdapter{}).Fetch(context.Background(), f, pageURL)
	if err == nil {
		t.Fatal("二级 404 应报错")
	}
	if !strings.Contains(err.Error(), pageURL) || !strings.Contains(err.Error(), "404") {
		t.Fatalf("错误缺 URL/状态上下文: %v", err)
	}
	if strings.Contains(err.Error(), "一级") {
		t.Fatalf("无 id 不应出现一级上下文: %v", err)
	}
}
