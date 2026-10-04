package snapshot

import (
	"fmt"
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

func TestRegisterDefaultPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("重复 RegisterDefault 应 panic")
		}
	}()
	RegisterDefault(func() (map[string]string, error) {
		return map[string]string{"x": "y"}, nil
	})
	RegisterDefault(func() (map[string]string, error) {
		return nil, nil
	})
}

func TestRunNoGenerator(t *testing.T) {
	// 确保 defaultGen 为 nil（测试隔离）
	if code := Run(t.TempDir(), []string{"check"}); code != 1 {
		t.Errorf("未注册生成器应 exit 1, got %d", code)
	}
}

func TestRunUpdatePath(t *testing.T) {
	dir := t.TempDir()
	// 确保 defaultGen 为 nil（测试隔离）
	defaultGen = nil
	RegisterDefault(func() (map[string]string, error) {
		return map[string]string{"test.txt": "content\n"}, nil
	})
	defer func() { defaultGen = nil }()
	if code := Run(dir, []string{"update"}); code != 0 {
		t.Errorf("update 应 exit 0, got %d", code)
	}
	// normalize 会移除末尾换行，所以写入文件后内容应为 "content"
	if content, _ := os.ReadFile(filepath.Join(dir, "test.txt")); string(content) != "content" {
		t.Errorf("update 应写文件，实际: %q", content)
	}
}

func TestRunCheckPath(t *testing.T) {
	dir := t.TempDir()
	// 确保 defaultGen 为 nil（测试隔离）
	defaultGen = nil
	RegisterDefault(func() (map[string]string, error) {
		return map[string]string{"test.txt": "content\n"}, nil
	})
	defer func() { defaultGen = nil }()
	// 先 update
	Run(dir, []string{"update"})
	// 再 check 应一致
	if code := Run(dir, []string{"check"}); code != 0 {
		t.Errorf("check 一致应 exit 0, got %d", code)
	}
	// 修改文件后 check 应不一致
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("modified\n"), 0o644)
	if code := Run(dir, []string{"check"}); code != 1 {
		t.Errorf("check 不一致应 exit 1, got %d", code)
	}
}

func TestRunGeneratorError(t *testing.T) {
	dir := t.TempDir()
	defaultGen = nil
	RegisterDefault(func() (map[string]string, error) {
		return nil, fmt.Errorf("gen 失败")
	})
	defer func() { defaultGen = nil }()
	if code := Run(dir, []string{"check"}); code != 1 {
		t.Errorf("生成器报错应 exit 1, got %d", code)
	}
}

func TestAtomicWriteError(t *testing.T) {
	// 写入一个不存在的目录 → atomicWrite 应失败
	dir := filepath.Join(t.TempDir(), "nonexistent", "nested")
	path := filepath.Join(dir, "out.txt")
	if err := atomicWrite(path, []byte("data")); err == nil {
		t.Fatal("atomicWrite 到不存在目录应返回 error")
	}
}

func TestReadTreeNonExistent(t *testing.T) {
	// 目录不存在 → readTree 应返回空 map，不 panic
	out := readTree(filepath.Join(t.TempDir(), "nope"))
	if len(out) != 0 {
		t.Errorf("readTree 不存在目录应返回空 map, got %v", out)
	}
}

func TestNormalize(t *testing.T) {
	if got := normalize("hello\r\nworld\r\n"); got != "hello\nworld" {
		t.Errorf("normalize CRLF = %q, want hello\\nworld", got)
	}
	if got := normalize("trailing\n\n"); got != "trailing" {
		t.Errorf("normalize trailing = %q, want trailing", got)
	}
	if got := normalize(""); got != "" {
		t.Errorf("normalize empty = %q", got)
	}
}

func TestUnifiedDiffLengthDiff(t *testing.T) {
	got := "a\nb\nc"
	want := "a\nb"
	d := unifiedDiff("f", got, want)
	if d == "" {
		t.Error("长度不同时 unifiedDiff 不应为空")
	}
	if !strings.Contains(d, "首个差异在第") {
		t.Errorf("unifiedDiff 应含行号: %q", d)
	}
}

func TestUnifiedDiffIdentical(t *testing.T) {
	d := unifiedDiff("f", "same\n", "same\n")
	if d != "" {
		t.Errorf("相同内容应返回空 diff, got %q", d)
	}
}
