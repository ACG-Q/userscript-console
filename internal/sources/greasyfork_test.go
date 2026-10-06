package sources

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── fixtures（文件回放，SPEC-ARCH-TEST §3.1） ─────────────────

func mustFixture(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(fixturePath(rel))
	if err != nil {
		t.Fatalf("读取 fixture %q 失败: %v", rel, err)
	}
	return string(b)
}

func fixturePath(rel string) string {
	// 测试文件在 internal/sources/，fixtures 在 tests/fixtures/
	return filepath.Join("..", "..", "tests", "fixtures", rel)
}

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

// ── 一级：update 子域直链成功（含 locale 查询去除断言） ────────

func TestGreasyfork_一级成功与locale去除(t *testing.T) {
	pageURL := "https://www.greasyfork.org/zh-CN/scripts/418600-example?locale=zh-CN"
	codeURL := "https://update.greasyfork.org/scripts/418600.user.js"
	script := mustFixture(t, "direct/example_user_script.user.js")
	f := newFake(map[string]fakeRoute{
		codeURL: ok(script),
	})
	a, err := Detect(pageURL)
	if err != nil || a.Type() != TypeGreasyFork {
		t.Fatalf("Detect = %v, %v", a, err)
	}
	res, err := a.Fetch(context.Background(), f, pageURL)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	if len(f.reqs) != 1 || f.reqs[0].URL.String() != codeURL {
		t.Fatalf("请求 = %v, 期望只请求 %s", f.urls(), codeURL)
	}
	if q := f.reqs[0].URL.RawQuery; q != "" {
		t.Fatalf("一级直链不得携带查询（实际 %q）", q)
	}
	if res.Name != "GreasyFork Fixture Script" || res.Version != "1.2.3" {
		t.Fatalf("元数据不符: %+v", res)
	}
	if res.SourceType != TypeGreasyFork {
		t.Fatalf("SourceType = %q", res.SourceType)
	}
}

// ── 一级：sleazyfork 映射 update.sleazyfork.org ──────────────

func TestGreasyfork_Sleazyfork一级(t *testing.T) {
	pageURL := "https://sleazyfork.org/scripts/555-demo"
	codeURL := "https://update.sleazyfork.org/scripts/555.user.js"
	script := mustFixture(t, "direct/example_user_script.user.js")
	f := newFake(map[string]fakeRoute{
		codeURL: ok(script),
	})
	res, err := (greasyforkAdapter{}).Fetch(context.Background(), f, pageURL)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	if len(f.reqs) != 1 || f.reqs[0].URL.String() != codeURL {
		t.Fatalf("请求 = %v, 期望只请求 %s", f.urls(), codeURL)
	}
	if res.Name != "GreasyFork Fixture Script" || res.SourceType != TypeGreasyFork {
		t.Fatalf("元数据不符: %+v", res)
	}
}

// ── 一级 404 → 二级页面内嵌头块回退 ─────────────────────────

func TestGreasyfork_一级404回退二级内嵌块(t *testing.T) {
	pageURL := "https://greasyfork.org/scripts/777-demo"
	codeURL := "https://update.greasyfork.org/scripts/777.user.js"
	pageHTML := mustFixture(t, "greasyfork/page_with_block.html")
	f := newFake(map[string]fakeRoute{
		codeURL: code(404),
		pageURL: ok(pageHTML),
	})
	res, err := (greasyforkAdapter{}).Fetch(context.Background(), f, pageURL)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	if len(f.reqs) != 2 {
		t.Fatalf("请求 = %v, 期望先一级后二级共 2 次", f.urls())
	}
	if res.Name != "GreasyFork Inline Fixture" || res.SourceType != TypeGreasyFork {
		t.Fatalf("元数据不符: %+v", res)
	}
}

// ── 一级 200 但无头 → 同样回退二级 ──────────────────────────

func TestGreasyfork_一级无头回退二级(t *testing.T) {
	pageURL := "https://greasyfork.org/scripts/888-demo"
	codeURL := "https://update.greasyfork.org/scripts/888.user.js"
	pageHTML := mustFixture(t, "greasyfork/page_with_block.html")
	f := newFake(map[string]fakeRoute{
		codeURL: ok("<html>反爬挑战页，不是脚本</html>"),
		pageURL: ok(pageHTML),
	})
	res, err := (greasyforkAdapter{}).Fetch(context.Background(), f, pageURL)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	if res.Name != "GreasyFork Inline Fixture" {
		t.Fatalf("应回退到二级内嵌块, 实际 %+v", res)
	}
}

// ── 一级 404 → 页面无内嵌块 → 安装直链回退 ───────────────────

