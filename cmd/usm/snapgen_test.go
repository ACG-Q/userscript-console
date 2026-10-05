package main

import (
	"path/filepath"
	"testing"

	"github.com/acg-q/userscript-console/internal/registry"
)

// TestBuildSiteSnapshot 站点快照生成：路径齐全、内容非空、同输入两次一致。
func TestBuildSiteSnapshot(t *testing.T) {
	reg, err := registry.Load(filepath.Join("..", "..", "tests", "corpus", "inputs", "registry.json"))
	if err != nil {
		t.Fatalf("加载语料失败: %v", err)
	}

	files, err := buildSiteSnapshot(reg)
	if err != nil {
		t.Fatalf("生成站点快照失败: %v", err)
	}
	for _, name := range []string{"site/index.html", "site/scripts.json", "site/build-warnings.txt"} {
		if files[name] == "" {
			t.Errorf("快照 %s 不应为空", name)
		}
	}
	if len(files) < 3+len(reg.Scripts) {
		t.Errorf("应含全部详情页, got %d 个文件", len(files))
	}

	again, err := buildSiteSnapshot(reg)
	if err != nil {
		t.Fatalf("第二次生成失败: %v", err)
	}
	for name, content := range files {
		if again[name] != content {
			t.Errorf("快照 %s 应确定性生成", name)
		}
	}
}
