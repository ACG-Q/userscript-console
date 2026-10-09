package pages

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acg-q/userscript-console/internal/registry"
)

// buildTestRegistry 构造测试用 registry。
func buildTestRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	return &registry.Registry{
		Schema: registry.SchemaVersion,
		Scripts: []registry.Script{
			{
				ID:          "self01",
				Type:        registry.TypeSelf,
				Name:        "测试脚本",
				Version:     "1.0.0",
				Description: "一个测试描述",
				Author:      "Tester",
				Namespace:   "https://test.example.com",
				Match:       []string{"*://*/*"},
				Grant:       []string{"none"},
				Enabled:     true,
				CreatedAt:   "2026-01-01T00:00:00Z",
				UpdatedAt:   "2026-10-05T00:00:00Z",
				Changelog: []registry.ChangelogEntry{
					{Version: "1.0.0", Date: "2026-10-05", Note: "初始版本"},
				},
				Discussions: []registry.DiscussionEntry{
					{Version: "1.0.0", Number: 1, NodeID: "D_test1", URL: "https://github.com/test/discussions/1", CreatedAt: "2026-10-05"},
				},
			},
			{
				ID:        "sync01",
				Type:      registry.TypeSynced,
				Name:      "同步脚本",
				Version:   "2.0.0",
				Author:    "SyncAuthor",
				Match:     []string{"*://example.com/*"},
				Grant:     []string{"GM.xmlHttpRequest"},
				Enabled:   false,
				CreatedAt: "2026-01-01T00:00:00Z",
				UpdatedAt: "2026-10-04T00:00:00Z",
			},
			{
				ID:        "del01",
				Type:      registry.TypeSelf,
				Name:      "已删除脚本",
				Version:   "0.5.0",
				Author:    "DelAuthor",
				Match:     []string{"*://*/*"},
				Grant:     []string{"none"},
				Enabled:   true,
				Deleted:   true,
				CreatedAt: "2026-01-01T00:00:00Z",
				UpdatedAt: "2026-10-03T00:00:00Z",
			},
		},
	}
}

func TestNormalizeOptions(t *testing.T) {
	var opts Options
	opts.normalize()
	if opts.Out != "dist" {
		t.Errorf("Out 默认值错误: %s", opts.Out)
	}
	if opts.Batch != 10 {
		t.Errorf("Batch 默认值错误: %d", opts.Batch)
	}
	if opts.CommandsPerPage != 5 {
		t.Errorf("CommandsPerPage 默认值错误: %d", opts.CommandsPerPage)
	}
}

func TestRenderMarkdown(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"简单文本", "hello", "hello"},
		{"粗体", "**bold**", "<strong>bold</strong>"},
		{"链接", "[link](http://example.com)", "href=\"http://example.com\""},
		{"XSS 脚本", "<script>alert(1)</script>", ""},
		{"XSS onerror", "<img onerror=alert(1) src=x>", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderMarkdown(tt.src)
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Errorf("RenderMarkdown(%q) = %q, want contain %q", tt.src, got, tt.want)
			}
			if tt.src == "<script>alert(1)</script>" && strings.Contains(got, "script") {
				t.Errorf("XSS 未过滤: %s", got)
			}
		})
	}
}

func TestBuildBasic(t *testing.T) {
	reg := buildTestRegistry(t)
	opts := Options{
		Out:       t.TempDir(),
		Now:       time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
		PagesBase: "https://test.github.io/repo",
	}
	data := Data{
		Discussions: map[string]Thread{
			"D_test1": {Comments: []Comment{{Author: "User1", Body: "好脚本！", CreatedAt: "2026-10-05"}}},
		},
	}
	out, err := Build(reg, opts, data)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if out.Pages <= 0 {
		t.Errorf("Pages 应 > 0, got %d", out.Pages)
	}
	if out.IndexHTML == "" {
		t.Error("IndexHTML 不应为空")
	}
	if out.ScriptsJSON == "" {
		t.Error("ScriptsJSON 不应为空")
	}
	if len(out.DetailHTMLs) != 3 { // 2 active + 1 deleted
		t.Errorf("DetailHTMLs 应为 3, got %d", len(out.DetailHTMLs))
	}
}

func TestScriptsJSONFormat(t *testing.T) {
	reg := buildTestRegistry(t)
	opts := Options{Out: t.TempDir(), PagesBase: "https://test.github.io/repo"}
	out, err := Build(reg, opts, Data{})
	if err != nil {
		t.Fatal(err)
	}
	var cards []struct {
		Type string `json:"type"`
		HTML string `json:"html"`
	}
	if err := json.Unmarshal([]byte(out.ScriptsJSON), &cards); err != nil {
		t.Fatalf("scripts.json 解析失败: %v", err)
	}
	if len(cards) != 2 { // 仅活跃脚本
		t.Errorf("cards 长度应为 2, got %d", len(cards))
	}
	for _, c := range cards {
		if !strings.Contains(c.HTML, "script-card") {
			t.Errorf("html 应含 script-card 结构: %s", c.HTML)
		}
	}
	if strings.Contains(out.ScriptsJSON, "&lt;") {
		t.Error("scripts.json 不应转义 HTML")
	}
	if strings.HasSuffix(out.ScriptsJSON, "\n") {
		t.Error("scripts.json 不应有结尾换行")
	}
}

