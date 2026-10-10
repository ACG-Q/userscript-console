package script

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acg-q/userscript-console/internal/registry"
)

// assertNoTempResidue 断言 root 下没有原子写残留的临时文件（*.tmp-*）。
func assertNoTempResidue(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.Contains(d.Name(), ".tmp-") {
			t.Errorf("残留临时文件: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 %s 失败: %v", root, err)
	}
}

// listDir 列出目录条目名。
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取目录 %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// notExist 断言路径不存在。
func notExist(t *testing.T, p, why string) {
	t.Helper()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("%s 应不存在（stat err=%v）", why, err)
	}
}

func Test源码写读往返(t *testing.T) {
	const content = "// ==UserScript==\n// @name 往返\n// ==/UserScript==\nbody();\n"
	cases := []struct {
		name       string
		id         string
		scriptType string
		wantRel    string
	}{
		{"自写脚本", "demo-script", registry.TypeSelf, "scripts/self/demo-script/index.js"},
		{"同步脚本", "0123456789ab", registry.TypeSynced, "scripts/synced/0123456789ab/script.user.js"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			f := FS{Root: root}
			if err := f.WriteSource(tt.id, tt.scriptType, content); err != nil {
				t.Fatalf("WriteSource 报错: %v", err)
			}
			got, err := f.ReadSource(tt.id, tt.scriptType)
			if err != nil {
				t.Fatalf("ReadSource 报错: %v", err)
			}
			if got != content {
				t.Errorf("读回内容不一致:\n got=%q\nwant=%q", got, content)
			}
			abs := filepath.Join(root, filepath.FromSlash(tt.wantRel))
			if _, err := os.Stat(abs); err != nil {
				t.Errorf("约定路径 %s 未生成: %v", tt.wantRel, err)
			}
			// 目录内只有目标文件：无临时残留、无多余文件
			names := listDir(t, filepath.Dir(abs))
			if len(names) != 1 || names[0] != filepath.Base(abs) {
				t.Errorf("目录条目不符，期望仅 %s，实际 %v", filepath.Base(abs), names)
			}
			assertNoTempResidue(t, root)
		})
	}
}

func Test自定义脚本与分发目录(t *testing.T) {
	root := t.TempDir()
	f := FS{Root: root, Scripts: "uscripts", Dist: "cdn"}
	if err := f.WriteSource("demo-script", registry.TypeSelf, "src"); err != nil {
		t.Fatalf("WriteSource 报错: %v", err)
	}
	if rel := f.SelfSourcePath("demo-script"); rel != "uscripts/self/demo-script/index.js" {
		t.Errorf("SelfSourcePath = %q", rel)
	}
	if _, err := os.Stat(filepath.Join(root, "uscripts", "self", "demo-script", "index.js")); err != nil {
		t.Errorf("自定义 scripts 目录未生效: %v", err)
	}
	if err := f.WriteDist("demo-script", "dist"); err != nil {
		t.Fatalf("WriteDist 报错: %v", err)
	}
	if rel := f.DistPath("demo-script"); rel != "cdn/demo-script.user.js" {
		t.Errorf("DistPath = %q", rel)
	}
	if _, err := os.Stat(filepath.Join(root, "cdn", "demo-script.user.js")); err != nil {
		t.Errorf("自定义 dist 目录未生效: %v", err)
	}
	// 零值 FS 行为与历史硬编码一致
	def := FS{Root: root}
	if rel := def.DistPath("demo-script"); rel != "dist/demo-script.user.js" {
		t.Errorf("零值 DistPath = %q", rel)
	}
}

func Test覆盖写入原子无临时残留(t *testing.T) {
	root := t.TempDir()
	f := FS{Root: root}
	id := "twice"
	if err := f.WriteSource(id, registry.TypeSelf, "first"); err != nil {
		t.Fatalf("首次写入报错: %v", err)
	}
	if err := f.WriteSource(id, registry.TypeSelf, "second"); err != nil {
		t.Fatalf("覆盖写入报错: %v", err)
	}
	got, err := f.ReadSource(id, registry.TypeSelf)
	if err != nil || got != "second" {
		t.Errorf("覆盖后内容 = %q (err=%v)，期望 %q", got, err, "second")
	}
	dir := filepath.Join(root, "scripts", "self", id)
	names := listDir(t, dir)
	if len(names) != 1 || names[0] != "index.js" {
		t.Errorf("覆盖写入后目录条目不符: %v", names)
	}
	assertNoTempResidue(t, root)
}

