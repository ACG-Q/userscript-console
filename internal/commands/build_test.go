package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acg-q/userscript-console/internal/registry"
)

// fakeSite 记录 /build 对 Env.Site 的调用。
type fakeSite struct {
	calls    int
	pages    int
	changed  bool
	warnings []string
	err      error
}

func (f *fakeSite) Build(*registry.Registry) (int, bool, []string, error) {
	f.calls++
	return f.pages, f.changed, f.warnings, f.err
}

// TestBuildWithoutSite Env.Site 为 nil（单测默认）时只产出 dist/ 脚本副本。
func TestBuildWithoutSite(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)
	env.PagesBase = "https://example.github.io/repo"

	res, err := Execute("build", env, "", nil)
	if err != nil {
		t.Fatalf("build 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "已构建") {
		t.Errorf("build 应提示已构建: %s", res.Text)
	}
	if strings.Contains(res.Text, "站点页面") {
		t.Errorf("未注入 Site 时不应出现站点页面行: %s", res.Text)
	}
	if res.Pages != 0 {
		t.Errorf("未注入 Site 时 Pages 应为 0, got %d", res.Pages)
	}
}

// TestBuildWithSite 注入 SiteBuilder 后：整站告警/页面数进 Result，changed 与站点联动。
func TestBuildWithSite(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)
	env.PagesBase = "https://example.github.io/repo"
	site := &fakeSite{pages: 7, changed: true, warnings: []string{"W1: IssueStats 未提供（降级渲染）"}}
	env.Site = site

	// 需要一个 self 源码文件，否则 dist 副本为空（changed 只能来自站点）。
	if err := writeSelfSource(env.Root, "self01"); err != nil {
		t.Fatalf("写源码失败: %v", err)
	}

	res, err := Execute("build", env, "", nil)
	if err != nil {
		t.Fatalf("build 执行失败: %v", err)
	}
	if site.calls != 1 {
		t.Errorf("Site.Build 应被调用 1 次, got %d", site.calls)
	}
	if !strings.Contains(res.Text, "站点页面: 7 个") {
		t.Errorf("回帖应含站点页面数: %s", res.Text)
	}
	if res.Pages != 7 {
		t.Errorf("Pages 应为 7, got %d", res.Pages)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "W1") {
		t.Errorf("Warnings 应透传站点告警, got %v", res.Warnings)
	}
	if !res.Changed {
		t.Errorf("站点有落盘变更时 Changed 应为 true")
	}
}

// TestBuildSiteError 站点构建失败是操作型错误 → 上抛（cli 层 exit 1）。
func TestBuildSiteError(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)
	env.PagesBase = "https://example.github.io/repo"
	env.Site = &fakeSite{err: errors.New("渲染失败")}

	if _, err := Execute("build", env, "", nil); err == nil {
		t.Fatal("Site.Build 失败应上抛 error")
	}
}

func writeSelfSource(root, id string) error {
	dir := filepath.Join(root, "scripts", "self", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "index.js"), []byte("// test"), 0o644)
}
