// Package layout 定义 usm 数据布局的可配置路径（设计 §2 D5/D6）。
//
// 四个可配置项（注册表/脚本目录/分发目录/归档文件）满足同一优先级链：
// CLI flag > 环境变量 > 默认值（默认值与历史硬编码完全一致）。
// 相对值相对数据根解析；绝对值原样使用。相对值必须是干净的
// slash 相对路径——拒绝 ".." 与反斜杠，防目录穿越与跨平台歧义。
package layout

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Layout 四个可配置路径；零值经各方法 normalize 兜底为默认布局，
// 保证调用方构造的零值 Layout{} 行为等价于 Defaults()。
type Layout struct {
	Registry string // 注册表文件，默认 "registry.json"
	Scripts  string // 脚本目录，默认 "scripts"
	Dist     string // 分发目录，默认 "dist"
	Archive  string // 命令归档文件，默认 "archive/commands.json"
}

// Defaults 返回与历史硬编码完全一致的默认布局。
func Defaults() Layout {
	return Layout{
		Registry: "registry.json",
		Scripts:  "scripts",
		Dist:     "dist",
		Archive:  "archive/commands.json",
	}
}

// FromEnv 读取 USM_REGISTRY / USM_SCRIPTS_DIR / USM_DIST_DIR / USM_ARCHIVE_PATH，
// 未设置或空值取默认（D6 的 env 层）。不在此校验：校验在 WithFlags 收口。
func FromEnv() Layout {
	d := Defaults()
	return Layout{
		Registry: envOr("USM_REGISTRY", d.Registry),
		Scripts:  envOr("USM_SCRIPTS_DIR", d.Scripts),
		Dist:     envOr("USM_DIST_DIR", d.Dist),
		Archive:  envOr("USM_ARCHIVE_PATH", d.Archive),
	}
}

// WithFlags 用非空 flag 覆盖对应项（flag > env > 默认），并校验四个值。
// 空串 flag 表示"未指定"，保留来源值。
func (l Layout) WithFlags(registry, scripts, dist, archive string) (Layout, error) {
	out := l
	if registry != "" {
		out.Registry = registry
	}
	if scripts != "" {
		out.Scripts = scripts
	}
	if dist != "" {
		out.Dist = dist
	}
	if archive != "" {
		out.Archive = archive
	}
	for _, kv := range []struct{ name, val string }{
		{"--registry", out.Registry},
		{"--scripts-dir", out.Scripts},
		{"--dist-dir", out.Dist},
		{"--archive-path", out.Archive},
	} {
		if err := validate(kv.name, kv.val); err != nil {
			return Layout{}, err
		}
	}
	return out, nil
}

// validate 相对值必须非空、不含 ".."、不含反斜杠（统一用 "/"，见 SPEC-DATA
// 跨平台契约）；绝对值直接放行（设计 §2 路径规则）。
func validate(name, p string) error {
	if p == "" {
		return errors.New(name + " 不能为空")
	}
	if filepath.IsAbs(p) {
		return nil
	}
	if strings.Contains(p, `\`) {
		return errors.New(name + " 必须使用 / 分隔符")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return errors.New(name + " 不得包含 ..")
		}
	}
	return nil
}

// Normalize 兜底零值字段为默认布局，供外部调用方（如 doctor）在使用字段值前收口。
func (l Layout) Normalize() Layout { return l.normalize() }

func (l Layout) normalize() Layout {
	d := Defaults()
	if l.Registry == "" {
		l.Registry = d.Registry
	}
	if l.Scripts == "" {
		l.Scripts = d.Scripts
	}
	if l.Dist == "" {
		l.Dist = d.Dist
	}
	if l.Archive == "" {
		l.Archive = d.Archive
	}
	return l
}

// join 绝对值原样返回；相对值按 root 解析（slash → 系统分隔符）。
func join(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, filepath.FromSlash(p))
}

// RegistryPath 注册表文件的绝对路径。
func (l Layout) RegistryPath(root string) string { return join(root, l.normalize().Registry) }

// ScriptsPath 脚本目录的绝对路径。
func (l Layout) ScriptsPath(root string) string { return join(root, l.normalize().Scripts) }

// DistPath 分发目录的绝对路径。
func (l Layout) DistPath(root string) string { return join(root, l.normalize().Dist) }

// ArchivePath 命令归档文件的绝对路径。
func (l Layout) ArchivePath(root string) string { return join(root, l.normalize().Archive) }

// DistSeg 是 dist 目录出现在站点 URL 中的路径段（Clean 后去首尾 '/'）。
// downloadURL/updateURL 等外部链接由 PagesBase + "/" + DistSeg 拼出，
// 自定义 dist-dir 时分发链接必须跟随（设计 §2 验收）。
func (l Layout) DistSeg() string {
	p := filepath.ToSlash(filepath.Clean(l.normalize().Dist))
	return strings.Trim(p, "/")
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
