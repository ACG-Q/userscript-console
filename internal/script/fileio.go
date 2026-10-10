package script

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/acg-q/userscript-console/internal/registry"
)

// ── 原子文件 IO（相对 --root；SPEC-DATA §2.1） ──────────────
//
// 写入：MkdirAll 0o755 → 同目录唯一临时文件 → sync → close → chmod 0o644 → rename；
// 失败路径尽力清理临时文件（defer remove，rename 成功后该调用无效果）。
// 移除：文件不存在 → nil（幂等，不报错）。
// 一切错误以 error 返回，不 panic。

// WriteSource 原子写入脚本源码：
// self → <scripts>/self/<id>/index.js；synced → <scripts>/synced/<id>/script.user.js。
// scriptType 非法、id 含非法路径段 → error。
func (f FS) WriteSource(id, scriptType, content string) error {
	rel, err := f.sourceRelPath(id, scriptType)
	if err != nil {
		return err
	}
	return writeFile(f.Root, rel, content)
}

// ReadSource 读取脚本源码（路径规则同 WriteSource）。
func (f FS) ReadSource(id, scriptType string) (string, error) {
	rel, err := f.sourceRelPath(id, scriptType)
	if err != nil {
		return "", err
	}
	return readFile(f.Root, rel)
}

// WriteDist 原子写入分发产物 <dist>/<id>.user.js。
func (f FS) WriteDist(id, content string) error {
	return writeFile(f.Root, f.DistPath(id), content)
}

// ReadDist 读取分发产物 <dist>/<id>.user.js。
func (f FS) ReadDist(id string) (string, error) {
	return readFile(f.Root, f.DistPath(id))
}

// WriteDoc 写自写脚本文档 <scripts>/self/<id>/README.md。
// synced 脚本无文档（DocPath 返回空串）→ error：调用方须先以类型/DocPath 判断，勿对 synced 调用。
func (f FS) WriteDoc(id, content string) error {
	rel := f.DocPath(id)
	if rel == "" {
		return errors.New("script: synced 脚本无文档文件（调用方须先以类型或 DocPath 判断）")
	}
	return writeFile(f.Root, rel, content)
}

// RemoveSource 移除脚本源码（连同其空目录）；文件/目录不存在 → nil（幂等）。
// 目录非空（如自写脚本的 README 仍在）则保留目录，不连带删除文档。
func (f FS) RemoveSource(id, scriptType string) error {
	rel, err := f.sourceRelPath(id, scriptType)
	if err != nil {
		return err
	}
	return removeFile(f.Root, rel, true)
}

// RemoveDist 移除分发产物；不存在 → nil（幂等）。
// <dist> 为多脚本共享目录，绝不回收 dist/ 本身。
func (f FS) RemoveDist(id string) error {
	return removeFile(f.Root, f.DistPath(id), false)
}

// RemoveDoc 移除自写脚本文档；synced（DocPath 空串）与文件不存在均 → nil（幂等，
// /rm 对 synced 脚本无条件调用不致失败）。目录清空后尽力回收。
func (f FS) RemoveDoc(id string) error {
	rel := f.DocPath(id)
	if rel == "" {
		return nil
	}
	return removeFile(f.Root, rel, true)
}

// sourceRelPath 按脚本类型给出源码相对路径；未知类型 → error。
func (f FS) sourceRelPath(id, scriptType string) (string, error) {
	switch scriptType {
	case registry.TypeSelf:
		return f.SelfSourcePath(id), nil
	case registry.TypeSynced:
		return f.SyncedSourcePath(id), nil
	default:
		return "", fmt.Errorf("script: 未知脚本类型 %q（只允许 %s|%s）", scriptType, registry.TypeSelf, registry.TypeSynced)
	}
}

// resolve 把 '/' 分隔的相对路径拼到数据根之下，并拒绝非法段（"."、".."、反斜杠；相对路径还拒绝空段），
// 防御畸形 id 把写入/删除带出 --root；相对路径的最终结果越出数据根同样拒绝（错误返回，不 panic）。
// 绝对路径（设计 §2 路径规则：自定义 scripts/dist 可为绝对值）经段校验后原样使用。
func resolve(root, rel string) (string, error) {
	if strings.Contains(rel, `\`) {
		return "", fmt.Errorf("script: 非法相对路径 %q（不允许反斜杠）", rel)
	}
	segs := strings.Split(rel, "/")
	for _, seg := range segs {
		if seg == ".." || seg == "." {
			return "", fmt.Errorf("script: 非法相对路径 %q（含非法段 %q）", rel, seg)
		}
	}
	fromSlash := filepath.FromSlash(rel)
	if filepath.IsAbs(fromSlash) {
		return fromSlash, nil
	}
	for _, seg := range segs {
		if seg == "" {
			return "", fmt.Errorf("script: 非法相对路径 %q（含空段）", rel)
		}
	}
	p := filepath.Join(root, fromSlash)
	// 双保险：即便上游路径规则变化，相对结果也不得越出数据根。
	if r, err := filepath.Rel(root, p); err != nil ||
		r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("script: 路径 %q 越出数据根 %q", rel, root)
	}
	return p, nil
}

// writeFile 原子写入：同目录临时文件 + rename；目录 0o755、文件 0o644。
func writeFile(root, rel, content string) error {
	p, err := resolve(root, rel)
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("script: 创建目录 %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(p)+".tmp-*")
	if err != nil {
		return fmt.Errorf("script: 创建临时文件 %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // rename 成功后该调用无效果
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("script: 写入 %s: %w", rel, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("script: 刷新 %s: %w", rel, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("script: 关闭 %s: %w", rel, err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("script: 设置权限 %s: %w", rel, err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		return fmt.Errorf("script: 落盘 %s: %w", rel, err)
	}
	return nil
}

// readFile 读取相对路径文件；不存在/越界 → error。
func readFile(root, rel string) (string, error) {
	p, err := resolve(root, rel)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("script: 读取 %s: %w", rel, err)
	}
	return string(b), nil
}

// removeFile 删除相对路径文件；不存在 → nil（幂等）。
// dropEmptyDir=true 时尽力回收空的脚本目录：目录非空（文档仍在）或已不存在则忽略错误。
func removeFile(root, rel string, dropEmptyDir bool) error {
	p, err := resolve(root, rel)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("script: 删除 %s: %w", rel, err)
	}
	if dropEmptyDir {
		_ = os.Remove(filepath.Dir(p))
	}
	return nil
}
