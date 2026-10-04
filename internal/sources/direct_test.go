package sources

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func directFixture() string {
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "direct", "example_user_script.user.js"))
	if err != nil {
		panic("读取 direct fixture 失败: " + err.Error())
	}
	return string(b)
}

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
	script := directFixture()
	f := newFake(map[string]fakeRoute{u: ok(script)})
	a, err := Detect(u)
	if err != nil {
		t.Fatalf("Detect 报错: %v", err)
	}
	res, err := a.Fetch(context.Background(), f, u)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	if res.Name != "GreasyFork Fixture Script" || res.SourceType != TypeDirect {
		t.Fatalf("元数据/类型不符: %+v / %q", res, res.SourceType)
	}
	if res.Code != script {
		t.Fatalf("Code 应与 fixture 原文一致")
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