func TestDetailPageContent(t *testing.T) {
	reg := buildTestRegistry(t)
	opts := Options{Out: t.TempDir(), PagesBase: "https://test.github.io/repo"}
	data := Data{
		Discussions: map[string]Thread{
			"D_test1": {Comments: []Comment{{Author: "User1", Body: "好脚本！", CreatedAt: "2026-10-05"}}},
		},
	}
	out, err := Build(reg, opts, data)
	if err != nil {
		t.Fatal(err)
	}
	active := out.DetailHTMLs["self01"]
	for _, want := range []string{"detail-card", "安装脚本", "Tester", "更新历史", "id=\"discData\"", "cmt"} {
		if !strings.Contains(active, want) {
			t.Errorf("详情页 self01 缺少 %q", want)
		}
	}
	tomb := out.DetailHTMLs["del01"]
	if !strings.Contains(tomb, "已下架") {
		t.Errorf("墓碑页应含 已下架 标记:\n%s", tomb)
	}
	if strings.Contains(tomb, "安装脚本") {
		t.Errorf("墓碑页不应有安装按钮")
	}
}

func TestBuildWarnings(t *testing.T) {
	reg := buildTestRegistry(t)
	opts := Options{Out: t.TempDir(), PagesBase: "https://test.github.io/repo"}
	// 不提供 Discussions → 应产生 W1 警告
	out, err := Build(reg, opts, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.BuildWarnings) == 0 {
		t.Error("缺少 Discussions 应产生 W1 警告")
	}
}

// TestTemplatesEmbedded 全部模板（含 assets/page-shell）在 embedFS 中可解析。
func TestTemplatesEmbedded(t *testing.T) {
	entries, err := fs.ReadDir(embedFS, "templates")
	if err != nil {
		t.Fatalf("读取 templates 目录失败: %v", err)
	}
	found := map[string]bool{}
	for _, e := range entries {
		found[e.Name()] = true
	}
	for _, want := range []string{
		"assets.tmpl", "index.tmpl", "detail.tmpl",
		"commands.tmpl", "commands-index.tmpl", "docs.tmpl",
	} {
		if !found[want] {
			t.Errorf("模板 %s 不存在", want)
		}
	}
	if _, err := newRenderer("", false); err != nil {
		t.Errorf("全集模板解析失败: %v", err)
	}
}

func TestBuildIdempotent(t *testing.T) {
	reg := buildTestRegistry(t)
	opts := Options{
		Out:       t.TempDir(),
		Now:       time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
		PagesBase: "https://test.github.io/repo",
	}
	data := Data{}

	out1, err := Build(reg, opts, data)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := Build(reg, opts, data)
	if err != nil {
		t.Fatal(err)
	}

	if out1.IndexHTML != out2.IndexHTML {
		t.Error("index.html 应幂等")
	}
	if out1.ScriptsJSON != out2.ScriptsJSON {
		t.Error("scripts.json 应幂等")
	}
	for id := range out1.DetailHTMLs {
		if out1.DetailHTMLs[id] != out2.DetailHTMLs[id] {
			t.Errorf("detail %s 应幂等", id)
		}
	}
}

func TestFilterActive(t *testing.T) {
	scripts := []registry.Script{
		{ID: "a", Deleted: false},
		{ID: "b", Deleted: true},
		{ID: "c", Deleted: false},
	}
	active := filterActive(scripts)
	if len(active) != 2 {
		t.Errorf("filterActive 应为 2, got %d", len(active))
	}
	for _, s := range active {
		if s.Deleted {
			t.Errorf("filterActive 应排除已删除: %s", s.ID)
		}
	}
}

func TestEscHTML(t *testing.T) {
	if got := esc("<script>alert(1)</script>"); got != "&lt;script&gt;alert(1)&lt;/script&gt;" {
		t.Errorf("esc = %q, 期望 HTML 转义", got)
	}
	if got := esc("<>&\"'"); got != "&lt;&gt;&amp;&#34;&#39;" {
		t.Errorf("esc = %q, 期望完整 HTML 转义", got)
	}
}

func TestBuildCommandPages(t *testing.T) {
	reg := buildTestRegistry(t)
	opts := Options{
		Out:             t.TempDir(),
		Now:             time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
		PagesBase:       "https://test.github.io/repo",
		CommandsPerPage: 3,
	}
	out, err := Build(reg, opts, Data{})
	if err != nil {
		t.Fatal(err)
	}
	// 命令分页可能因 archive 不存在而为空，但不应 panic
	_ = out.CommandsIndex
}

func TestBuildWithDiscussions(t *testing.T) {
	reg := buildTestRegistry(t)
	opts := Options{
		Out:       t.TempDir(),
		Now:       time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
		PagesBase: "https://test.github.io/repo",
	}
	data := Data{
		Discussions: map[string]Thread{},
	}
	out, err := Build(reg, opts, data)
	if err != nil {
		t.Fatal(err)
	}
	// 有 Discussions 时不应有 W1 警告
	for _, w := range out.BuildWarnings {
		if strings.Contains(w, "W1") {
			t.Error("有 Discussions 时不应产生 W1 警告")
		}
	}
}

func TestBuildEmptyRegistry(t *testing.T) {
	reg := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{}}
	opts := Options{Out: t.TempDir(), PagesBase: "https://test.github.io/repo"}
	out, err := Build(reg, opts, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if out.IndexHTML == "" {
		t.Error("空 registry 也应生成 index.html")
	}
	if len(out.DetailHTMLs) != 0 {
		t.Errorf("空 registry 不应有详情页, got %d", len(out.DetailHTMLs))
	}
}

