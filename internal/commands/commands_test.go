package commands

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acg-q/userscript-console/internal/registry"
)

// buildTestEnvWithSource 构造带源码文件的测试 Env。
func buildTestEnvWithSource(t *testing.T) (*Env, string) {
	t.Helper()
	root := t.TempDir()
	reg := &registry.Registry{
		Schema: registry.SchemaVersion,
		Scripts: []registry.Script{
			{
				ID:        "self01",
				Type:      registry.TypeSelf,
				Name:      "测试脚本",
				Version:   "1.0.0",
				Enabled:   true,
				Deleted:   false,
				Match:     []string{"*://*/*"},
				Grant:     []string{"none"},
				CreatedAt: "2026-01-01T00:00:00Z",
				UpdatedAt: "2026-10-05T00:00:00Z",
				Changelog: []registry.ChangelogEntry{{Version: "1.0.0", Date: "2026-10-05", Note: "初始"}},
				Issue:     &registry.IssueRef{Number: 1, NodeID: "I_test1", URL: "https://github.com/test/issues/1"},
			},
			{
				ID:        "del01",
				Type:      registry.TypeSelf,
				Name:      "已删除脚本",
				Version:   "0.1.0",
				Enabled:   true,
				Deleted:   true,
				Match:     []string{},
				Grant:     []string{},
				CreatedAt: "2026-01-01T00:00:00Z",
				UpdatedAt: "2026-10-01T00:00:00Z",
			},
			{
				ID:           "sync01",
				Type:         registry.TypeSynced,
				Name:         "同步脚本",
				Version:      "2.0.0",
				Enabled:      true,
				Deleted:      false,
				Match:        []string{"*://example.com/*"},
				Grant:        []string{"GM.xmlHttpRequest"},
				CreatedAt:    "2026-01-01T00:00:00Z",
				UpdatedAt:    "2026-10-04T00:00:00Z",
				SourceURL:    stringPtr("https://greasyfork.org/scripts/12345"),
				SourceType:   stringPtr("greasyfork"),
				LastSyncedAt: stringPtr("2026-10-04T03:00:00Z"),
				SyncEnabled:  boolPtr(true),
			},
			{
				ID:        "disabled01",
				Type:      registry.TypeSynced,
				Name:      "停用脚本",
				Version:   "1.0.0",
				Enabled:   false,
				Deleted:   false,
				Match:     []string{},
				Grant:     []string{},
				CreatedAt: "2026-01-01T00:00:00Z",
				UpdatedAt: "2026-10-01T00:00:00Z",
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
	env, _ := buildTestEnvWithSource(t)
	res, err := Execute("rm", env, "self01", nil)
	if err != nil {
		t.Fatalf("rm 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "已软删除") {
		t.Errorf("rm 应返回成功消息: %s", res.Text)
	}
}

func TestRmAlreadyDeleted(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	_, _ = Execute("rm", env, "del01", nil)
	res, err := Execute("rm", env, "del01", nil)
	if err != nil {
		t.Fatalf("rm 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "已是已删除状态") {
		t.Errorf("rm 已删除应提示: %s", res.Text)
	}
}

func TestRmNotFound(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	res, err := Execute("rm", env, "nonexistent", nil)
	if err != nil {
		t.Fatalf("rm 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "未找到") {
		t.Errorf("rm 未找到应提示: %s", res.Text)
	}
}

func TestRmEmptyArgs(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	res, err := Execute("rm", env, "", nil)
	if err != nil {
		t.Fatalf("rm 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "用法") {
		t.Errorf("rm 空参数应提示用法: %s", res.Text)
	}
}

func TestSyncNotSynced(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	res, err := Execute("sync", env, "self01", nil)
	if err != nil {
		t.Fatalf("sync 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "自写脚本") {
		t.Errorf("sync 对 self 脚本应提示无法同步: %s", res.Text)
	}
}

func TestSyncDisabled(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	res, err := Execute("sync", env, "disabled01", nil)
	if err != nil {
		t.Fatalf("sync 执行失败: %v", err)
	}
	// 停用脚本可能因为缺少 SourceURL 而报错
	if !strings.Contains(res.Text, "停用") && !strings.Contains(res.Text, "来源 URL") {
		t.Errorf("sync 停用脚本应提示错误或缺少 URL: %s", res.Text)
	}
}

func TestSyncNotFound(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	res, err := Execute("sync", env, "nonexistent", nil)
	if err != nil {
		t.Fatalf("sync 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "未找到") {
		t.Errorf("sync 未找到应提示: %s", res.Text)
	}
}

func TestBuildNoPagesBase(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	env.PagesBase = ""
	res, err := Execute("build", env, "", nil)
	if err != nil {
		t.Fatalf("build 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "PAGES_BASE") {
		t.Errorf("build 缺少 PagesBase 应提示: %s", res.Text)
	}
}

func TestProjectNoRepoOwner(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	env.RepoOwner = ""
	res, err := Execute("project", env, "", nil)
	if err != nil {
		t.Fatalf("project 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "GH_REPO_OWNER") {
		t.Errorf("project 缺少 RepoOwner 应提示: %s", res.Text)
	}
}

func TestProjectWithRepoOwner(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	env.RepoOwner = "test-owner"
	res, err := Execute("project", env, "", nil)
	if err != nil {
		t.Fatalf("project 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "活跃脚本") {
		t.Errorf("project 应输出统计: %s", res.Text)
	}
}

func TestCleanupDryRun(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	res, err := Execute("cleanup", env, "", nil)
	if err != nil {
		t.Fatalf("cleanup 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "dry-run") {
		t.Errorf("cleanup 默认应 dry-run: %s", res.Text)
	}
}

func TestCleanupWithApply(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	res, err := Execute("cleanup", env, "--apply", nil)
	if err != nil {
		t.Fatalf("cleanup --apply 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "cleanup 完成") {
		t.Errorf("cleanup --apply 应有清理完成消息: %s", res.Text)
	}
}

func TestListWithDeleted(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	res, err := Execute("list", env, "", nil)
	if err != nil {
		t.Fatalf("list 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "测试脚本") {
		t.Errorf("list 应显示活跃脚本: %s", res.Text)
	}
	if !strings.Contains(res.Text, "已删除") && !strings.Contains(res.Text, "del01") {
		// 可能显示"另有 N 个已删除"
		if !strings.Contains(res.Text, "已删除") {
			t.Errorf("list 应提及已删除脚本: %s", res.Text)
		}
	}
}

func TestInfoNotFound(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	res, err := Execute("info", env, "nonexistent", nil)
	if err != nil {
		t.Fatalf("info 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "未找到") {
		t.Errorf("info 未找到应提示: %s", res.Text)
	}
}

func TestNamesAllCommands(t *testing.T) {
	names := Names()
	expected := []string{"add", "build", "cleanup", "info", "list", "project", "rm", "sync", "sync-all"}
	for _, want := range expected {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("命令 %q 未注册，当前: %v", want, names)
		}
	}
}

func TestExecuteUnknown(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	res, err := Execute("bogus", env, "", nil)
	if err == nil {
		t.Fatal("未知命令应返回 error")
	}
	if res.Text != "" {
		t.Errorf("未知命令结果文本应为空: %q", res.Text)
	}
}

func TestExecuteNilEnv(t *testing.T) {
	res, err := Execute("list", nil, "", nil)
	if err == nil {
		t.Fatal("nil Env 应返回 error")
	}
	if !strings.Contains(err.Error(), "Env 未初始化") {
		t.Errorf("错误信息应包含 'Env 未初始化': %v", err)
	}
	if res.Text != "" {
		t.Errorf("结果文本应为空: %q", res.Text)
	}
}

func TestWrapPanicBoundary(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic 未被 Wrap 捕获: %v", r)
		}
	}()

	// 先注册 panic 命令
	Register(Command{
		Name:  "panic_test",
		Help:  "panic test",
		Usage: "/panic_test",
		Run: func(env *Env, args string, code []string) (Result, error) {
			panic("intentional panic for testing")
		},
	})

	env, _ := buildTestEnvWithSource(t)
	res, err := Execute("panic_test", env, "", nil)
	if err != nil {
		t.Fatalf("Wrap 应捕获 panic，err 应为 nil: %v", err)
	}
	if res.Text == "" {
		t.Error("panic 后应返回错误文本")
	}
}

func TestRegisterDuplicate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("重复注册应 panic")
		}
	}()
	Register(Command{
		Name:  "info", // 已存在，会触发重复注册 panic
		Help:  "dup",
		Usage: "/dup",
		Run: func(env *Env, args string, code []string) (Result, error) {
			return Result{}, nil
		},
	})
}

func TestRegisterWithCustomRun(t *testing.T) {
	Register(Command{
		Name:  "custom_test",
		Help:  "自定义命令",
		Usage: "/custom_test",
		Run: func(env *Env, args string, code []string) (Result, error) {
			return Result{Text: "custom result"}, nil
		},
	})
	env, _ := buildTestEnvWithSource(t)
	res, err := Execute("custom_test", env, "", nil)
	if err != nil {
		t.Fatalf("自定义命令执行失败: %v", err)
	}
	if res.Text != "custom result" {
		t.Errorf("应返回 custom result，实际: %q", res.Text)
	}
}

func TestRegisterEmptyName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("空名应 panic")
		}
	}()
	Register(Command{
		Name:  "",
		Help:  "empty",
		Usage: "/empty",
		Run: func(env *Env, args string, code []string) (Result, error) {
			return Result{}, nil
		},
	})
}

func TestRegisterNilHandler(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("nil handler 应 panic")
		}
	}()
	Register(Command{
		Name:  "nil_handler_test",
		Help:  "nil",
		Usage: "/nil",
		Run:   nil,
	})
}

func TestFindEntryByID(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	r, err := loadReg(env)
	if err != nil {
		t.Fatal(err)
	}
	s := findEntry(r, "self01")
	if s == nil {
		t.Fatal("应找到 self01")
	}
	if s.ID != "self01" {
		t.Errorf("ID 不匹配: %s", s.ID)
	}
}

func TestFindEntryByName(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	r, err := loadReg(env)
	if err != nil {
		t.Fatal(err)
	}
	s := findEntry(r, "测试脚本")
	if s == nil {
		t.Fatal("应找到测试脚本")
	}
	if s.Name != "测试脚本" {
		t.Errorf("名称不匹配: %s", s.Name)
	}
}

func TestFindEntryByURL(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	r, err := loadReg(env)
	if err != nil {
		t.Fatal(err)
	}
	s := findEntry(r, "https://greasyfork.org/scripts/12345")
	if s == nil {
		t.Fatal("应找到 sync01")
	}
	if s.ID != "sync01" {
		t.Errorf("ID 不匹配: %s", s.ID)
	}
}
