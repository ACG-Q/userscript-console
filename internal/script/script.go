// Package script 提供用户脚本的文本层与数据根文件层公共工具（职责参照 Python utils.py）：
//
//   - 元数据头：ExtractMeta / BuildHeader / EnsureURLs / SyncVersion（头解析与渲染复用 internal/meta，不重复造轮子）；
//   - 版本演进：IncrementVersion（分段数字自增，容忍 v 前缀）；
//   - changelog：PrependChangelog（新→旧头插，同版本幂等）；
//   - 路径契约：SPEC-DATA §2.1 的相对 --root 路径；
//   - 原子文件 IO：同目录临时文件 + rename，MkdirAll 0o755、文件 0o644，移除幂等；
//   - Format：D-03（JS 美化）待定·不阻塞，保留 stub。

// 依赖方向：registry（类型）→ meta（头解析/渲染）→ 标准库；本包不做网络与业务编排。
package script

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/acg-q/userscript-console/internal/meta"
	"github.com/acg-q/userscript-console/internal/registry"
)

// ── 元数据头 ────────────────────────────────────────────────

// ExtractMeta 从脚本源码提取结构化元数据头（包装 meta.Parse）。
// 找不到完整头块 → ok=false。
func ExtractMeta(src string) (meta.Header, bool) {
	return meta.Parse(src)
}

// BuildHeader 把 registry.Script 渲染为规范头块（含开闭行）。
// Name/Version/Description/Author/Namespace/Match/Grant 取自 s；
// DownloadURL/UpdateURL 不在此处填充——由调用方经 EnsureURLs 注入。
func BuildHeader(s registry.Script) string {
	h := meta.Header{
		Name:        s.Name,
		Namespace:   s.Namespace,
		Version:     s.Version,
		Description: s.Description,
		Author:      s.Author,
		Match:       s.Match,
		Grant:       s.Grant,
	}
	return h.Render()
}

// EnsureURLs 确保头块内 @downloadURL/@updateURL 正确：
//   - 值已正确 → 原行不动（保留原有空格/对齐风格）；
//   - 值错误 → 整行规范替换（保序、保原行 \r）；
//   - 缺失 → 在闭合行前补一行（换行风格跟随 src）；
//   - 传入空串的 URL 视为「不作为」（既不替换也不添加）；
//   - 无头块 → 原样返回。
//
// 幂等：两次调用结果一致（测试断言）。
func EnsureURLs(src, downloadURL, updateURL string) string {
	lines := strings.Split(src, "\n")
	start, end, ok := findHeader(lines)
	if !ok {
		return src
	}
	crlf := strings.Contains(src, "\r\n")
	lines, end = setHeaderLine(lines, start, end, "downloadURL", downloadURL, crlf)
	lines, _ = setHeaderLine(lines, start, end, "updateURL", updateURL, crlf)
	return strings.Join(lines, "\n")
}

// SyncVersion 把头块内 @version 行替换为新值：
//   - 版本行缺失 → 在闭合行前补一行；
//   - 无头块 → 原样返回；
//   - version 为空串 → 不作为（原样返回，避免把版本刷空）。
//
// 幂等。
func SyncVersion(src, version string) string {
	if version == "" {
		return src
	}
	lines := strings.Split(src, "\n")
	start, end, ok := findHeader(lines)
	if !ok {
		return src
	}
	lines, _ = setHeaderLine(lines, start, end, "version", version, strings.Contains(src, "\r\n"))
	return strings.Join(lines, "\n")
}

// ── 头块行操作（与 meta.Parse 同规则定位，避免二次解析口径漂移） ──

const (
	openToken  = "==UserScript=="
	closeToken = "==/UserScript=="
)

// findHeader 定位头块开/闭行下标（规则与 meta.Parse 一致）。找不到完整块 → ok=false。
func findHeader(lines []string) (start, end int, ok bool) {
	start, end = -1, -1
	for i, ln := range lines {
		t := strings.TrimSpace(strings.TrimSuffix(ln, "\r"))
		if start < 0 && strings.Contains(t, openToken) {
			start = i
			continue
		}
		if start >= 0 && strings.Contains(t, closeToken) {
			end = i
			break
		}
	}
	if start < 0 || end < 0 {
		return -1, -1, false
	}
	return start, end, true
}

// lineKV 解析头内一行的键值（与 meta.Parse 相同的去注释/@ 前缀/分词规则）。
// 返回小写 key；非 @ 行 → ok=false。
func lineKV(ln string) (key, val string, ok bool) {
	t := strings.TrimSpace(strings.TrimSuffix(ln, "\r"))
	t = strings.TrimPrefix(t, "//")
	t = strings.TrimSpace(t)
	if !strings.HasPrefix(t, "@") {
		return "", "", false
	}
	t = t[1:]
	sp := strings.IndexAny(t, " \t")
	if sp < 0 {
		return strings.ToLower(t), "", true
	}
	return strings.ToLower(t[:sp]), strings.TrimSpace(t[sp+1:]), true
}

// setHeaderLine 在头块内设置 @key=val：
//   - 值相同 → 原行不动；
//   - 值不同 → 规范替换（保留该行原有 \r）；
//   - 缺失 → 在闭合行前插入（crlf 决定新行行尾）；
//   - val 为空 → 不作为。
//
// 返回更新后的行切片与新的闭合行下标。
func setHeaderLine(lines []string, start, end int, key, val string, crlf bool) ([]string, int) {
	if val == "" {
		return lines, end
	}
	low := strings.ToLower(key)
	found := false
	for i := start + 1; i < end; i++ {
		k, cur, ok := lineKV(lines[i])
		if !ok || k != low {
			continue
		}
		found = true
		if cur == val {
			continue // 值已正确：保持原行（含对齐空格风格）
		}
		lines[i] = canonicalLine(key, val, strings.HasSuffix(lines[i], "\r"))
	}
	if found {
		return lines, end
	}
	return slices.Insert(lines, end, canonicalLine(key, val, crlf)), end + 1
}

