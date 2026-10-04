package snapshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	write("a.txt", "hello\n")
	write("b/c.txt", "世界\r\n") // CRLF 归一后应一致
	write("orphan.txt", "x")

	files := map[string]string{
		"a.txt":   "hello\n",
		"b/c.txt": "世界\n",
		"new.txt": "added",
	}
	diffs := Check(dir, files)
	joined := strings.Join(diffs, "\n")
	if !strings.Contains(joined, "不一致: new.txt") && !strings.Contains(joined, "缺失: new.txt") {
		t.Errorf("应报缺失 new.txt: %s", joined)
	}
	if !strings.Contains(joined, "孤儿: orphan.txt") {
		t.Errorf("应报孤儿 orphan.txt: %s", joined)
	}
	if strings.Contains(joined, "a.txt") || strings.Contains(joined, "b/c.txt") {
		t.Errorf("一致的文件不应出现在差异中: %s", joined)
	}

	// 内容差异 → 逐行上下文
	files["a.txt"] = "world\n"
	diffs = Check(dir, files)
	if len(diffs) == 0 || !strings.Contains(diffs[0], "首个差异在第 1 行") {
		t.Errorf("内容差异未检出: %v", diffs)
	}
}

func TestCheckClean(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{"x/y.md": "# 标题\n内容"}
	if diffs := Check(dir, files); len(diffs) != 1 { // 缺失 x/y.md
		t.Fatalf("初始应报缺失: %v", diffs)
	}
	if _, err := Update(dir, files); err != nil {
		t.Fatal(err)
	}
	if diffs := Check(dir, files); len(diffs) != 0 {
		t.Errorf("update 后应一致: %v", diffs)
	}
	// 幂等：再 update 无变化
	changed, err := Update(dir, files)
	if err != nil || len(changed) != 0 {
		t.Errorf("重复 update 应无变化: %v %v", changed, err)
	}
	// 末尾换行差异不算不一致（normalize）
	files2 := map[string]string{"x/y.md": "# 标题\n内容\n"}
	if diffs := Check(dir, files2); len(diffs) != 0 {
		t.Errorf("末尾换行归一失败: %v", diffs)
	}
}

func TestUpdateRemovesOrphans(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "old.txt"), []byte("x"), 0o644)
	changed, err := Update(dir, map[string]string{"keep.txt": "k"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 2 {
		t.Errorf("changed = %v", changed)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.txt")); !os.IsNotExist(err) {
		t.Error("孤儿基线未删除")
	}
}

func TestRunUsage(t *testing.T) {
	if code := Run(t.TempDir(), []string{"bogus"}); code != 2 {
		t.Errorf("用法错误应 exit 2, got %d", code)
	}
	if code := Run(t.TempDir(), nil); code != 2 {
		t.Errorf("缺参应 exit 2, got %d", code)
	}
}

func TestIgnoredFiles不参与孤儿判定(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{"a.txt": "a"}
	if _, err := Update(dir, files); err != nil {
		t.Fatal(err)
	}
	// README.md 允许存在且不报孤儿、不被清理
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# 说明"), 0o644); err != nil {
		t.Fatal(err)
	}
	if diffs := Check(dir, files); len(diffs) != 0 {
		t.Errorf("README 不应算孤儿: %v", diffs)
	}
	changed, err := Update(dir, files)
	if err != nil || len(changed) != 0 {
		t.Errorf("README 不应被清理: %v %v", changed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Error("README 被误删")
	}
}