func TestRenderTombstone(t *testing.T) {
	s := registry.Script{
		ID:        "tomb01",
		Type:      registry.TypeSelf,
		Name:      "已删脚本",
		Version:   "0.1.0",
		Enabled:   true,
		Deleted:   true,
		Match:     []string{},
		Grant:     []string{},
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}
	r, err := newRenderer("test/repo", false)
	if err != nil {
		t.Fatal(err)
	}
	html, err := r.renderDetail(s, Options{PagesBase: "https://test.github.io/repo", Now: time.Now()}, Data{})
	if err != nil {
		t.Fatalf("renderDetail 失败: %v", err)
	}
	if !strings.Contains(html, "已删脚本") {
		t.Errorf("html 应含脚本名")
	}
	if !strings.Contains(html, "已下架") {
		t.Errorf("墓碑页应含 已下架 标记")
	}
}

// isolatedOut 返回隔离输出根（<tmp>/dist），archive/docs 落在同一 <tmp> 下，避免用例间互相污染。
func isolatedOut(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	return out
}

// writeCommandsArchive 在 opts.Out 的父目录写 archive/commands.json（renderCommands 探测路径约定）。
func writeCommandsArchive(t *testing.T, out, archiveJSON string) {
	t.Helper()
	archiveDir := filepath.Join(filepath.Dir(filepath.Clean(out)), "archive")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "commands.json"), []byte(archiveJSON), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRenderCommandPagesWithArchive(t *testing.T) {
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{
		"schema": 1,
		"commands": [
			{
				"command": "add",
				"author": "user1",
				"created_at": "2026-10-01T00:00:00Z",
				"results": [
					{"id": "c1", "author": "user1", "body": "/add https://example.com/a.js", "created_at": "2026-10-01T00:00:00Z"}
				]
			}
		]
	}`)

	r, err := newRenderer("test/repo", false)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Out:             outDir,
		Now:             time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
		PagesBase:       "https://test.github.io/repo",
		CommandsPerPage: 5,
	}
	cp, idx, err := r.renderCommands(opts)
	if err != nil {
		t.Fatalf("renderCommands 失败: %v", err)
	}
	if len(cp) == 0 {
		t.Error("应有命令分页")
	}
	if idx == "" {
		t.Error("commands/index.html 不应为空")
	}
}

func TestRenderMarkdownEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"空字符串", "", ""},
		{"纯文本", "hello world", "hello world"},
		{"HTML 标签", "<div>test</div>", "<div>test</div>"},
		{"换行", "line1\nline2", "line1\nline2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderMarkdown(tt.src)
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Errorf("RenderMarkdown(%q) = %q, want contain %q", tt.src, got, tt.want)
			}
		})
	}
}

func TestRenderCommandsBadArchive(t *testing.T) {
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, "not json")
	r, err := newRenderer("test/repo", false)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Out: outDir, PagesBase: "https://test.github.io/repo"}
	if _, _, err := r.renderCommands(opts); err == nil {
		t.Fatal("renderCommands 坏 JSON 应返回 error")
	}
}

func TestBuildWithCommandsArchive(t *testing.T) {
	reg := buildTestRegistry(t)
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[{"command":"add","author":"u","created_at":"2026-01-01T00:00:00Z","results":[{"id":"r1","author":"u","body":"/add url","created_at":"2026-01-01T00:00:00Z"}]}]}`)
	opts := Options{
		Out:             outDir,
		Now:             time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
		PagesBase:       "https://test.github.io/repo",
		CommandsPerPage: 3,
	}
	out, err := Build(reg, opts, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.CommandPages) == 0 {
		t.Error("有归档时应生成命令页")
	}
}

func TestFilterActiveEmpty(t *testing.T) {
	result := filterActive(nil)
	if len(result) != 0 {
		t.Errorf("filterActive(nil) = %d, want 0", len(result))
	}
}

// TestRenderPageBodyNotFound 不存在的 body define 应返回 error。
func TestRenderPageBodyNotFound(t *testing.T) {
	r, err := newRenderer("", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.renderPage("x", "nonexistent_template_xyz", nil, nil, "index.html"); err == nil {
		t.Fatal("渲染不存在的模板应返回 error")
	}
}

func TestRenderMarkdownErrorPath(t *testing.T) {
	// RenderMarkdown 内部用 goldmark；输入为纯字符串时不会触发错误。
	// 这里验证的是正常渲染路径输出包含内容。
	got := RenderMarkdown("# 标题\n\n正文")
	if !strings.Contains(got, "标题") {
		t.Errorf("RenderMarkdown 应渲染标题: %s", got)
	}
}

// TestBuildLinksAreRelative 站点内链必须是相对路径，Pages 子路径部署不得 404（CUTOVER §3）。
func TestBuildLinksAreRelative(t *testing.T) {
	reg := buildTestRegistry(t)
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[{"command":"add","author":"u","created_at":"2026-10-01T00:00:00Z","results":[{"id":"r1","author":"u","body":"/add url","created_at":"2026-10-01T00:00:00Z"}]}]}`)

	out, err := Build(reg, Options{
		Out: outDir, PagesBase: "https://test.github.io/repo",
		Now: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
	}, Data{Discussions: map[string]Thread{}})
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}

	cases := []struct{ name, html, want, forbid string }{
		{"首页脚本卡", out.IndexHTML, `href="scripts/`, `href="/scripts/`},
		{"首页命令归档导航", out.IndexHTML, `href="commands/page-1.html"`, `href="/commands`},
		{"详情页返回首页", out.DetailHTMLs["self01"], `href="../index.html"`, `href="/index.html"`},
		{"命令页返回列表", out.CommandPages[1], `href="../index.html"`, `href="/index.html"`},
		{"命令索引分页链接", out.CommandsIndex, `href="page-1.html"`, `/commands/?cmd=`},
	}
	for _, c := range cases {
		if !strings.Contains(c.html, c.want) {
			t.Errorf("%s 应含 %q", c.name, c.want)
		}
		if strings.Contains(c.html, c.forbid) {
			t.Errorf("%s 不应含 %q", c.name, c.forbid)
		}
	}
}