func TestGreasyfork_安装直链回退(t *testing.T) {
	pageURL := "https://greasyfork.org/scripts/776-install"
	codeURL := "https://update.greasyfork.org/scripts/776.user.js"
	pageHTML := mustFixture(t, "greasyfork/page_install_link.html")
	installURL := "https://update.greasyfork.org/scripts/776/GreasyFork-InstallLink-Fixture.user.js"
	script := mustFixture(t, "direct/example_user_script.user.js")
	f := newFake(map[string]fakeRoute{
		codeURL:    code(404),
		pageURL:    ok(pageHTML),
		installURL: ok(script),
	})
	res, err := (greasyforkAdapter{}).Fetch(context.Background(), f, pageURL)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	want := []string{codeURL, pageURL, installURL}
	if len(f.reqs) != len(want) {
		t.Fatalf("请求 = %v, 期望 %v", f.urls(), want)
	}
	for i, w := range want {
		if f.reqs[i].URL.String() != w {
			t.Fatalf("第 %d 次请求 = %s, 期望 %s（全量 %v）", i, f.reqs[i].URL.String(), w, f.urls())
		}
	}
	if res.Name != "GreasyFork Fixture Script" || res.SourceType != TypeGreasyFork {
		t.Fatalf("元数据不符: %+v", res)
	}
}

// ── 一级 404 → 页面无内嵌块/安装直链 → /code 源码页回退 ───────

func TestGreasyfork_Code页回退(t *testing.T) {
	pageURL := "https://greasyfork.org/scripts/779-source"
	codeURL := "https://update.greasyfork.org/scripts/779.user.js"
	pageHTML := mustFixture(t, "greasyfork/page_no_block.html")
	srcPageURL := "https://greasyfork.org/scripts/779-source/code"
	codePage := mustFixture(t, "greasyfork/code_page.html")
	f := newFake(map[string]fakeRoute{
		codeURL:    code(404),
		pageURL:    ok(pageHTML),
		srcPageURL: ok(codePage),
	})
	res, err := (greasyforkAdapter{}).Fetch(context.Background(), f, pageURL)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	want := []string{codeURL, pageURL, srcPageURL}
	if len(f.reqs) != len(want) {
		t.Fatalf("请求 = %v, 期望 %v", f.urls(), want)
	}
	for i, w := range want {
		if f.reqs[i].URL.String() != w {
			t.Fatalf("第 %d 次请求 = %s, 期望 %s（全量 %v）", i, f.reqs[i].URL.String(), w, f.urls())
		}
	}
	if res.Name != "GreasyFork CodePage Fixture" || res.Version != "2.0.0" {
		t.Fatalf("元数据不符: %+v", res)
	}
	if !strings.Contains(res.Code, "// ==/UserScript==") {
		t.Fatalf("Code 应含完整头块: %q", res.Code)
	}
}

// ── 一级 404 + 二级 500 → 组合错误（含两级上下文与 %w 链） ──────

func TestGreasyfork_两级失败组合错误(t *testing.T) {
	pageURL := "https://greasyfork.org/scripts/999-dead"
	codeURL := "https://update.greasyfork.org/scripts/999.user.js"
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
	pageHTML := mustFixture(t, "greasyfork/page_with_block.html")
	f := newFake(map[string]fakeRoute{
		pageURL: ok(pageHTML),
	})
	res, err := (greasyforkAdapter{}).Fetch(context.Background(), f, pageURL)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	if len(f.reqs) != 1 || f.reqs[0].URL.String() != pageURL {
		t.Fatalf("请求 = %v, 期望只请求页面一次", f.urls())
	}
	if res.Name != "GreasyFork Inline Fixture" || res.SourceType != TypeGreasyFork {
		t.Fatalf("结果不符: %+v", res)
	}
}

// ── 页面无任何源码入口 → 报错（不允许 Code 为空的结果） ────────

func TestGreasyfork_页面无源码报错(t *testing.T) {
	pageURL := "https://greasyfork.org/scripts/1000-metabase"
	codeURL := "https://update.greasyfork.org/scripts/1000.user.js"
	pageHTML := mustFixture(t, "greasyfork/page_no_block.html")
	srcPageURL := "https://greasyfork.org/scripts/1000-metabase/code"
	f := newFake(map[string]fakeRoute{
		codeURL:    code(404),
		pageURL:    ok(pageHTML),
		srcPageURL: code(404),
	})
	res, err := (greasyforkAdapter{}).Fetch(context.Background(), f, pageURL)
	if err == nil {
		t.Fatal("无任何源码入口应报错（禁止返回 Code 为空的成功结果）")
	}
	if res != nil {
		t.Fatalf("失败不应返回结果: %+v", res)
	}
	msg := err.Error()
	for _, want := range []string{codeURL, pageURL, srcPageURL, "404", "页面无内嵌头块", "页面无安装直链"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误缺少 %q: %v", want, err)
		}
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
