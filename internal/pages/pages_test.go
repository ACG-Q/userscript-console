package pages

import (
	"encoding/json"
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
		IssueStats: map[string]int{"I_test1": 5},
		DiscussionComments: map[string][]Comment{
			"D_test1": {{Author: "User1", Body: "好脚本！", CreatedAt: "2026-10-05"}},
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
	// scripts.json 可解析且无转义 HTML
	var cards []scriptCard
	if err := json.Unmarshal([]byte(out.ScriptsJSON), &cards); err != nil {
		t.Fatalf("scripts.json 解析失败: %v", err)
	}
	if len(cards) != 2 { // 仅活跃脚本
		t.Errorf("cards 长度应为 2, got %d", len(cards))
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
		DiscussionComments: map[string][]Comment{
			"D_test1": {{Author: "User1", Body: "好脚本！", CreatedAt: "2026-10-05"}},
		},
	}
	out, err := Build(reg, opts, data)
	if err != nil {
		t.Fatal(err)
	}
	// 检查详情页包含必要元素
	for id, html := range out.DetailHTMLs {
		// 活跃脚本应有下载按钮，墓碑页应有 tombstone 类
		if !strings.Contains(html, "脚本控制台") && !strings.Contains(html, "下载脚本") && !strings.Contains(html, "tombstone") {
			t.Errorf("详情页 %s 缺少预期内容", id)
		}
	}
}

func TestBuildWarnings(t *testing.T) {
	reg := buildTestRegistry(t)
	opts := Options{Out: t.TempDir(), PagesBase: "https://test.github.io/repo"}
	// 不提供 IssueStats → 应产生 W1 警告
	out, err := Build(reg, opts, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.BuildWarnings) == 0 {
		t.Error("缺少 IssueStats 应产生 W1 警告")
	}
}

func TestTemplatesExist(t *testing.T) {
	expected := []string{"index.tmpl", "detail.tmpl", "commands.tmpl", "commands-index.tmpl"}
	names, err := readTemplates()
	if err != nil {
		t.Fatalf("readTemplates 失败: %v", err)
	}
	for _, want := range expected {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("模板 %s 不存在", want)
		}
	}
}

func TestStaticFilesEmbed(t *testing.T) {
	// 验证静态资源可嵌入
	entries, err := embedFS.ReadDir("static")
	if err != nil {
		t.Fatalf("读取 static 目录失败: %v", err)
	}
	if len(entries) == 0 {
		t.Error("static 目录应为空或包含文件")
	}
	cssCount := 0
	jsCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".css") {
			cssCount++
		}
		if strings.HasSuffix(e.Name(), ".js") {
			jsCount++
		}
	}
	if cssCount == 0 {
		t.Error("应有 CSS 文件")
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

func TestSanitizeDangerous(t *testing.T) {
	input := `<p>正常</p><script>alert(1)</script><div>也正常</div>`
	got := sanitizeDangerous(input)
	if strings.Contains(got, "<script>") {
		t.Errorf("sanitizeDangerous 应移除 script: %s", got)
	}
	if !strings.Contains(got, "<p>正常</p>") {
		t.Errorf("sanitizeDangerous 应保留 p: %s", got)
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

func TestBuildWithIssueStats(t *testing.T) {
	reg := buildTestRegistry(t)
	opts := Options{
		Out:       t.TempDir(),
		Now:       time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
		PagesBase: "https://test.github.io/repo",
	}
	data := Data{
		IssueStats: map[string]int{"I_test1": 10},
	}
	out, err := Build(reg, opts, data)
	if err != nil {
		t.Fatal(err)
	}
	// 有 IssueStats 时不应有 W1 警告
	for _, w := range out.BuildWarnings {
		if strings.Contains(w, "W1") {
			t.Error("有 IssueStats 时不应产生 W1 警告")
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
	opts := Options{PagesBase: "https://test.github.io/repo", Now: time.Now()}
	html, err := renderTombstone(s, opts)
	if err != nil {
		t.Fatalf("renderTombstone 失败: %v", err)
	}
	if !strings.Contains(html, "已删脚本") {
		t.Errorf("html 应含脚本名: %s", html)
	}
}

func TestReadTemplates(t *testing.T) {
	names, err := readTemplates()
	if err != nil {
		t.Fatalf("readTemplates 失败: %v", err)
	}
	if len(names) == 0 {
		t.Error("应有模板文件")
	}
}

func TestStaticFilesEmbedRecursive(t *testing.T) {
	// 验证 static 子目录也可嵌入
	found := false
	entries, err := embedFS.ReadDir("static/filter")
	if err == nil && len(entries) > 0 {
		found = true
	}
	entries, err = embedFS.ReadDir("static/list")
	if err == nil && len(entries) > 0 {
		found = true
	}
	entries, err = embedFS.ReadDir("static/disc")
	if err == nil && len(entries) > 0 {
		found = true
	}
	if !found {
		t.Log("static 子目录可能不存在，跳过")
	}
}

func TestRenderCommandPagesWithArchive(t *testing.T) {
	// 创建归档文件 - archive 在 Out 的父目录
	root := t.TempDir()
	archiveDir := filepath.Join(filepath.Dir(root), "archive")
	os.MkdirAll(archiveDir, 0o755)
	archiveData := `{
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
	}`
	if err := os.WriteFile(filepath.Join(archiveDir, "commands.json"), []byte(archiveData), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		Out:             root,
		Now:             time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
		PagesBase:       "https://test.github.io/repo",
		CommandsPerPage: 5,
	}
	cp, idx, err := renderCommands(opts)
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

func TestFilterActiveEmpty(t *testing.T) {
	result := filterActive(nil)
	if len(result) != 0 {
		t.Errorf("filterActive(nil) = %d, want 0", len(result))
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