// TestRenderMarkdownSanitizesXSS XSS 用例硬验收（规格 D-04/DR-6）：危险载荷必须被消毒。
func TestRenderMarkdownSanitizesXSS(t *testing.T) {
	cases := []string{
		`<script>alert(1)</script>ok`,
		`<img src=x onerror="alert(1)">`,
		`<svg onload=alert(1)></svg>`,
		`<iframe src="https://evil"></iframe>`,
		`[x](javascript:alert(1))`,
		`<a href="javascript:alert(1)">x</a>`,
		`<object data="e"></object><embed src="e">`,
		`<form action="e"><input name="a"></form>`,
	}
	for _, src := range cases {
		out := RenderMarkdown(src)
		low := strings.ToLower(out)
		for _, bad := range []string{"<script", "onerror=", "onload=", "<iframe", "javascript:", "<object", "<embed", "<form"} {
			if strings.Contains(low, bad) {
				t.Errorf("源 %q 未被消毒，输出含 %q:\n%s", src, bad, out)
			}
		}
	}
}

// TestRenderMarkdownKeepsSafeHTML 消毒不得误伤安全标记。
func TestRenderMarkdownKeepsSafeHTML(t *testing.T) {
	out := RenderMarkdown("**粗体** [链接](https://example.com) `code`")
	for _, want := range []string{"<strong>", "href=\"https://example.com\"", "<code>"} {
		if !strings.Contains(out, want) {
			t.Errorf("安全标记丢失 %q:\n%s", want, out)
		}
	}
}

// TestCommentBodyPlainTextAndClipped 评论正文是纯文本（渲染处转义一次）且截断至 400 字（I9）。
func TestCommentBodyPlainTextAndClipped(t *testing.T) {
	reg := buildTestRegistry(t)
	opts := Options{Out: t.TempDir(), PagesBase: "https://test.github.io/repo"}
	data := Data{
		Discussions: map[string]Thread{
			"D_test1": {Comments: []Comment{{Author: "User1", Body: "x <b>y</b>", CreatedAt: "2026-10-05"}}},
		},
	}
	out, err := Build(reg, opts, data)
	if err != nil {
		t.Fatal(err)
	}
	joined := out.DetailHTMLs["self01"]
	if !strings.Contains(joined, "x &lt;b&gt;y&lt;/b&gt;") {
		t.Errorf("评论纯文本应被转义一次: 缺 x &lt;b&gt;y&lt;/b&gt;")
	}
	if strings.Contains(joined, "<b>y</b>") {
		t.Errorf("评论正文不应渲染为 HTML")
	}

	// 超 400 字正文须截断并以省略号结尾
	long := strings.Repeat("a", 500)
	data = Data{
		Discussions: map[string]Thread{
			"D_test1": {Comments: []Comment{{Author: "User1", Body: long, CreatedAt: "2026-10-05"}}},
		},
	}
	out, err = Build(reg, opts, data)
	if err != nil {
		t.Fatal(err)
	}
	joined = out.DetailHTMLs["self01"]
	if strings.Contains(joined, strings.Repeat("a", 401)) {
		t.Errorf("评论正文未截断到 400 字")
	}
	if !strings.Contains(joined, strings.Repeat("a", 400)+"…") {
		t.Errorf("截断后应以 400 字 + 省略号结尾")
	}
}

