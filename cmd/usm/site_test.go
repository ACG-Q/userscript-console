package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/pages"
	"github.com/acg-q/userscript-console/internal/registry"
)

// fakeGH 注入 siteGH：不发网络请求即可覆盖 fetchData 的成功/降级分支。
type fakeGH struct {
	threads map[string]github.Thread
	errs    map[string]error
}

func (f *fakeGH) DiscThread(_ context.Context, nodeID string) (github.Thread, error) {
	if err, ok := f.errs[nodeID]; ok {
		return github.Thread{}, err
	}
	return f.threads[nodeID], nil
}

// siteTestRegistry 带 Issue 与版本帖账本的语料（含一个空 NodeID 的条目）。
func siteTestRegistry() *registry.Registry {
	return &registry.Registry{
		Schema: registry.SchemaVersion,
		Scripts: []registry.Script{{
			ID:      "self01",
			Type:    registry.TypeSelf,
			Name:    "测试",
			Version: "1.0.0",
			Enabled: true,
			Match:   []string{"*://*/*"},
			Grant:   []string{"none"},
			Issue:   &registry.IssueRef{Number: 1, NodeID: "ISSUE_NODE"},
			Discussions: []registry.DiscussionEntry{
				{Version: "1.0.0", NodeID: "DSC_OK", URL: "https://github.com/o/r/discussions/1", CreatedAt: "2026-01-01T00:00:00Z"},
				{Version: "0.9.0", NodeID: "", URL: "https://github.com/o/r/discussions/2", CreatedAt: "2026-01-02T00:00:00Z"},
				{Version: "0.8.0", NodeID: "DSC_ERR", URL: "https://github.com/o/r/discussions/3", CreatedAt: "2026-01-03T00:00:00Z"},
			},
			CreatedAt: "2026-01-01T00:00:00Z",
			UpdatedAt: "2026-10-05T00:00:00Z",
		}},
	}
}

// TestSiteFetchDataWithGH 抓取成功：版本帖评论与 answer 标记进 pages.Data，
// 抓取失败的版本帖降级为告警、不中断。
func TestSiteFetchDataWithGH(t *testing.T) {
	b := &siteBuilder{gh: &fakeGH{
		threads: map[string]github.Thread{
			"DSC_OK": {
				Comments:  []github.Comment{{Author: "alice", Body: "发布说明", CreatedAt: "2026-01-01T00:00:00Z"}},
				HasAnswer: true,
			},
		},
		errs: map[string]error{"DSC_ERR": errors.New("GraphQL 失败")},
	}}

	data, warnings := b.fetchData(siteTestRegistry())

	th := data.Discussions["DSC_OK"]
	if len(th.Comments) != 1 || th.Comments[0].Author != "alice" {
		t.Errorf("版本帖评论应落进 Data, got %+v", th)
	}
	if !th.HasAnswer {
		t.Error("HasAnswer 应透传进 Data")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "DSC_ERR") {
		t.Errorf("抓取失败应降级为告警, got %v", warnings)
	}
	if _, ok := data.Discussions["DSC_ERR"]; ok {
		t.Error("失败的版本帖不应写入数据")
	}
}

// TestSiteFetchDataDegraded gh=nil + 注册表含版本帖 → 无自身告警 + 空 Data（W1 由 pages.Build 统一判定）。
func TestSiteFetchDataDegraded(t *testing.T) {
	b := &siteBuilder{}

	data, warnings := b.fetchData(siteTestRegistry())

	if len(warnings) != 0 {
		t.Errorf("fetchData 不应自带降级告警（W1 归 Build）, got %v", warnings)
	}
	if data.Discussions != nil {
		t.Errorf("降级时 Data{} 应为零值, got %v", data.Discussions)
	}
}

// TestSiteFetchDataAllFailWithDiscussions 有账本但 0 线程抓到 → 全灭视为降级，返回 Data{}。
func TestSiteFetchDataAllFailWithDiscussions(t *testing.T) {
	b := &siteBuilder{gh: &fakeGH{
		errs: map[string]error{
			"DSC_OK":  errors.New("boom"),
			"DSC_ERR": errors.New("boom"),
		},
	}}

	data, warnings := b.fetchData(siteTestRegistry())

	if len(warnings) != 2 {
		t.Errorf("两条失败应各产一条告警, got %v", warnings)
	}
	if data.Discussions != nil {
		t.Errorf("全灭降级应返回 Data{}, got %v", data.Discussions)
	}
}

// TestSiteBuilderBuildWithGH Build 全链路：抓取 → 渲染 → 落盘。
func TestSiteBuilderBuildWithGH(t *testing.T) {
	root := t.TempDir()
	b := &siteBuilder{
		root:      root,
		pagesBase: "https://test.github.io/repo",
		now:       time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		gh: &fakeGH{
			threads: map[string]github.Thread{"DSC_OK": {}},
		},
	}

	pagesCount, changed, warnings, err := b.Build(siteTestRegistry())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if pagesCount == 0 {
		t.Error("页面数应 > 0")
	}
	if !changed {
		t.Error("首次构建应有落盘变更")
	}
	if len(warnings) != 0 {
		t.Errorf("抓取成功时不应有告警, got %v", warnings)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", "index.html")); err != nil {
		t.Errorf("Build 应写出 index.html: %v", err)
	}
}

