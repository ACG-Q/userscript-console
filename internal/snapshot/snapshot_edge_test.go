package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateFailsWhenParentPathIsFile(t *testing.T) {
	dir := t.TempDir()
	// sub 是文件而非目录：写入 sub/x.txt 前的 MkdirAll 必须失败
	if err := os.WriteFile(filepath.Join(dir, "sub"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(dir, map[string]string{"sub/x.txt": "v"}); err == nil {
		t.Fatal("父路径被文件占用时 Update 应返回 error")
	}
}
