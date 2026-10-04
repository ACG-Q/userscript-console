package sources

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func zoneFixture() string {
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "userscriptzone", "page_fixture.html"))
	if err != nil {
		panic("读取 userscriptzone fixture 失败: " + err.Error())
	}
	return string(b)
}

func TestZone_MatchURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"裸域", "https://userscript.zone/s/abc", true},
		{"www子域", "https://www.userscript.zone/s/abc", true},
		{"http协议", "http://userscript.zone/", true},
		{"前缀拼接绕过", "https://eviluserscript.zone/x", false},
		{"后缀挂域名绕过", "https://userscript.zone.evil.com/x", false},
		{"别的站", "https://greasyfork.org/scripts/1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (zoneAdapter{}).MatchURL(c.url); got != c.want {
				t.Fatalf("MatchURL(%q) = %v, 期望 %v", c.url, got, c.want)
			}
		})
	}
}

func TestZone_内嵌块成功(t *testing.T) {
	pageURL := "https://userscript.zone/s/demo"
	f := newFake(map[string]fakeRoute{pageURL: ok(zoneFixture())})
	a, err := Detect(pageURL)
	if err != nil || a.Type() != TypeZone {
		t.Fatalf("Detect = %v, %v", a, err)
	}
	res, err := a.Fetch(context.Background(), f, pageURL)
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	if res.Name != "Zone Fixture Script" || res.SourceType != TypeZone {
		t.Fatalf("元数据不符: %+v", res)
	}
	if !strings.Contains(res.Code, "==UserScript==") {
		t.Fatalf("Code 应含头标记, 实际前50字符 = %q", res.Code[:min(len(res.Code), 50)])
	}
}

func TestZone_无块报错(t *testing.T) {
	pageURL := "https://userscript.zone/s/no-header"
	f := newFake(map[string]fakeRoute{
		pageURL: ok(`<html><body><p>普通页面，没有元数据头。</p></body></html>`),
	})
	_, err := (zoneAdapter{}).Fetch(context.Background(), f, pageURL)
	if err == nil {
		t.Fatal("无内嵌块应报错")
	}
	if !strings.Contains(err.Error(), pageURL) || !strings.Contains(err.Error(), "==UserScript==") {
		t.Fatalf("错误应含 URL 与头 token 上下文: %v", err)
	}
}

func TestZone_非2xx报错(t *testing.T) {
	pageURL := "https://userscript.zone/s/gone"
	f := newFake(map[string]fakeRoute{pageURL: code(503)})
	_, err := (zoneAdapter{}).Fetch(context.Background(), f, pageURL)
	if err == nil {
		t.Fatal("非 2xx 应报错")
	}
	if !strings.Contains(err.Error(), pageURL) || !strings.Contains(err.Error(), "503") {
		t.Fatalf("错误应含 URL 与状态码: %v", err)
	}
}