// canonicalLine 生成规范头行 "// @key val"；crlf=true 时保留 \r 行尾。
func canonicalLine(key, val string, crlf bool) string {
	s := "// @" + key + " " + val
	if crlf {
		s += "\r"
	}
	return s
}

// ── 版本演进 ────────────────────────────────────────────────

// IncrementVersion 分段版本号自增：按 '.' 分段，所有段必须为纯数字，仅末段 +1，
// 段数保持不变；容忍前缀 v/V（原样保留，"v1.2.3"→"v1.2.4"）；
// 不补零（"1.09"→"1.10"）；进位自然发生（"1.9"→"1.10"）。
// 空串、去除前缀后为空、含非数字/空分段、末段溢出 → error（不 panic）。
func IncrementVersion(v string) (string, error) {
	orig := v
	if v == "" {
		return "", errors.New("script: 版本号为空")
	}
	prefix := ""
	if v[0] == 'v' || v[0] == 'V' {
		prefix = v[:1]
		v = v[1:]
	}
	if v == "" {
		return "", fmt.Errorf("script: 版本号 %q 仅有前缀无数字", orig)
	}
	segs := strings.Split(v, ".")
	for _, seg := range segs {
		if seg == "" || !allDigits(seg) {
			return "", fmt.Errorf("script: 版本号 %q 含非数字分段 %q", orig, seg)
		}
	}
	last := segs[len(segs)-1]
	n, err := strconv.Atoi(last)
	if err != nil {
		return "", fmt.Errorf("script: 版本号 %q 末段数值溢出: %w", orig, err)
	}
	segs[len(segs)-1] = strconv.Itoa(n + 1)
	return prefix + strings.Join(segs, "."), nil
}

// allDigits 仅接受 ASCII 数字（"+"、"-"、空白、非 ASCII 数字均拒绝）。
func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// ── changelog（新→旧） ─────────────────────────────────────

// PrependChangelog 头插一条 changelog（列表约定新→旧）：
//   - 头部已为同 version → 原样返回（幂等，不重复追加）；
//   - 否则插入头部，其余顺序保持；
//   - date 由调用方传入，本函数不取 now（可测性）。
//
// 不修改入参切片内容。
func PrependChangelog(list []registry.ChangelogEntry, version, date, note string) []registry.ChangelogEntry {
	if len(list) > 0 && list[0].Version == version {
		return list
	}
	return append([]registry.ChangelogEntry{{Version: version, Date: date, Note: note}}, list...)
}

// ── 路径（相对 --root，SPEC-DATA §2.1；恒用 '/' 分隔，跨平台契约一致） ──

// FS 是带数据根与可配置脚本/分发目录的路径视图（设计 §2.1 D5/D6）。
// Scripts/Dist 零值兜底 "scripts"/"dist"，与 layout.Defaults 一致，
// 保证零值 FS{Root: r} 与历史硬编码行为完全等价。
type FS struct {
	Root    string // 数据根（--root）
	Scripts string // 脚本目录（相对 root 或绝对），零值 → "scripts"
	Dist    string // 分发目录（相对 root 或绝对），零值 → "dist"
}

func (f FS) scripts() string {
	if f.Scripts == "" {
		return "scripts"
	}
	return f.Scripts
}

func (f FS) dist() string {
	if f.Dist == "" {
		return "dist"
	}
	return f.Dist
}

// SelfSourcePath 自写脚本源码路径：<scripts>/self/<id>/index.js。
func (f FS) SelfSourcePath(id string) string {
	return f.scripts() + "/self/" + id + "/index.js"
}

// SyncedSourcePath 同步脚本源码路径：<scripts>/synced/<id>/script.user.js。
func (f FS) SyncedSourcePath(id string) string {
	return f.scripts() + "/synced/" + id + "/script.user.js"
}

// DistPath 分发产物路径：<dist>/<id>.user.js。
func (f FS) DistPath(id string) string {
	return f.dist() + "/" + id + ".user.js"
}

// DocPath 自写脚本文档路径：<scripts>/self/<id>/README.md。
// synced 脚本无文档文件 → 返回空串（synced id = registry.SourceID = URL MD5 前 12 位小写十六进制，
// 依此形态识别；调用方另有 registry.Script.Type 可权威判断，DocPath 仅用于快速取路径）。
func (f FS) DocPath(id string) string {
	if isSyncedID(id) {
		return ""
	}
	return f.scripts() + "/self/" + id + "/README.md"
}

// isSyncedID 判断 id 是否为 synced 形态（12 位小写十六进制，见 registry.SourceID）。
// 已知局限：若 self 脚本 id 恰为 12 位小写 hex 会被误判——self id 为人类可读 slug，正常不重合。
func isSyncedID(id string) bool {
	if len(id) != 12 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ── D-03 stub：JS 美化 ─────────────────────────────────────

// ErrFormatNotImplemented Format 的占位错误（D-03 待定·不阻塞，DR-6）。
var ErrFormatNotImplemented = errors.New("script: format/JS 美化为 stub（D-03 待定·不阻塞）")

// Format JS 美化 stub：原样返回源码与 ErrFormatNotImplemented，
// 供调用方降级（/add /up 的美化输出暂为原文）。拍板 D-03 后再实现，签名不变。
func Format(src string) (string, error) {
	return src, ErrFormatNotImplemented
}
