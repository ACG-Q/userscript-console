package commands

import (
	"path/filepath"
	"strings"
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
				Issue: &registry.IssueRef{Number: 1, NodeID: "I_test1", URL: "https://github.com/test/issues/1"},
			},
			{ID: "del01", Type: registry.TypeSelf, Name: "已删除", Version: "0.1.0", Enabled: true, Deleted: true,
				Match: []string{}, Grant: []string{},
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-10-01T00:00:00Z",
			},
		},
	}
	if err := reg.Save(filepath.Join(root, "registry.json")); err != nil {
		t.Fatal(err)
	}
	return &Env{
		Root:   root,
		Now:    time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
		AuthorName: "Tester",
	}, root
}

func TestNamesContainsExpected(t *testing.T) {
	names := Names()
	expected := []string{"info", "list"}
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

func TestGetUnknown(t *testing.T) {
	_, ok := Get("nonexistent")
	if ok {
		t.Error("未注册命令应返回 ok=false")
	}
}

func TestExecuteUnknown(t *testing.T) {
	env, _ := buildTestEnv(t)
	_, err := Execute("nonexistent", env, "", nil)
	if err == nil {
		t.Error("执行未注册命令应返回 error")
	}
	if !strings.Contains(err.Error(), "未知命令") {
		t.Errorf("错误应包含'未知命令': %v", err)
	}
}

func TestInfoBasic(t *testing.T) {
	env, _ := buildTestEnv(t)
	res, err := Execute("info", env, "self01", nil)
	if err != nil {
		t.Fatalf("info 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "测试脚本") {
		t.Errorf("info 应包含脚本名: %s", res.Text)
	}
	if !strings.Contains(res.Text, "self01") {
		t.Errorf("info 应包含 ID: %s", res.Text)
	}
	if !strings.Contains(res.Text, "1.0.0") {
		t.Errorf("info 应包含版本: %s", res.Text)
	}
}

func TestInfoNotFound(t *testing.T) {
	env, _ := buildTestEnv(t)
	res, err := Execute("info", env, "nonexistent", nil)
	if err != nil {
		t.Fatalf("info 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "未找到") {
		t.Errorf("info 应返回未找到提示: %s", res.Text)
	}
}

func TestListBasic(t *testing.T) {
	env, _ := buildTestEnv(t)
	res, err := Execute("list", env, "", nil)
	if err != nil {
		t.Fatalf("list 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "测试脚本") {
		t.Errorf("list 应包含活动脚本: %s", res.Text)
	}
	// 检查表格中不包含已删除脚本 ID
	lines := strings.Split(res.Text, "\n")
	for _, line := range lines {
		if strings.Contains(line, "|") && strings.Contains(line, "del01") {
			t.Errorf("list 表格不应包含已删除脚本: %s", res.Text)
		}
	}
	if !strings.Contains(res.Text, "共 1 个") {
		t.Errorf("list 统计应正确: %s", res.Text)
	}
}

func TestListWithDeleted(t *testing.T) {
	env, _ := buildTestEnv(t)
	res, err := Execute("list", env, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "另有 1 个已删除") {
		t.Errorf("list 应提示已删除数量: %s", res.Text)
	}
}

func TestWrapPanicBoundary(t *testing.T) {
	// 测试 Wrap 的 panic 边界
	h := func(env *Env, args string, code []string) (Result, error) {
		panic("test panic")
	}
	wrapped := Wrap("test", h)
	res, err := wrapped(&Env{}, "", nil)
	if err != nil {
		t.Errorf("Wrap 应使 panic 转换为 err=nil，实际 err=%v", err)
	}
	if !strings.Contains(res.Text, "内部错误") {
		t.Errorf("Wrap 应返回错误信息: %s", res.Text)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("重复注册应 panic")
		}
	}()
	Register(Command{Name: "test", Run: func(*Env, string, []string) (Result, error) { return Result{}, nil }})
	Register(Command{Name: "test", Run: func(*Env, string, []string) (Result, error) { return Result{}, nil }})
}

func TestRegisterEmptyName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("空名注册应 panic")
		}
	}()
	Register(Command{Name: "", Run: func(*Env, string, []string) (Result, error) { return Result{}, nil }})
}

func TestRegisterNilHandler(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("nil handler 注册应 panic")
		}
	}()
	Register(Command{Name: "test"})
}

func TestFindEntryByID(t *testing.T) {
	env, root := buildTestEnv(t)
	_ = env
	_ = root
	// findEntry 是包内函数，通过 Execute 间接测试
	_, err := Execute("info", env, "self01", nil)
	if err != nil {
		t.Errorf("按 ID 查找应成功: %v", err)
	}
}