// TestIndexJSGuardsMissingLoadMore Total≤Batch 时 list-js 根本不注入，
// loadMore 元素也不渲染（I10）；注入时 JS 必须有 null 守卫。
func TestIndexJSGuardsMissingLoadMore(t *testing.T) {
	reg := buildTestRegistry(t) // 2 活跃 ≤ 默认 batch 10
	opts := Options{Out: t.TempDir(), PagesBase: "https://test.github.io/repo", Batch: 10}
	out, err := Build(reg, opts, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.IndexHTML, "id=\"loadMore\"") {
		t.Errorf("Total≤Batch 不应渲染 loadMore")
	}
	if strings.Contains(out.IndexHTML, "getElementById('scriptList')") {
		t.Errorf("Total≤Batch 不应注入 list-js")
	}

	// Total>Batch：list-js 注入且带守卫
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[]}`)
	out2, err := Build(manyScriptsRegistry(12), Options{Out: outDir, PagesBase: "https://test.github.io/repo", Batch: 5}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2.IndexHTML, "id=\"loadMore\"") || !strings.Contains(out2.IndexHTML, "id=\"listSentinel\"") {
		t.Errorf("Total>Batch 应渲染 loadMore 与 sentinel")
	}
	if !strings.Contains(out2.IndexHTML, "if (!list) return;") ||
		!strings.Contains(out2.IndexHTML, "if (more) more.addEventListener") {
		t.Errorf("list-js 缺少 null 守卫:\n%s", out2.IndexHTML)
	}
}

// manyScriptsRegistry n 个活跃脚本。
func manyScriptsRegistry(n int) *registry.Registry {
	reg := &registry.Registry{Schema: registry.SchemaVersion}
	for i := 0; i < n; i++ {
		reg.Scripts = append(reg.Scripts, registry.Script{
			ID:        fmt.Sprintf("s%02d", i),
			Type:      registry.TypeSelf,
			Name:      fmt.Sprintf("脚本%02d", i),
			Version:   "1.0.0",
			Enabled:   true,
			Match:     []string{"*://*/*"},
			Grant:     []string{"none"},
			CreatedAt: "2026-01-01T00:00:00Z",
			UpdatedAt: fmt.Sprintf("2026-10-%02dT00:00:00Z", i+1),
		})
	}
	return reg
}

// TestIndexBatchInitialHide 服务端只渲染前 batch 张卡（贴 projec-02 shown），
// 余量由 list-js 从 scripts.json 续载（I7④）。
func TestIndexBatchInitialHide(t *testing.T) {
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[]}`)
	out, err := Build(manyScriptsRegistry(12), Options{Out: outDir, PagesBase: "https://t.example/x", Batch: 5}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(out.IndexHTML, `class="script-card"`); got != 5 {
		t.Errorf("index 应只渲染前 5 张卡, got %d", got)
	}
	if !strings.Contains(out.IndexHTML, `data-batch="5"`) || !strings.Contains(out.IndexHTML, `data-total="12"`) {
		t.Errorf("scriptList 应携带 data-batch/data-total")
	}
	// scripts.json 全量
	var items []map[string]any
	if err := json.Unmarshal([]byte(out.ScriptsJSON), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 12 {
		t.Errorf("scripts.json 应含全量 12 条, got %d", len(items))
	}
}

// TestCommandsGlobalPagination 全局倒序整组分页：12 组 / 每页 5 → 3 页（对齐 build_pages.py，I7①）。
func TestCommandsGlobalPagination(t *testing.T) {
	reg := buildTestRegistry(t)
	outDir := isolatedOut(t)
	var groups []string
	for i := 1; i <= 12; i++ {
		created := fmt.Sprintf("2026-01-%02dT00:00:00Z", i)
		groups = append(groups, fmt.Sprintf(
			`{"command":"cmd%02d","author":"u","created_at":"%s","results":[{"id":"r%02d","author":"u","body":"mark-%02d","created_at":"%s"}]}`,
			i, created, i, i, created))
	}
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[`+strings.Join(groups, ",")+`]}`)

	out, err := Build(reg, Options{Out: outDir, PagesBase: "https://test.github.io/repo", CommandsPerPage: 5}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.CommandPages) != 3 {
		t.Fatalf("12 组 / 每页 5 应产 3 页, got %d", len(out.CommandPages))
	}
	for _, p := range []int{1, 2, 3} {
		if out.CommandPages[p] == "" {
			t.Errorf("page-%d 不应为空", p)
		}
	}
	if !strings.Contains(out.CommandPages[1], "mark-12") {
		t.Errorf("page1 应含最新组（cmd12）")
	}
	if strings.Contains(out.CommandPages[1], "mark-01") {
		t.Errorf("page1 不应含最旧组（cmd01）")
	}
	if !strings.Contains(out.CommandPages[3], "mark-01") {
		t.Errorf("page3 应含最旧组（cmd01）")
	}
	if !strings.Contains(out.CommandPages[1], "第 1 / 3 页") {
		t.Errorf("page1 应显示 第 1 / 3 页:\n%s", out.CommandPages[1])
	}
	// 序号每页从 1 起
	if !strings.Contains(out.CommandPages[2], "#1") {
		t.Errorf("page2 序号应从 #1 重新开始")
	}
}

// TestCommandsIndexMetaRefresh commands/index.html 必须是 meta-refresh 跳转页（I7②）。
func TestCommandsIndexMetaRefresh(t *testing.T) {
	reg := buildTestRegistry(t)
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[{"command":"add","author":"u","created_at":"2026-01-01T00:00:00Z","results":[{"id":"r1","author":"u","body":"/add x","created_at":"2026-01-01T00:00:00Z"}]}]}`)
	out, err := Build(reg, Options{Out: outDir, PagesBase: "https://test.github.io/repo"}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.CommandsIndex, `<meta http-equiv="refresh"`) ||
		!strings.Contains(out.CommandsIndex, "url=page-1.html") {
		t.Errorf("commands/index.html 应为 meta-refresh 跳转页:\n%s", out.CommandsIndex)
	}
}

// TestArchiveNavLinks 首页与详情页均须有归档导航（相对路径，I7③）。
func TestArchiveNavLinks(t *testing.T) {
	reg := buildTestRegistry(t)
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[{"command":"add","author":"u","created_at":"2026-01-01T00:00:00Z","results":[{"id":"r1","author":"u","body":"/add x","created_at":"2026-10-01T00:00:00Z"}]}]}`)
	out, err := Build(reg, Options{Out: outDir, PagesBase: "https://test.github.io/repo"}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.IndexHTML, `href="commands/page-1.html"`) {
		t.Errorf("首页应含归档导航 href=commands/page-1.html")
	}
	if !strings.Contains(out.DetailHTMLs["self01"], `href="../commands/page-1.html"`) {
		t.Errorf("详情页应含归档导航 href=../commands/page-1.html")
	}
}

// ── 新 UI 合同 ───────────────────────────────────────────────

// TestIndexHeroAndFilters 首页 hero 三统计卡与筛选 chips（设计 §2）。
func TestIndexHeroAndFilters(t *testing.T) {
	reg := buildTestRegistry(t)
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[]}`)
	data := Data{Discussions: map[string]Thread{
		"D_test1": {
			Comments: []Comment{
				{Author: "a", Body: "1", CreatedAt: "2026-10-05"},
				{Author: "b", Body: "2", CreatedAt: "2026-10-05"},
			},
			HasAnswer: true,
		},
	}}
	out, err := Build(reg, Options{Out: outDir, PagesBase: "https://test.github.io/repo", Now: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)}, data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"class=\"hero\"", "脚本总数", "讨论回复", "已解决反馈",
		`data-filter="all"`, `data-filter="self"`, `data-filter="synced"`,
		`id="scriptList"`, "filter-empty", "没有符合筛选条件的脚本",
	} {
		if !strings.Contains(out.IndexHTML, want) {
			t.Errorf("index 缺少 %q", want)
		}
	}
	if !strings.Contains(out.IndexHTML, "<b>2</b><span>讨论回复</span>") {
		t.Errorf("讨论回复应为 2（Σ 评论数）")
	}
	if !strings.Contains(out.IndexHTML, "<b>1</b><span>已解决反馈</span>") {
		t.Errorf("已解决反馈应为 1")
	}
}

