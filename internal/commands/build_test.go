package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acg-q/userscript-console/internal/registry"
)

func TestRunBuildMissingSource(t *testing.T) {
	root := t.TempDir()
	// 创建一个 registry，其中 self 脚本的源码文件不存在
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
			},
		},
	}
	if err := reg.Save(filepath.Join(root, "registry.json")); err != nil {
		t.Fatal(err)
	}
	// 不创建脚本文件 → ReadSource 将失败

	env := &Env{
		Root:      root,
		PagesBase: "https://test.github.io/repo",
		Now:       time.Now(),
	}
	res, err := runBuild(env, "", nil)
	if err != nil {
		t.Fatalf("runBuild 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "错误") {
		t.Errorf("缺少源码时应含错误信息: %s", res.Text)
	}
}

func TestRunBuildEmptyRegistry(t *testing.T) {
	root := t.TempDir()
	reg := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{}}
	if err := reg.Save(filepath.Join(root, "registry.json")); err != nil {
		t.Fatal(err)
	}
	env := &Env{
		Root:      root,
		PagesBase: "https://test.github.io/repo",
		Now:       time.Now(),
	}
	res, err := runBuild(env, "", nil)
	if err != nil {
		t.Fatalf("runBuild 空注册表不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "已构建: 0") {
		t.Errorf("空注册表应输出 0 构建: %s", res.Text)
	}
}

func TestLoadRegInvalidRoot(t *testing.T) {
	env := &Env{Root: "/nonexistent/root/path"}
	_, err := loadReg(env)
	if err == nil {
		t.Fatal("loadReg 无效路径应返回 error")
	}
}

// TestListDist_FilePath 读取文件路径时：Windows 返回空 slice（无 error），其他平台返回 error。
func TestListDist_FilePath(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "afile.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := listDist(filePath)
	if err != nil {
		// Linux/macOS：os.ReadDir 对文件路径返回 error
		t.Logf("listDist 对文件路径返回 error（非 Windows 行为）: %v", err)
		return
	}
	// Windows：os.ReadDir 对文件路径返回空 slice，无 error
	if len(files) != 0 {
		t.Fatalf("Windows 下应返回空 slice，实际: %v", files)
	}
}

// TestListDist_NonExistentDir 不存在的目录应返回 nil 而非 error。
func TestListDist_NonExistentDir(t *testing.T) {
	root := t.TempDir()
	_, err := listDist(filepath.Join(root, "no-such-dir"))
	if err != nil {
		t.Fatalf("不存在目录应返回 nil, 实际: %v", err)
	}
}
