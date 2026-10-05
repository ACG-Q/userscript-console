package sources

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const gistAPIURL = "https://api.github.com/gists/deadbeef"

// mustReadFixture 读取 fixture 文件，失败时终止测试。
func mustReadFixture(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "gist", filepath.Base(rel)))
	if err != nil {
		t.Fatalf("读取 fixture %q 失败: %v", rel, err)
	}
	return string(b)
}

func TestGist_MatchURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"gist页面", "https://gist.github.com/user/deadbeef", true},
		{"gistAPI", "https://api.github.com/gists/deadbeef", true},
		{"www子域", "https://www.gist.github.com/user/deadbeef", true},
		{"后缀挂域名绕过", "https://gist.github.com.evil.com/user/x", false},
		{"前缀拼接绕过", "https://notgist.github.com/user/x", false},
		{"别的站", "https://github.com/user/repo", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (gistAdapter{}).MatchURL(c.url); got != c.want {
				t.Fatalf("MatchURL(%q) = %v, 期望 %v", c.url, got, c.want)
			}
		})
	}
}

func TestGist_ID推导两形态(t *testing.T) {
	cases := []struct {
		name   string
		url    string
		wantID string
		wantOK bool
	}{
		{"页面形态取第2段", "https://gist.github.com/alice/0d1b3c5d7e9f", "0d1b3c5d7e9f", true},
		{"页面形态带fragment", "https://gist.github.com/alice/0d1b3c5d7e9f#file-foo-js", "0d1b3c5d7e9f", true},
		{"API形态取gists后一段", "https://api.github.com/gists/0d1b3c5d7e9f", "0d1b3c5d7e9f", true},
		{"API形态带后缀路径", "https://api.github.com/gists/0d1b3c5d7e9f/comments", "0d1b3c5d7e9f", true},
		{"页面缺id段", "https://gist.github.com/alice", "", false},
		{"API缺id段", "https://api.github.com/gists", "", false},
		{"API非gists前缀", "https://api.github.com/users/alice", "", false},
		{"非白名单host", "https://evil.com/gists/abc", "", false},
		{"ftp协议", "ftp://gist.github.com/alice/abc", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, ok := gistID(c.url)
			if ok != c.wantOK || id != c.wantID {
				t.Fatalf("gistID(%q) = (%q, %v), 期望 (%q, %v)", c.url, id, ok, c.wantID, c.wantOK)
			}
		})
	}
}

func TestGist_userJS文件优先(t *testing.T) {
	gistJSON := mustReadFixture(t, "api_github_com_gists_testfixture123.json")
	f := newFake(map[string]fakeRoute{gistAPIURL: ok(gistJSON)})
	a, err := Detect("https://gist.github.com/alice/deadbeef")
	if err != nil || a.Type() != TypeGist {
		t.Fatalf("Detect = %v, %v", a, err)
	}
	res, err := a.Fetch(context.Background(), f, "https://gist.github.com/alice/deadbeef")
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	if !strings.Contains(res.Code, "Gist Fixture Script") {
		t.Fatalf("应选中 *.user.js 文件, 实际 Code 前50字符 = %q", res.Code[:min(len(res.Code), 50)])
	}
	if res.SourceType != TypeGist {
		t.Fatalf("SourceType = %q", res.SourceType)
	}
}

func TestGist_无userJS取键序首文件(t *testing.T) {
	gistJSON := mustReadFixture(t, "api_github_com_gists_plainfixture.json")
	f := newFake(map[string]fakeRoute{gistAPIURL: ok(gistJSON)})
	res, err := (gistAdapter{}).Fetch(context.Background(), f, "https://api.github.com/gists/deadbeef")
	if err != nil {
		t.Fatalf("Fetch 报错: %v", err)
	}
	// plainfixture 中文件按字母序：helper.js（h）< utils.js（u），取首个 helper.js
	if res.Code != "// Another plain JS file\nvar x = 1;\n" {
		t.Fatalf("无 .user.js 时应取键序首个 helper.js, 实际 Code = %q", res.Code)
	}
	if res.Name != "" {
		t.Fatalf("无头文件 Name 应为空, 实际 %q", res.Name)
	}
}

func TestGist_宽松策略CodeOnly不报错(t *testing.T) {
	// 使用一个内联 plain JSON 作为宽松策略测试（无头文件场景）
	gistPlain := `{"files":{"plain.js":{"filename":"plain.js","content":"console.log('no header here');"}}}`
	f := newFake(map[string]fakeRoute{gistAPIURL: ok(gistPlain)})
	res, err := (gistAdapter{}).Fetch(context.Background(), f, "https://gist.github.com/alice/deadbeef")
	if err != nil {
		t.Fatalf("头解析失败只应填 Code, 不应报错: %v", err)
	}
	if res.Code != "console.log('no header here');" {
		t.Fatalf("Code = %q", res.Code)
	}
	if res.Name != "" || res.Version != "" || res.Match != nil || res.Grant != nil {
		t.Fatalf("无头时元数据应保持零值: %+v", res)
	}
	if res.SourceType != TypeGist {
		t.Fatalf("SourceType = %q", res.SourceType)
	}
}

func TestGist_非2xx报错(t *testing.T) {
	f := newFake(map[string]fakeRoute{gistAPIURL: code(403)})
	_, err := (gistAdapter{}).Fetch(context.Background(), f, "https://gist.github.com/alice/deadbeef")
	if err == nil {
		t.Fatal("API 非 2xx 应报错")
	}
	if !strings.Contains(err.Error(), gistAPIURL) || !strings.Contains(err.Error(), "403") {
		t.Fatalf("错误应含 API URL 与状态码: %v", err)
	}
}

func TestGist_空files报错(t *testing.T) {
	f := newFake(map[string]fakeRoute{gistAPIURL: ok(`{"files":{}}`)})
	_, err := (gistAdapter{}).Fetch(context.Background(), f, "https://gist.github.com/alice/deadbeef")
	if err == nil {
		t.Fatal("gist 无文件应报错")
	}
	if !strings.Contains(err.Error(), "deadbeef") {
		t.Fatalf("错误应含 gist id 上下文: %v", err)
	}
}
