package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRejectsNilScripts(t *testing.T) {
	r := &Registry{Schema: SchemaVersion}
	err := r.Validate()
	if err == nil {
		t.Fatal("scripts 为 null 应校验失败")
	}
	if !strings.Contains(err.Error(), "scripts 必须是数组") {
		t.Errorf("错误信息应说明 scripts 必须是数组: %v", err)
	}
}

func TestRemoveIDRemovesExistingAndMisses(t *testing.T) {
	r := &Registry{
		Schema: SchemaVersion,
		Scripts: []Script{
			{ID: "keep01", Type: TypeSelf, Name: "保留"},
			{ID: "drop01", Type: TypeSelf, Name: "移除"},
		},
	}
	if !r.RemoveID("drop01") {
		t.Fatal("移除已存在 ID 应返回 true")
	}
	if len(r.Scripts) != 1 || r.Scripts[0].ID != "keep01" {
		t.Errorf("移除后应只剩 keep01: %+v", r.Scripts)
	}
	if r.RemoveID("drop01") {
		t.Error("重复移除同一 ID 应返回 false")
	}
}

func TestSaveFailsWhenParentPathIsFile(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Registry{Schema: SchemaVersion, Scripts: []Script{}}
	if err := r.Save(filepath.Join(blocker, "registry.json")); err == nil {
		t.Fatal("父目录被同名文件占用时 Save 应返回 error")
	}
}
