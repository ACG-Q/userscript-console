package commands

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/acg-q/userscript-console/internal/registry"
)

// buildTestEnv 构造测试用 Env。
func buildTestEnv(t *testing.T) (*Env, string) {
	t.Helper()
	root := t.TempDir()
	reg := &registry.Registry{
		Schema: registry.SchemaVersion,
		Scripts: []registry.Script{
			{ID: "self01", Type: registry.TypeSelf, Name: "测试脚本", Version: "1.0.0", Enabled: true,
				Match: []string{"*://*/*"}, Grant: []string{"none"},
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-10-05T00:00:00Z",
				Changelog: []registry.ChangelogEntry{{Version: "1.0.0", Date: "2026-10-05", Note: "初始"}},
				Issue:     &registry.IssueRef{Number: 1, NodeID: "I_test1", URL: "https://github.com/test/issues/1"},
			},
			{ID: "del01", Type: registry.TypeSelf, Name: "已删除", Version: "0.1.0", Enabled: true, Deleted: true,
				Match: []string{}, Grant: []string{},
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-10-01T00:00:00Z",
			},
			{ID: "sync01", Type: registry.TypeSynced, Name: "同步脚本", Version: "2.0.0", Enabled: true,
				Match: []string{"*://example.com/*"}, Grant: []string{"GM.xmlHttpRequest"},
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-10-04T00:00:00Z",
				SourceURL:    stringPtr("https://greasyfork.org/scripts/12345"),
				SourceType:   stringPtr("greasyfork"),
				LastSyncedAt: stringPtr("2026-10-04T03:00:00Z"),
				SyncEnabled:  boolPtr(true),
			},
		},
	}
	if err := reg.Save(filepath.Join(root, "registry.json")); err != nil {
		t.Fatal(err)
	}
	return &Env{
		Root:       root,
		Now:        time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
		AuthorName: "Tester",
		RepoOwner:  "test-owner",
	}, root
}

func TestRmBasic(t *testing.T) {
	env, _ := buildTestEnv(t)
	res, err := Execute("rm", env, "self01", nil)
	if err != nil {
		t.Fatalf("rm 执行失败: %v", err)
	}
	if !contains(res.Text, "已软删除") {
		t.Errorf("rm 应返回成功消息: %s", res.Text)
	}
}

func TestRmAlreadyDeleted(t *testing.T) {
	env, _ := buildTestEnv(t)
	// 先删除
	Execute("rm", env, "del01", nil)
	// 再次删除应提示已删除
	res, err := Execute("rm", env, "del01", nil)
	if err != nil {
		t.Fatalf("rm 不应返回 error: %v", err)
	}
	if !contains(res.Text, "已是已删除状态") {
		t.Errorf("rm 已删除应提示: %s", res.Text)
	}
}

func TestRmNotFound(t *testing.T) {
	env, _ := buildTestEnv(t)
	res, err := Execute("rm", env, "nonexistent", nil)
	if err != nil {
		t.Fatalf("rm 不应返回 error: %v", err)
	}
	if !contains(res.Text, "未找到") {
		t.Errorf("rm 未找到应提示: %s", res.Text)
	}
}

func TestSyncNotSynced(t *testing.T) {
	env, _ := buildTestEnv(t)
	res, err := Execute("sync", env, "self01", nil)
	if err != nil {
		t.Fatalf("sync 执行失败: %v", err)
	}
	if !contains(res.Text, "自写脚本") {
		t.Errorf("sync 对 self 脚本应提示无法同步: %s", res.Text)
	}
}

func TestBuildNoPagesBase(t *testing.T) {
	env, _ := buildTestEnv(t)
	env.PagesBase = ""
	res, err := Execute("build", env, "", nil)
	if err != nil {
		t.Fatalf("build 不应返回 error: %v", err)
	}
	if !contains(res.Text, "PAGES_BASE") {
		t.Errorf("build 缺少 PagesBase 应提示: %s", res.Text)
	}
}

func TestProjectNoRepoOwner(t *testing.T) {
	env, _ := buildTestEnv(t)
	env.RepoOwner = ""
	res, err := Execute("project", env, "", nil)
	if err != nil {
		t.Fatalf("project 不应返回 error: %v", err)
	}
	if !contains(res.Text, "GH_REPO_OWNER") {
		t.Errorf("project 缺少 RepoOwner 应提示: %s", res.Text)
	}
}

func TestCleanupDryRun(t *testing.T) {
	env, _ := buildTestEnv(t)
	res, err := Execute("cleanup", env, "", nil)
	if err != nil {
		t.Fatalf("cleanup 不应返回 error: %v", err)
	}
	if !contains(res.Text, "dry-run") {
		t.Errorf("cleanup 默认应 dry-run: %s", res.Text)
	}
}

func TestCleanupWithApply(t *testing.T) {
	env, _ := buildTestEnv(t)
	res, err := Execute("cleanup", env, "--apply", nil)
	if err != nil {
		t.Fatalf("cleanup --apply 不应返回 error: %v", err)
	}
	if !contains(res.Text, "已清理") && !contains(res.Text, "dry-run") {
		t.Errorf("cleanup --apply 应有清理消息: %s", res.Text)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) > 0 && len(needle) > 0 && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
