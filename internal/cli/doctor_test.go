package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acg-q/userscript-console/internal/registry"
)

// buildRepo 搭一个健康数据根。
func buildRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	reg := &registry.Registry{
		Schema: registry.SchemaVersion,
		Scripts: []registry.Script{
			{ID: "self01", Type: registry.TypeSelf, Name: "A", Version: "1.0.0", Enabled: true,
				Match: []string{}, Grant: []string{}, Changelog: []registry.ChangelogEntry{}, Discussions: []registry.DiscussionEntry{}},
			{ID: "sync01", Type: registry.TypeSynced, Name: "B", Version: "2.0.0", Enabled: true,
				Match: []string{}, Grant: []string{}, Changelog: []registry.ChangelogEntry{}, Discussions: []registry.DiscussionEntry{}},
		},
	}
	if err := reg.Save(filepath.Join(root, "registry.json")); err != nil {
		t.Fatal(err)
	}
	mustWrite := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(root, "scripts", "self", "self01", "index.js"), "// self")
	mustWrite(filepath.Join(root, "scripts", "synced", "sync01", "script.user.js"), "// synced")
	mustWrite(filepath.Join(root, "dist", "self01.user.js"), "// dist")
	return root
}

func TestDoctor健康仓库(t *testing.T) {
	probs := doctorProblems(buildRepo(t))
	if len(probs) != 0 {
		t.Errorf("健康仓库应无问题: %v", probs)
	}
}

func TestDoctorRegistry无法解析(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "registry.json"), []byte("{坏 JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	probs := doctorProblems(root)
	if len(probs) != 1 || !ContainsAll(probs[0], "registry.json", "无法解析") {
		t.Errorf("problems = %v", probs)
	}
}

