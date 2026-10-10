package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/acg-q/userscript-console/internal/cli"
	"github.com/acg-q/userscript-console/internal/layout"
)

// assemblePages 把 dist 产物按旧 assemble_site.py 语义搬入 pagesOut：
// *.user.js → <pagesOut>/<dist 段>/，其余（HTML/JSON/文档目录）→ <pagesOut>/ 根。
// 搬移后 dist 目录清空；pagesOut 为空由调用方跳过（纯 build 模式）。
// 相对 pagesOut 相对数据根解析（SPEC-CLI §0.1：只读写数据根内文件），绝对值原样使用。
// 移动优先 os.Rename，跨设备（EXDEV）回落 copy+remove。
func assemblePages(root string, lay layout.Layout, pagesOut string) (int, error) {
	if !filepath.IsAbs(pagesOut) {
		pagesOut = filepath.Join(root, filepath.FromSlash(pagesOut))
	}
	distDir := lay.DistPath(root)
	entries, err := os.ReadDir(distDir)
	if err != nil {
		return 0, fmt.Errorf("读取 dist 目录失败: %w", err)
	}
	if len(entries) == 0 {
		return 0, errors.New("dist/ 为空：build 可能未生成任何产物")
	}

	// user.js 落 <pagesOut>/<dist 段>；dist 为绝对路径时不能直接当相对段
	// （会逃出 pagesOut），改用 dist 目录名（设计 §2 D6 组合规则）。
	distSeg := lay.Dist
	if filepath.IsAbs(distSeg) {
		distSeg = filepath.Base(distDir)
	}
	jsDir := filepath.Join(pagesOut, filepath.FromSlash(distSeg))
	if err := os.MkdirAll(pagesOut, 0o755); err != nil {
		return 0, fmt.Errorf("创建 pages-out 目录失败: %w", err)
	}
	if err := os.MkdirAll(jsDir, 0o755); err != nil {
		return 0, fmt.Errorf("创建分发子目录失败: %w", err)
	}

	moved := 0
	for _, e := range entries {
		dstDir := pagesOut
		if strings.HasSuffix(e.Name(), ".user.js") {
			dstDir = jsDir
		}
		if err := movePath(filepath.Join(distDir, e.Name()), filepath.Join(dstDir, e.Name())); err != nil {
			return moved, err
		}
		moved++
	}
	return moved, nil
}

// pagesOutFrom 解析站点搬移目标：--pages-out > env USM_PAGES_OUT > 空（不搬移）。
func pagesOutFrom(args []string) string {
	if v := flagVal(args, "--pages-out"); v != "" {
		return v
	}
	return cli.EnvOr("USM_PAGES_OUT", "")
}

// movePath 优先 os.Rename；跨设备（EXDEV）回落 copy+remove（目录递归）。
func movePath(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("移动 %s → %s 失败: %w", src, dst, err)
	}
	if err := copyPath(src, dst); err != nil {
		return fmt.Errorf("跨设备复制 %s → %s 失败: %w", src, dst, err)
	}
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("删除源 %s 失败: %w", src, err)
	}
	return nil
}

// copyPath 递归复制文件/目录（保留权限位）。
func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		children, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, c := range children {
			if err := copyPath(filepath.Join(src, c.Name()), filepath.Join(dst, c.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