// TestIndexHeroDegraded 降级（Data{}）时统计显示占位符「—」。
func TestIndexHeroDegraded(t *testing.T) {
	reg := buildTestRegistry(t)
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[]}`)
	out, err := Build(reg, Options{Out: outDir, PagesBase: "https://test.github.io/repo"}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.IndexHTML, "<b>—</b><span>讨论回复</span>") ||
		!strings.Contains(out.IndexHTML, "<b>—</b><span>已解决反馈</span>") {
		t.Errorf("降级时统计应为占位符 —:\n%s", out.IndexHTML)
	}
}

// TestCardDiscThreeStates 卡片讨论面板三态（设计 §2 card-disc）。
func TestCardDiscThreeStates(t *testing.T) {
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[]}`)
	opts := Options{Out: outDir, PagesBase: "https://test.github.io/repo", Now: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)}

	// Kind 0：无讨论无 Issue
	reg0 := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{{
		ID: "bare", Type: registry.TypeSelf, Name: "裸脚本", Version: "1.0.0", Enabled: true,
		Match: []string{"*://*/*"}, Grant: []string{"none"},
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-10-05T00:00:00Z",
	}}}
	out0, err := Build(reg0, opts, Data{Discussions: map[string]Thread{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out0.IndexHTML, "还没有讨论") || !strings.Contains(out0.IndexHTML, "发起讨论 →") {
		t.Errorf("Kind0 应渲染 还没有讨论 + 发起讨论")
	}
	// html/template 会把 href 中的 + 转义为 &#43;
	if !strings.Contains(out0.IndexHTML, "issues?q=is%3Aissue&#43;label%3Ascript") {
		t.Errorf("Kind0 链接应指向 Issue 列表")
	}

	// Kind 1：有 Issue 但抓取全灭
	reg1 := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{{
		ID: "iss", Type: registry.TypeSelf, Name: "有Issue", Version: "1.0.0", Enabled: true,
		Match: []string{"*://*/*"}, Grant: []string{"none"},
		Issue: &registry.IssueRef{Number: 7, NodeID: "I7", URL: "https://github.com/test/repo/issues/7"},
		Discussions: []registry.DiscussionEntry{
			{Version: "1.0.0", Number: 3, NodeID: "D_x", URL: "https://github.com/test/repo/discussions/3", CreatedAt: "2026-10-05"},
		},
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-10-05T00:00:00Z",
	}}}
	out1, err := Build(reg1, opts, Data{Discussions: map[string]Thread{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out1.IndexHTML, "摘要暂不可用") || !strings.Contains(out1.IndexHTML, "在 GitHub 打开 →") {
		t.Errorf("Kind1 应渲染 摘要暂不可用 + 在 GitHub 打开")
	}
	if !strings.Contains(out1.IndexHTML, "https://github.com/test/repo/issues/7") {
		t.Errorf("Kind1 应链到 Issue")
	}

	// Kind 2：有数据
	reg2 := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{{
		ID: "live", Type: registry.TypeSelf, Name: "有讨论", Version: "1.0.0", Enabled: true,
		Match: []string{"*://*/*"}, Grant: []string{"none"},
		Discussions: []registry.DiscussionEntry{
			{Version: "1.0.0", Number: 9, NodeID: "D_live", URL: "https://github.com/test/repo/discussions/9", CreatedAt: "2026-10-05"},
		},
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-10-05T00:00:00Z",
	}}}
	out2, err := Build(reg2, opts, Data{Discussions: map[string]Thread{
		"D_live": {
			Comments:  []Comment{{Author: "alice", Body: "这个脚本太棒了", CreatedAt: "2026-10-05T00:00:00Z"}},
			HasAnswer: true,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2.IndexHTML, "已解决") || !strings.Contains(out2.IndexHTML, "1 条回复") {
		t.Errorf("Kind2 应渲染 issue-badges")
	}
	if !strings.Contains(out2.IndexHTML, "这个脚本太棒了") || !strings.Contains(out2.IndexHTML, "alice") {
		t.Errorf("Kind2 应渲染最新评论引言")
	}
}

// TestDetailDiscJSONPayload #discData 载荷：结构完整 + JSON 转义防 </script> 注入（I11）。
func TestDetailDiscJSONPayload(t *testing.T) {
	reg := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{{
		ID: "p01", Type: registry.TypeSelf, Name: "载荷", Version: "2.0.0", Enabled: true,
		Match: []string{"*://*/*"}, Grant: []string{"none"},
		Discussions: []registry.DiscussionEntry{
			{Version: "1.0.0", Number: 1, NodeID: "D_old", URL: "https://github.com/test/repo/discussions/1", CreatedAt: "2026-10-01"},
			{Version: "2.0.0", Number: 2, NodeID: "D_new", URL: "https://github.com/test/repo/discussions/2", CreatedAt: "2026-10-05"},
		},
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-10-05T00:00:00Z",
	}}}
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[]}`)
	out, err := Build(reg, Options{Out: outDir, PagesBase: "https://test.github.io/repo", Now: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)}, Data{Discussions: map[string]Thread{
		"D_old": {Comments: []Comment{{Author: "u", Body: "</script><script>alert(1)</script>", CreatedAt: "2026-10-01T00:00:00Z"}}},
		"D_new": {Comments: []Comment{{Author: "v", Body: "新版说明", CreatedAt: "2026-10-05T00:00:00Z"}}, HasAnswer: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	html := out.DetailHTMLs["p01"]
	if !strings.Contains(html, `id="discData"`) {
		t.Fatalf("详情页应含 discData")
	}
	// 载荷是合法 JSON 数组（新→旧）
	start := strings.Index(html, `id="discData">`) + len(`id="discData">`)
	end := strings.Index(html[start:], "</script>")
	var payload []discPayloadPost
	if err := json.Unmarshal([]byte(html[start:start+end]), &payload); err != nil {
		t.Fatalf("discData 非法 JSON: %v\n%s", err, html[start:start+end])
	}
	if len(payload) != 2 || payload[0].Version != "2.0.0" || payload[1].Version != "1.0.0" {
		t.Errorf("载荷应新→旧: %+v", payload)
	}
	if !payload[0].IsAnswered || payload[0].ReplyCount != 1 {
		t.Errorf("最新帖应 is_answered=true reply_count=1: %+v", payload[0])
	}
	// json.Marshal 已转义 < → 载荷内不得出现原始 </script>
	if strings.Contains(html[start:start+end], "</script>") {
		t.Errorf("载荷内不应出现原始 </script>（防注入）")
	}
	// 服务端首帖渲染（v2 最新）
	if !strings.Contains(html, "v2.0.0（最新）") || !strings.Contains(html, "新版说明") {
		t.Errorf("服务端应默认渲染最新版本帖")
	}
}

// TestDetailFallbackPanel 详情页无帖时的回退面板（设计 §2 详情 fallback）。
func TestDetailFallbackPanel(t *testing.T) {
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[]}`)
	opts := Options{Out: outDir, PagesBase: "https://test.github.io/repo"}

	// 无 Issue 无讨论 → 还没有讨论
	reg0 := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{{
		ID: "b0", Type: registry.TypeSelf, Name: "裸", Version: "1.0.0", Enabled: true,
		Match: []string{"*://*/*"}, Grant: []string{"none"},
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-10-05T00:00:00Z",
	}}}
	out0, err := Build(reg0, opts, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out0.DetailHTMLs["b0"], "还没有讨论") {
		t.Errorf("无链接时应渲染 还没有讨论")
	}

	// 有 Issue → 摘要暂不可用 + 链接
	reg1 := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{{
		ID: "b1", Type: registry.TypeSelf, Name: "有Issue", Version: "1.0.0", Enabled: true,
		Match: []string{"*://*/*"}, Grant: []string{"none"},
		Issue:     &registry.IssueRef{Number: 5, NodeID: "I5", URL: "https://github.com/test/repo/issues/5"},
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-10-05T00:00:00Z",
	}}}
	out1, err := Build(reg1, opts, Data{})
	if err != nil {
		t.Fatal(err)
	}
	html := out1.DetailHTMLs["b1"]
	if !strings.Contains(html, "摘要暂不可用") || !strings.Contains(html, "https://github.com/test/repo/issues/5") {
		t.Errorf("有链接时应渲染 摘要暂不可用: %s", html)
	}
}