func Test自动创建数据根与深层目录(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "root") // 数据根尚不存在
	f := FS{Root: root}
	const content = "dist 内容\n"
	if err := f.WriteDist("demo", content); err != nil {
		t.Fatalf("WriteDist 应自动创建数据根: %v", err)
	}
	got, err := f.ReadDist("demo")
	if err != nil || got != content {
		t.Errorf("读回 = %q (err=%v)", got, err)
	}
	assertNoTempResidue(t, root)
}

func TestDist与文档写读(t *testing.T) {
	root := t.TempDir()
	f := FS{Root: root}
	const dist = "// dist 内容\n"
	if err := f.WriteDist("demo", dist); err != nil {
		t.Fatalf("WriteDist 报错: %v", err)
	}
	got, err := f.ReadDist("demo")
	if err != nil || got != dist {
		t.Errorf("ReadDist = %q (err=%v)", got, err)
	}
	if p := f.DistPath("demo"); p != "dist/demo.user.js" {
		t.Errorf("DistPath = %q", p)
	}

	const doc = "# 文档\n\n这是说明。\n"
	selfID := "my-doc-script" // self id（非 synced 形态），DocPath 非空
	if err := f.WriteDoc(selfID, doc); err != nil {
		t.Fatalf("WriteDoc 报错: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.DocPath(selfID))))
	if err != nil {
		t.Fatalf("文档文件未生成: %v", err)
	}
	if string(b) != doc {
		t.Errorf("文档内容 = %q，期望 %q", string(b), doc)
	}
	assertNoTempResidue(t, root)
}

func Test对Synced无文档路径(t *testing.T) {
	const syncedID = "0123456789ab" // 12 位小写 hex = synced 形态
	f := FS{}
	if f.DocPath(syncedID) != "" {
		t.Errorf("synced 的 DocPath 应为空串，得到 %q", f.DocPath(syncedID))
	}
	root := t.TempDir()
	fr := FS{Root: root}
	if err := fr.WriteDoc(syncedID, "x"); err == nil {
		t.Error("对 synced 写文档应报错（调用方须先判断）")
	}
	// 移除则幂等返回 nil：/rm 对 synced 脚本无条件调用不致失败
	if err := fr.RemoveDoc(syncedID); err != nil {
		t.Errorf("对 synced 删文档应幂等 nil，实际: %v", err)
	}
}

func Test移除幂等与目录回收(t *testing.T) {
	root := t.TempDir()
	f := FS{Root: root}
	selfID := "d1"
	syncedID := "0123456789ab"

	// self：源码 + 文档同目录
	if err := f.WriteSource(selfID, registry.TypeSelf, "src"); err != nil {
		t.Fatalf("WriteSource 报错: %v", err)
	}
	if err := f.WriteDoc(selfID, "# doc"); err != nil {
		t.Fatalf("WriteDoc 报错: %v", err)
	}
	selfDir := filepath.Join(root, "scripts", "self", selfID)

	// RemoveSource 只删源码，文档不被连带删除（目录非空 → 保留）
	if err := f.RemoveSource(selfID, registry.TypeSelf); err != nil {
		t.Fatalf("RemoveSource 报错: %v", err)
	}
	notExist(t, filepath.Join(selfDir, "index.js"), "源码文件")
	if _, err := os.Stat(filepath.Join(selfDir, "README.md")); err != nil {
		t.Errorf("文档不应被连带删除: %v", err)
	}
	// 重复移除 → nil（幂等）
	if err := f.RemoveSource(selfID, registry.TypeSelf); err != nil {
		t.Errorf("重复 RemoveSource 应 nil，实际: %v", err)
	}

	// RemoveDoc 删文档后，空目录被回收；重复移除 → nil
	if err := f.RemoveDoc(selfID); err != nil {
		t.Fatalf("RemoveDoc 报错: %v", err)
	}
	notExist(t, filepath.Join(selfDir, "README.md"), "文档文件")
	notExist(t, selfDir, "空的脚本目录")
	if err := f.RemoveDoc(selfID); err != nil {
		t.Errorf("重复 RemoveDoc 应 nil，实际: %v", err)
	}

	// synced：删源码即回收目录
	if err := f.WriteSource(syncedID, registry.TypeSynced, "upstream"); err != nil {
		t.Fatalf("WriteSource 报错: %v", err)
	}
	syncedDir := filepath.Join(root, "scripts", "synced", syncedID)
	if err := f.RemoveSource(syncedID, registry.TypeSynced); err != nil {
		t.Fatalf("RemoveSource 报错: %v", err)
	}
	notExist(t, syncedDir, "synced 源码目录")
	if err := f.RemoveSource(syncedID, registry.TypeSynced); err != nil {
		t.Errorf("重复 RemoveSource 应 nil，实际: %v", err)
	}

	// dist：文件删除幂等，且不回收共享的 dist/ 目录
	if err := f.WriteDist(selfID, "dist"); err != nil {
		t.Fatalf("WriteDist 报错: %v", err)
	}
	if err := f.RemoveDist(selfID); err != nil {
		t.Fatalf("RemoveDist 报错: %v", err)
	}
	notExist(t, filepath.Join(root, "dist", selfID+".user.js"), "dist 文件")
	if err := f.RemoveDist(selfID); err != nil {
		t.Errorf("重复 RemoveDist 应 nil，实际: %v", err)
	}
	if names := listDir(t, filepath.Join(root, "dist")); len(names) != 0 {
		t.Errorf("dist/ 目录本身不应被回收，残留: %v", names)
	}
}

func Test非法输入返回错误不Panic(t *testing.T) {
	root := t.TempDir()
	f := FS{Root: root}

	// 未知脚本类型
	if err := f.WriteSource("x", "bogus", "c"); err == nil {
		t.Error("未知类型写入应报错")
	}
	if _, err := f.ReadSource("x", "bogus"); err == nil {
		t.Error("未知类型读取应报错")
	}
	if err := f.RemoveSource("x", "bogus"); err == nil {
		t.Error("未知类型移除应报错")
	}

	// resolve 反斜杠拒绝
	if _, err := resolve(root, "bad\\path"); err == nil {
		t.Error("resolve 反斜杠路径应报错")
	}
	// resolve 空段拒绝（相对路径）
	if _, err := resolve(root, "a//b"); err == nil {
		t.Error("resolve 空段路径应报错")
	}
	// resolve ".." 拒绝
	if _, err := resolve(root, "a/../b"); err == nil {
		t.Error("resolve .. 路径应报错")
	}
	// resolve 成功路径
	got, err := resolve(root, "scripts/self/demo/index.js")
	if err != nil {
		t.Fatalf("resolve 成功路径应无 error: %v", err)
	}
	if !strings.HasSuffix(got, "scripts\\self\\demo\\index.js") && !strings.HasSuffix(got, "scripts/self/demo/index.js") {
		t.Errorf("resolve 路径不符: %s", got)
	}

	// 路径逃逸：id 含 .. 段
	if err := f.WriteSource("..", registry.TypeSelf, "c"); err == nil {
		t.Error("id 越界应报错")
	}
	if err := f.WriteSource("../../evil", registry.TypeSelf, "c"); err == nil {
		t.Error("id 越界应报错")
	}
	if err := f.RemoveSource("..", registry.TypeSelf); err == nil {
		t.Error("越界移除应报错")
	}
	notExist(t, filepath.Join(root, "scripts", "index.js"), "越界文件")
	notExist(t, filepath.Join(root, "..", "evil"), "逃逸文件")

	// 数据根本身是文件 → 创建目录失败，错误返回而非 panic
	fz := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(fz, []byte("x"), 0o644); err != nil {
		t.Fatalf("准备夹具失败: %v", err)
	}
	if err := (FS{Root: fz}).WriteSource("x", registry.TypeSelf, "c"); err == nil {
		t.Error("数据根为文件时应报错")
	}

	// 读取不存在的文件 → error
	if _, err := f.ReadSource("never", registry.TypeSelf); err == nil {
		t.Error("读取不存在的源码应报错")
	}
	if _, err := f.ReadDist("never"); err == nil {
		t.Error("读取不存在的 dist 应报错")
	}
}
