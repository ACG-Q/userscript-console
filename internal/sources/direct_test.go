package sources

import (
	"context"
	"strings"
	"testing"
)

const directScript = `// ==UserScript==
// @name 直连脚本
// @version 9.9.9
// @description 直连描述
// @author 直连作者
// @match *://*/*
// @grant none
// ==/UserScript==
document.title = "direct";
`

func TestDirect_MatchURL恒true(t *testing.T) {
	urls := []string{
		"https://example.com/a.user.js",
		"http://localhost:8080/x",
		"https://evil.com/path/greasyfork.org",
		"https://totally-unrelated.site/y",
	}
	for _, u := range urls {
		if !(direct{}).MatchURL(u) {
			t.Fatalf("direct.MatchURL(%q) 应恒为 true", u)
		}
	}
}

func TestDirect_成功(t *testing.T) {
	u := "https://example.com/raw/a.user.js"
	f := newFake(map[string]fakeRoute{u: ok(directScript)})
	a, err := Detect(u)
	if err != nil {
		t.Fatalf("Detect 报错: %v", err)
	}
	res, err := a.Fetch(context.Background(), f, u)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	if res.Name != "直连脚本" || res.Version != "9.9.9" ||
		res.Description != "直连描述" || res.Author != "直连作者" {
		t.Fatalf("元数据不符: %+v", res)
	}
	if len(res.Match) != 1 || res.Match[0] != "*://*/*" {
		t.Fatalf("Match = %v", res.Match)
	}
	if len(res.Grant) != 1 || res.Grant[0] != "none" {
		t.Fatalf("Grant = %v", res.Grant)
	}
	if res.Code != directScript || res.SourceType != TypeDirect {
		t.Fatalf("Code/SourceType 不符: %q / %q", res.Code, res.SourceType)
	}
}

func TestDirect_非用户脚本报错(t *testing.T) {
	u := "https://example.com/not-a-userscript.js"
	f := newFake(map[string]fakeRoute{
		u: ok(`<html><body>普通网页而已</body></html>`),
	})
	_, err := (direct{}).Fetch(context.Background(), f, u)
	if err == nil {
		t.Fatal("缺少 ==UserScript== 头应报错")
	}
	if !strings.Contains(err.Error(), "不是用户脚本") || !strings.Contains(err.Error(), u) {
		t.Fatalf("错误应含「不是用户脚本」与 URL: %v", err)
	}
}

func TestDirect_抓取失败带上下文(t *testing.T) {
	u := "https://example.com/gone.user.js"
	f := newFake(map[string]fakeRoute{u: code(404)})
	_, err := (direct{}).Fetch(context.Background(), f, u)
	if err == nil {
		t.Fatal("非 2xx 应报错")
	}
	if !strings.Contains(err.Error(), u) || !strings.Contains(err.Error(), "404") {
		t.Fatalf("错误缺 URL/状态上下文: %v", err)
	}
}