func siteOutcome() pages.Outcome {
	return pages.Outcome{
		IndexHTML:     "<html>index</html>",
		ScriptsJSON:   "[]",
		DetailHTMLs:   map[string]string{"abc": "<html>detail</html>"},
		CommandPages:  map[int]string{1: "<html>cmd</html>"},
		CommandsIndex: "<html>cmds</html>",
		DocHTMLs:      map[string]string{"index.html": "<html>docs</html>", "guide.html": "<html>guide</html>"},
		BuildWarnings: []string{"W1: 讨论数据未提供（降级渲染）"},
	}
}

func siteRelPaths() []string {
	return []string{
		"dist/index.html",
		"dist/scripts.json",
		"dist/scripts/abc.html",
		"dist/commands/page-1.html",
		"dist/commands/index.html",
		"dist/docs/index.html",
		"dist/docs/guide.html",
		"dist/build-warnings.txt",
	}
}

// TestWriteSitePathContract 首次写盘必须落齐 PLAN.md C3-15 的路径契约。
func TestWriteSitePathContract(t *testing.T) {
	root := t.TempDir()

	changed, err := writeSite(root, siteOutcome())
	if err != nil {
		t.Fatalf("writeSite 失败: %v", err)
	}
	if !changed {
		t.Error("首次写盘应 changed=true")
	}
	for _, rel := range siteRelPaths() {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("缺少站点产物 %s: %v", rel, err)
		}
	}
}

// TestWriteSiteIdempotent 相同输入第二次构建不写盘（changed=false）。
func TestWriteSiteIdempotent(t *testing.T) {
	root := t.TempDir()
	out := siteOutcome()

	if _, err := writeSite(root, out); err != nil {
		t.Fatalf("首次写盘失败: %v", err)
	}
	changed, err := writeSite(root, out)
	if err != nil {
		t.Fatalf("第二次写盘失败: %v", err)
	}
	if changed {
		t.Error("相同输入第二次应 changed=false")
	}
}

// TestWriteSiteRemovesStale 删除脚本后其详情页/命令页必须被清理，
// 否则已删除内容仍能从站点访问。
func TestWriteSiteRemovesStale(t *testing.T) {
	root := t.TempDir()
	out := siteOutcome()
	if _, err := writeSite(root, out); err != nil {
		t.Fatalf("首次写盘失败: %v", err)
	}

	delete(out.DetailHTMLs, "abc")
	out.CommandPages = map[int]string{}
	out.CommandsIndex = ""
	out.DocHTMLs = map[string]string{}

	changed, err := writeSite(root, out)
	if err != nil {
		t.Fatalf("陈旧清理失败: %v", err)
	}
	if !changed {
		t.Error("删除产物应 changed=true")
	}
	for _, rel := range []string{
		"dist/scripts/abc.html", "dist/commands/page-1.html",
		"dist/commands/index.html", "dist/docs/index.html", "dist/docs/guide.html",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			t.Errorf("陈旧产物 %s 应被删除", rel)
		}
	}
}

// TestWriteSiteKeepsUserScripts 清理只针对 .html 站点产物，
// 不能误删 dist/ 下的 .user.js 分发文件。
func TestWriteSiteKeepsUserScripts(t *testing.T) {
	root := t.TempDir()
	userJS := filepath.Join(root, "dist", "self01.user.js")
	if err := os.MkdirAll(filepath.Dir(userJS), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userJS, []byte("// ==UserScript==\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := writeSite(root, siteOutcome()); err != nil {
		t.Fatalf("writeSite 失败: %v", err)
	}
	if _, err := os.Stat(userJS); err != nil {
		t.Errorf(".user.js 分发产物不应被清理: %v", err)
	}
}

// TestWriteSiteRejectsTraversalID 详情页 ID 进入文件路径，必须挡住路径穿越。
func TestWriteSiteRejectsTraversalID(t *testing.T) {
	root := t.TempDir()
	out := pages.Outcome{
		IndexHTML:    "<html></html>",
		DetailHTMLs:  map[string]string{"../../evil": "<html>x</html>"},
		CommandPages: map[int]string{},
	}

	if _, err := writeSite(root, out); err == nil {
		t.Fatal("含 .. 的 ID 应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(root, "evil.html")); err == nil {
		t.Error("不应写出数据根之外的文件")
	}
}

// TestWriteSiteRejectsTraversalDoc 文档 slug 同样进文件路径，必须挡住路径穿越。
func TestWriteSiteRejectsTraversalDoc(t *testing.T) {
	root := t.TempDir()
	out := pages.Outcome{
		IndexHTML:   "<html></html>",
		DocHTMLs:    map[string]string{"../evil.html": "<html>x</html>"},
		DetailHTMLs: map[string]string{},
	}

	if _, err := writeSite(root, out); err == nil {
		t.Fatal("含 .. 的文档文件名应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(root, "dist", "evil.html")); err == nil {
		t.Error("不应写出 docs 目录之外的文件")
	}
}

// TestBuildRunWritesSite buildRun 端到端：/build 必须真正产出站点文件。
func TestBuildRunWritesSite(t *testing.T) {
	root := buildTestRegistryDir(t)
	t.Setenv("PAGES_BASE", "https://test.github.io/repo")

	out := captureStdout(t, func() {
		if code := buildRun([]string{"--root", root, "--json"}); code != 0 {
			t.Errorf("buildRun 应返回 0, got %d", code)
		}
	})

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("build --json 输出非法 JSON: %v\n%q", err, out)
	}
	res, _ := payload["result"].(string)
	if res == "" {
		t.Fatalf("result 为空: %v", payload)
	}

	for _, rel := range []string{"dist/index.html", "dist/scripts.json", "dist/build-warnings.txt", "dist/scripts/self01.html"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("build 应产出 %s: %v", rel, err)
		}
	}
}
