package main

// build --pages-out（合并 assemble_site.py，设计 D7）：
// *.user.js → <pagesOut>/<dist 段>/，其余 → <pagesOut>/；搬移后 dist 清空；
// pagesOut 空 → 跳过（纯 build）；目标不存在自动创建。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/acg-q/userscript-console/internal/layout"
)

func writeDistEntry(t *testing.T, root, rel string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func listNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestAssemblePages搬迁产物(t *testing.T) {
	root := t.TempDir()
	writeDistEntry(t, root, "dist/a.user.js")
	writeDistEntry(t, root, "dist/index.html")

	moved, err := assemblePages(root, layout.Defaults(), "site")
	if err != nil {
		t.Fatalf("assemblePages 失败: %v", err)
	}
	if moved != 2 {
		t.Errorf("moved 应为 2, got %d", moved)
	}
	if _, err := os.Stat(filepath.Join(root, "site", "dist", "a.user.js")); err != nil {
		t.Errorf("user.js 应落 <pagesOut>/<dist 段>/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "site", "index.html")); err != nil {
		t.Errorf("其余产物应落 <pagesOut>/ 根: %v", err)
	}
	if got := listNames(t, filepath.Join(root, "dist")); len(got) != 0 {
		t.Errorf("搬移后 dist 应清空, got %v", got)
	}
}

func TestAssemblePages嵌套dist段(t *testing.T) {
	root := t.TempDir()
	writeDistEntry(t, root, "bundle/js/app.user.js")
	lay := layout.Layout{
		Registry: "registry.json", Scripts: "scripts",
		Dist: "bundle/js", Archive: "archive/commands.json",
	}

	if _, err := assemblePages(root, lay, "site"); err != nil {
		t.Fatalf("assemblePages 失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "site", "bundle", "js", "app.user.js")); err != nil {
		t.Errorf("嵌套 dist 段应保持 <pagesOut>/bundle/js/: %v", err)
	}
}

func TestAssemblePages绝对dist(t *testing.T) {
	root := t.TempDir()
	absDist := t.TempDir()
	writeDistEntry(t, absDist, "xx.user.js")
	lay := layout.Layout{
		Registry: "registry.json", Scripts: "scripts",
		Dist: absDist, Archive: "archive/commands.json",
	}

	if _, err := assemblePages(root, lay, "site"); err != nil {
		t.Fatalf("assemblePages 失败: %v", err)
	}
	// 绝对 dist 不得逃出 pagesOut：用 dist 目录名作段。
	want := filepath.Join(root, "site", filepath.Base(absDist), "xx.user.js")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("绝对 dist 应落 <pagesOut>/<dist 目录名>/: %v", err)
	}
	if got := listNames(t, absDist); len(got) != 0 {
		t.Errorf("源 dist 应清空, got %v", got)
	}
}

func TestAssemblePages缺dist或空dist报错(t *testing.T) {
	root := t.TempDir()
	if _, err := assemblePages(root, layout.Defaults(), "site"); err == nil {
		t.Error("dist 缺失应报错")
	}
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := assemblePages(root, layout.Defaults(), "site"); err == nil {
		t.Error("dist 为空应报错")
	}
}

func TestAssemblePages自动创建pagesOut(t *testing.T) {
	root := t.TempDir()
	writeDistEntry(t, root, "dist/a.user.js")

	if _, err := assemblePages(root, layout.Defaults(), "out/deep/nested"); err != nil {
		t.Fatalf("assemblePages 失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "out", "deep", "nested", "dist", "a.user.js")); err != nil {
		t.Errorf("深层 pagesOut 应自动创建: %v", err)
	}
}

// TestPagesOutFrom优先级 --pages-out > env USM_PAGES_OUT > 空（不搬移）。
func TestPagesOutFrom优先级(t *testing.T) {
	t.Setenv("USM_PAGES_OUT", "envsite")
	if got := pagesOutFrom([]string{"--pages-out", "flagsite"}); got != "flagsite" {
		t.Errorf("flag 应覆盖 env, got %q", got)
	}
	if got := pagesOutFrom(nil); got != "envsite" {
		t.Errorf("无 flag 应取 env, got %q", got)
	}
	t.Setenv("USM_PAGES_OUT", "")
	if got := pagesOutFrom(nil); got != "" {
		t.Errorf("全缺省应为空, got %q", got)
	}
}

// TestBuildRunPagesOut搬移 build 成功后按 --pages-out 搬入，dist 清空。
func TestBuildRunPagesOut搬移(t *testing.T) {
	root := buildTestRegistryDir(t)
	t.Setenv("PAGES_BASE", "https://test.github.io/repo")
	t.Setenv("USM_PAGES_OUT", "")

	out := captureStdout(t, func() {
		if rc := buildRun([]string{"--root", root, "--json", "--pages-out=site"}); rc != 0 {
			t.Errorf("buildRun 应返回 0, got %d", rc)
		}
	})

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("输出非法 JSON: %v\n%s", err, out)
	}
	if changed, _ := payload["changed"].(bool); !changed {
		t.Error("有搬移时 changed 应为 true")
	}
	if _, err := os.Stat(filepath.Join(root, "site", "dist", "self01.user.js")); err != nil {
		t.Errorf("user.js 产物应搬入 site/dist/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "site", "index.html")); err != nil {
		t.Errorf("页面产物应搬入 site/ 根: %v", err)
	}
	if got := listNames(t, filepath.Join(root, "dist")); len(got) != 0 {
		t.Errorf("搬移后 dist 应清空, got %v", got)
	}
}

// TestBuildRunPagesOut空跳过 pagesOut 为空 → 纯 build，dist 原样保留。
func TestBuildRunPagesOut空跳过(t *testing.T) {
	root := buildTestRegistryDir(t)
	t.Setenv("PAGES_BASE", "https://test.github.io/repo")
	t.Setenv("USM_PAGES_OUT", "")

	out := captureStdout(t, func() {
		if rc := buildRun([]string{"--root", root, "--json"}); rc != 0 {
			t.Errorf("buildRun 应返回 0, got %d", rc)
		}
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("输出非法 JSON: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", "index.html")); err != nil {
		t.Errorf("pagesOut 为空时 dist 应原样保留: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "site")); !os.IsNotExist(err) {
		t.Errorf("pagesOut 为空不应创建 site/, err=%v", err)
	}
}