// TestEmptyStates 空 registry 首页空态文案（设计 §2）。
func TestEmptyStates(t *testing.T) {
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[]}`)
	out, err := Build(&registry.Registry{Schema: registry.SchemaVersion}, Options{Out: outDir, PagesBase: "https://test.github.io/repo"}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.IndexHTML, "暂无脚本，请在命令面板 Issue #1 中使用 /add 添加。") {
		t.Errorf("首页空态文案缺失")
	}
	if strings.Contains(out.IndexHTML, `id="filter-empty"`) {
		t.Errorf("无卡片时不应渲染 filter-empty")
	}
}

// TestShellRepoDerived Repo 从 PagesBase 推导；GitHub/管理入口随之显隐。
func TestShellRepoDerived(t *testing.T) {
	reg := buildTestRegistry(t)
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[]}`)

	out, err := Build(reg, Options{Out: outDir, PagesBase: "https://test.github.io/repo"}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.IndexHTML, "https://github.com/test/repo") {
		t.Errorf("应渲染 GitHub 仓库链接")
	}
	if !strings.Contains(out.IndexHTML, "管理入口 · Issue #1") {
		t.Errorf("应渲染管理入口 Issue #1")
	}

	out2, err := Build(reg, Options{Out: t.TempDir()}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out2.IndexHTML, "GitHub 仓库") {
		t.Errorf("无 PagesBase 不应渲染 GitHub 链接")
	}
	if strings.Contains(out2.IndexHTML, "管理入口") {
		t.Errorf("无 PagesBase 不应渲染管理入口")
	}
}

// ── 纯函数单测 ───────────────────────────────────────────────