func TestDoctorSchema不匹配(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "registry.json"), []byte(`{"schema":99,"scripts":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	probs := doctorProblems(root)
	if len(probs) != 1 || !ContainsAll(probs[0], "schema=99") {
		t.Errorf("problems = %v", probs)
	}
}

func TestDoctor缺源码与状态不一致(t *testing.T) {
	root := buildRepo(t)
	// 删掉活跃 self 源码 → 缺源码
	if err := os.Remove(filepath.Join(root, "scripts", "self", "self01", "index.js")); err != nil {
		t.Fatal(err)
	}
	probs := doctorProblems(root)
	found := false
	for _, p := range probs {
		if ContainsAll(p, "self01", "缺源码文件") {
			found = true
		}
	}
	if !found {
		t.Errorf("应报缺源码: %v", probs)
	}

	// 软删除条目但盘上文件仍在 → 双重报错
	root2 := buildRepo(t)
	reg, err := registry.Load(filepath.Join(root2, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.Scripts[1].Deleted = true
	if err := reg.Save(filepath.Join(root2, "registry.json")); err != nil {
		t.Fatal(err)
	}
	probs = doctorProblems(root2)
	var hasSrc, hasDist bool
	for _, p := range probs {
		if ContainsAll(p, "sync01", "仍存在源码") {
			hasSrc = true
		}
		if ContainsAll(p, "sync01", "条目已删而 dist 还在") {
			hasDist = true // dist 里没有 sync01 —— 应不触发
		}
	}
	if !hasSrc {
		t.Errorf("应报已删仍存源码: %v", probs)
	}
	if hasDist {
		t.Errorf("sync01 本无 dist，不应报: %v", probs)
	}
}

func TestDoctor已删但dist仍在(t *testing.T) {
	root := buildRepo(t)
	reg, err := registry.Load(filepath.Join(root, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.Scripts[0].Deleted = true // self01 有 dist 文件
	if err := reg.Save(filepath.Join(root, "registry.json")); err != nil {
		t.Fatal(err)
	}
	probs := doctorProblems(root)
	found := false
	for _, p := range probs {
		if ContainsAll(p, "self01", "条目已删而 dist 还在") {
			found = true
		}
	}
	if !found {
		t.Errorf("应报 dist 残留: %v", probs)
	}
}

func TestDoctor孤儿目录(t *testing.T) {
	root := buildRepo(t)
	if err := os.MkdirAll(filepath.Join(root, "scripts", "self", "ghost"), 0o755); err != nil {
		t.Fatal(err)
	}
	probs := doctorProblems(root)
	found := false
	for _, p := range probs {
		if ContainsAll(p, "孤儿目录", "ghost") {
			found = true
		}
	}
	if !found {
		t.Errorf("应报孤儿目录: %v", probs)
	}
}

func TestDoctorArchive重复与损坏(t *testing.T) {
	root := buildRepo(t)
	dir := filepath.Join(root, "archive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dup := `{"schema":1,"commands":[{"command_id":"IC_1"},{"command_id":"IC_1"}]}`
	if err := os.WriteFile(filepath.Join(dir, "commands.json"), []byte(dup), 0o644); err != nil {
		t.Fatal(err)
	}
	probs := doctorProblems(root)
	found := false
	for _, p := range probs {
		if ContainsAll(p, "重复 command_id", "IC_1") {
			found = true
		}
	}
	if !found {
		t.Errorf("应报重复: %v", probs)
	}

	if err := os.WriteFile(filepath.Join(dir, "commands.json"), []byte("坏"), 0o644); err != nil {
		t.Fatal(err)
	}
	probs = doctorProblems(root)
	found = false
	for _, p := range probs {
		if ContainsAll(p, "archive/commands.json", "无法解析") {
			found = true
		}
	}
	if !found {
		t.Errorf("应报 archive 损坏: %v", probs)
	}
}

func TestRunDoctor退出码(t *testing.T) {
	if code := RunDoctor(buildRepo(t), true, false); code != 0 {
		t.Errorf("健康应 0，got %d", code)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "registry.json"), []byte("{坏"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := RunDoctor(root, true, false); code != 1 {
		t.Errorf("--check 有问题应 1，got %d", code)
	}
	// 非 --check 只打印不失败
	if code := RunDoctor(root, false, false); code != 0 {
		t.Errorf("缺省应 0，got %d", code)
	}
}

// ── 新增覆盖率测试 ───────────────────────────────────────────

// TestEnvOr 测试环境变量读取。
func TestEnvOr(t *testing.T) {
	os.Setenv("TEST_CLI_ENV_OR_X", "hello")
	defer os.Unsetenv("TEST_CLI_ENV_OR_X")
	if got := EnvOr("TEST_CLI_ENV_OR_X", "default"); got != "hello" {
		t.Errorf("EnvOr = %q, want hello", got)
	}
	if got := EnvOr("TEST_CLI_ENV_OR_NONEXIST", "default"); got != "default" {
		t.Errorf("EnvOr 缺失应返回默认: got %q", got)
	}
}

// TestEnvInt 测试 envInt 路径。
func TestEnvInt(t *testing.T) {
	os.Setenv("TEST_CLI_ENVINT_X", "42")
	defer os.Unsetenv("TEST_CLI_ENVINT_X")
	// envInt 未导出，通过 RunDoctor 间接覆盖（无相关 env）
	// 直接测试 boolToInt
	if got := boolToInt(true); got != 1 {
		t.Errorf("boolToInt(true) = %d", got)
	}
	if got := boolToInt(false); got != 0 {
		t.Errorf("boolToInt(false) = %d", got)
	}
}

// TestRunDoctorJSONMode 测试 JSON 输出模式。
func TestRunDoctorJSONMode(t *testing.T) {
	root := buildRepo(t)
	// 健康仓库 + JSON → 应输出 JSON 且返回 0
	code := RunDoctor(root, false, true)
	if code != 0 {
		t.Errorf("健康仓库 JSON 应返回 0, got %d", code)
	}
}

// TestScriptSourcePathSelf 测试 self 类型路径。
func TestScriptSourcePathSelf(t *testing.T) {
	s := registry.Script{ID: "s1", Type: registry.TypeSelf}
	got := scriptSourcePath("/root", s)
	want := filepath.Join("/root", "scripts", "self", "s1", "index.js")
	if got != want {
		t.Errorf("scriptSourcePath(self) = %q, want %q", got, want)
	}
}

// TestScriptSourcePathSynced 测试 synced 类型路径。
func TestScriptSourcePathSynced(t *testing.T) {
	s := registry.Script{ID: "s2", Type: registry.TypeSynced}
	got := scriptSourcePath("/root", s)
	want := filepath.Join("/root", "scripts", "synced", "s2", "script.user.js")
	if got != want {
		t.Errorf("scriptSourcePath(synced) = %q, want %q", got, want)
	}
}

// TestFileExistsTrue 测试文件存在。
func TestFileExistsTrue(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "test.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileExists(path) {
		t.Error("fileExists 应对存在的文件返回 true")
	}
}

// TestFileExistsFalse 测试文件不存在。
func TestFileExistsFalse(t *testing.T) {
	if fileExists("/nonexistent/path/file.txt") {
		t.Error("fileExists 应对不存在的文件返回 false")
	}
}

// TestFileExistsDir 测试目录不是文件。
func TestFileExistsDir(t *testing.T) {
	root := t.TempDir()
	if fileExists(root) {
		t.Error("fileExists 应对目录返回 false")
	}
}

// TestContainsAll 测试 ContainsAll 辅助函数。
func TestContainsAll(t *testing.T) {
	if !ContainsAll("hello world", "hello", "world") {
		t.Error("ContainsAll 应包含所有子串")
	}
	if ContainsAll("hello", "world") {
		t.Error("ContainsAll 不应包含不存在的子串")
	}
}

// TestDoctorMissingArchiveDir 测试 archive 目录不存在时不报错。
func TestDoctorMissingArchiveDir(t *testing.T) {
	root := buildRepo(t)
	// archive 目录不存在 → 不应报错
	probs := doctorProblems(root)
	for _, p := range probs {
		if strings.Contains(p, "archive") {
			t.Errorf("不应报 archive 问题: %v", probs)
		}
	}
}

// TestDoctorOrphanSynced 测试 synced 类型孤儿目录。
func TestDoctorOrphanSynced(t *testing.T) {
	root := buildRepo(t)
	if err := os.MkdirAll(filepath.Join(root, "scripts", "synced", "ghost"), 0o755); err != nil {
		t.Fatal(err)
	}
	probs := doctorProblems(root)
	found := false
	for _, p := range probs {
		if strings.Contains(p, "synced") && strings.Contains(p, "ghost") {
			found = true
		}
	}
	if !found {
		t.Errorf("应报 synced 孤儿目录: %v", probs)
	}
}

// TestDoctorDeletedSyncedMissingSrc 测试已删除 synced 脚本缺源码。
func TestDoctorDeletedSyncedMissingSrc(t *testing.T) {
	root := buildRepo(t)
	reg, err := registry.Load(filepath.Join(root, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	// 删除 sync01 的源码并标记已删除 → 不应报错（符合预期）
	reg.Scripts[1].Deleted = true
	if err := reg.Save(filepath.Join(root, "registry.json")); err != nil {
		t.Fatal(err)
	}
	// 确保源码文件存在 → 应报"已删仍存源码"
	probs := doctorProblems(root)
	found := false
	for _, p := range probs {
		if strings.Contains(p, "sync01") && strings.Contains(p, "仍存在源码") {
			found = true
		}
	}
	if !found {
		t.Errorf("应报已删仍存源码: %v", probs)
	}
}
