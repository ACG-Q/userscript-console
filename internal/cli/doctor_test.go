package cli

import (
	"os"
	"path/filepath"
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