func TestKebab(t *testing.T) {
	cases := map[string]string{
		"SPEC-DATA-MODEL": "spec-data-model",
		"index":           "index",
		"中文文档":            "中文文档",
		"a  b":            "a-b",
		"--x--":           "x",
		"":                "",
	}
	for in, want := range cases {
		if got := kebab(in); got != want {
			t.Errorf("kebab(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDocTitle(t *testing.T) {
	if got := docTitle("# 主标题\n\n正文", "fallback"); got != "主标题" {
		t.Errorf("docTitle 首个 H1: %q", got)
	}
	if got := docTitle("没有标题", "fallback"); got != "fallback" {
		t.Errorf("docTitle 回退: %q", got)
	}
	if got := docTitle("  ## 二级不算\n正文", "fallback"); got != "fallback" {
		t.Errorf("docTitle 只认一级标题: %q", got)
	}
}

func TestSafeURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/a/b": "https://github.com/a/b",
		"http://example.com":     "http://example.com",
		"javascript:alert(1)":    "",
		"data:text/html,x":       "",
		"//evil.com":             "",
		"":                       "",
		"/relative/path":         "",
	}
	for in, want := range cases {
		if got := safeURL(in); got != want {
			t.Errorf("safeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveRepo(t *testing.T) {
	cases := map[string]string{
		"https://acg-q.github.io/userscript-console": "acg-q/userscript-console",
		"https://acg-q.github.io":                    "",
		"https://example.com/repo":                   "",
		"":                                           "",
		"not a url":                                  "",
	}
	for in, want := range cases {
		if got := deriveRepo(in); got != want {
			t.Errorf("deriveRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCmdTime(t *testing.T) {
	cases := map[string]string{
		"2026-10-01T12:34:56Z": "2026-10-01 12:34",
		"2026-10-01T12:34:56":  "2026-10-01 12:34",
		"":                     "—",
		"garbage":              "garbage",
	}
	for in, want := range cases {
		if got := cmdTime(in); got != want {
			t.Errorf("cmdTime(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirst10(t *testing.T) {
	if got := first10("2026-10-05T00:00:00Z"); got != "2026-10-05" {
		t.Errorf("first10 = %q", got)
	}
	if got := first10("短"); got != "短" {
		t.Errorf("first10 短串应原样: %q", got)
	}
}

// ── 文档页 ───────────────────────────────────────────────────

// writeDocs 在 out 同级的 docs/ 写入文档文件。
func writeDocs(t *testing.T, out string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(filepath.Dir(filepath.Clean(out)), "docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestBuildWithDocs 有 index.md 时产 docs 页、nav 出「文档」、子页相对路径正确。
func TestBuildWithDocs(t *testing.T) {
	reg := buildTestRegistry(t)
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[]}`)
	writeDocs(t, outDir, map[string]string{
		"index.md":           "# 文档中心\n\n欢迎。",
		"SPEC-DATA-MODEL.md": "# 数据模型\n\n内容。",
	})

	out, err := Build(reg, Options{Out: outDir, PagesBase: "https://test.github.io/repo"}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.DocHTMLs) != 2 {
		t.Fatalf("应产 2 个文档页, got %d: %v", len(out.DocHTMLs), out.DocHTMLs)
	}
	idx, ok := out.DocHTMLs["index.html"]
	if !ok {
		t.Fatal("缺 docs/index.html")
	}
	if !strings.Contains(idx, "文档中心") || !strings.Contains(idx, `href="spec-data-model.html"`) {
		t.Errorf("docs/index 应含标题与目录链接")
	}
	spec := out.DocHTMLs["spec-data-model.html"]
	if !strings.Contains(spec, "数据模型") || !strings.Contains(spec, `href="index.html"`) {
		t.Errorf("子文档页应含标题与回目录链接")
	}
	if !strings.Contains(spec, `href="../index.html"`) {
		t.Errorf("文档页根链接应为 ../index.html")
	}
	// nav HasDocs
	if !strings.Contains(out.IndexHTML, `href="docs/index.html"`) {
		t.Errorf("首页 nav 应含文档链接")
	}
	if !strings.Contains(out.DetailHTMLs["self01"], `href="../docs/index.html"`) {
		t.Errorf("详情页 nav 应含 ../docs/index.html")
	}
}

// TestBuildWithoutIndexMD 无 index.md → 不产 docs（nav 死链防护）。
func TestBuildWithoutIndexMD(t *testing.T) {
	reg := buildTestRegistry(t)
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[]}`)
	writeDocs(t, outDir, map[string]string{"orphan.md": "# 孤儿"})

	out, err := Build(reg, Options{Out: outDir, PagesBase: "https://test.github.io/repo"}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.DocHTMLs) != 0 {
		t.Errorf("无 index.md 不应产文档页, got %v", out.DocHTMLs)
	}
	if strings.Contains(out.IndexHTML, "docs/index.html") {
		t.Errorf("无文档页时 nav 不应出现文档链接")
	}
}

// TestBuildDocsSlugConflict slug 冲突应报错而非静默覆盖。
func TestBuildDocsSlugConflict(t *testing.T) {
	reg := buildTestRegistry(t)
	outDir := isolatedOut(t)
	writeDocs(t, outDir, map[string]string{
		"index.md": "# 首页",
		"a b.md":   "# A",
		"a-b.md":   "# B（与 a b.md 同 slug）",
	})
	_, err := Build(reg, Options{Out: outDir, PagesBase: "https://test.github.io/repo"}, Data{})
	if err == nil {
		t.Fatal("slug 冲突应返回 error")
	}
	if !strings.Contains(err.Error(), "slug 冲突") {
		t.Errorf("错误应说明 slug 冲突: %v", err)
	}
}
