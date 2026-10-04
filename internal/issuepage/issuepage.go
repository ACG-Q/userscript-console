// Package issuepage 生成命令面板 Issue / 版本帖的标题与正文。
//
// 不变量（SPEC-ARCH-TEST I-1）：同输入两次输出必须一致（确定性）；
// 投影判等的比较对象是工具自己的上次输出（一次性重写已接受，DR-6）。
// 首行 marker 契约：Issue 正文首行恰为 `<!-- script-id: <id> -->`，
// 墓碑正文**绝不**带 marker（投影器据 marker 判断是否升级为墓碑）。
package issuepage

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/acg-q/userscript-console/internal/escape"
	"github.com/acg-q/userscript-console/internal/registry"
)

const (
	markerPrefix = "<!-- script-id: "
	markerSuffix = " -->"
)

// markerRe 用于 HasMarker：id 不含空白与尖括号。
var markerRe = regexp.MustCompile(`<!-- script-id: ([^<>\s]+) -->`)

// Marker 首行标记。
func Marker(id string) string { return markerPrefix + id + markerSuffix }

// validID id 合法性：非空、无空白/尖括号、不含 "--"（防注释提前终止）。
func validID(id string) bool {
	if id == "" || strings.Contains(id, "--") || strings.ContainsAny(id, "<>") {
		return false
	}
	for _, r := range id {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// ScriptIDFromBody 仅当 body **首行**恰为合法 marker 时返回 id。
func ScriptIDFromBody(body string) (string, bool) {
	first, _, _ := strings.Cut(body, "\n")
	first = strings.TrimSpace(strings.TrimRight(first, "\r"))
	if !strings.HasPrefix(first, markerPrefix) || !strings.HasSuffix(first, markerSuffix) {
		return "", false
	}
	if len(first) < len(markerPrefix)+len(markerSuffix) {
		return "", false
	}
	id := first[len(markerPrefix) : len(first)-len(markerSuffix)]
	if !validID(id) || first != Marker(id) {
		return "", false
	}
	return id, true
}

// HasMarker 任意位置存在合法 marker。
func HasMarker(body string) bool {
	m := markerRe.FindStringSubmatch(body)
	return m != nil && validID(m[1])
}

// BuildTitle 标题（确定性；状态后缀顺序固定：停用 → 删除）。
func BuildTitle(s registry.Script) string {
	t := s.Name + " · v" + s.Version
	if !s.Enabled {
		t += " [已停用]"
	}
	if s.Deleted {
		t += " [已删除]"
	}
	return t
}

// TombstoneTitle 墓碑标题：强制 Deleted 后复用 BuildTitle（对已删除脚本与 BuildTitle 恒等）。
func TombstoneTitle(s registry.Script) string {
	s.Deleted = true
	return BuildTitle(s)
}

// BuildBody Issue 正文：首行为 marker，其后 Markdown（表格单元格全部过 EscapeMdCell）。
// 非表格文本不转义（描述等原文必须可读，Contains 类断言成立）。
func BuildBody(s registry.Script) string {
	var b strings.Builder
	b.WriteString(Marker(s.ID))
	b.WriteString("\n\n")

	fmt.Fprintf(&b, "## %s · v%s\n\n", s.Name, s.Version)
	b.WriteString("- **作者**：" + s.Author + "\n")
	if s.Description != "" {
		b.WriteString("- **描述**：" + s.Description + "\n")
	}
	fmt.Fprintf(&b, "- **ID**：`%s`\n", s.ID)
	if s.Namespace != "" {
		b.WriteString("- **命名空间**：" + s.Namespace + "\n")
	}
	fmt.Fprintf(&b, "- **类型**：%s\n", s.Type)
	b.WriteString("- **状态**：" + enabledLabel(s) + "\n")
	if s.Deleted {
		b.WriteString("- **软删除**：已删除（源码与安装包已移除，条目保留）\n")
	}
	if s.SourceURL != nil && *s.SourceURL != "" {
		fmt.Fprintf(&b, "- **来源**：%s\n", *s.SourceURL)
	}

	b.WriteString("\n### 安装规则（match）\n\n")
	if len(s.Match) == 0 {
		b.WriteString("无\n")
	} else {
		b.WriteString("| # | 规则 |\n|---|---|\n")
		for i, m := range s.Match {
			fmt.Fprintf(&b, "| %d | %s |\n", i+1, escape.EscapeMdCell(m))
		}
	}

	if len(s.Changelog) > 0 {
		n := len(s.Changelog)
		shown := n
		if shown > 3 {
			shown = 3
		}
		b.WriteString("\n### 更新日志（新 → 旧）\n\n")
		b.WriteString("| 版本 | 日期 | 说明 |\n|---|---|---|\n")
		for _, c := range s.Changelog[:shown] {
			fmt.Fprintf(&b, "| %s | %s | %s |\n",
				escape.EscapeMdCell(c.Version), escape.EscapeMdCell(c.Date), escape.EscapeMdCell(c.Note))
		}
		if n > shown {
			fmt.Fprintf(&b, "\n（共 %d 条，仅显示最近 %d 条）\n", n, shown)
		}
	}
	return b.String()
}

func enabledLabel(s registry.Script) string {
	st := "已启用"
	if !s.Enabled {
		st = "已停用"
	}
	if s.Deleted {
		st += " · 已软删除"
	}
	return st
}

// TombstoneBody 墓碑正文：**不含 marker、不含 "script-id" 字样、不写原始 ID**。
func TombstoneBody(s registry.Script) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s · v%s（已下架）\n\n", s.Name, s.Version)
	b.WriteString("> 该脚本已从控制台下架（软删除）：源码与安装包已移除，注册条目保留以支持同源复活。\n\n")
	fmt.Fprintf(&b, "- **名称**：%s\n", s.Name)
	fmt.Fprintf(&b, "- **版本**：%s\n", s.Version)
	b.WriteString("- **状态**：已软删除\n")
	return b.String()
}

// DiscussionTitle 版本帖标题。
func DiscussionTitle(s registry.Script, version string) string {
	return s.Name + " · v" + version + " 版本讨论"
}

// DiscussionBody 版本帖正文：不带 script marker；按 version 取 changelog 说明。
func DiscussionBody(s registry.Script, version string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s · v%s\n\n", s.Name, version)
	fmt.Fprintf(&b, "脚本 **%s** 的 `v%s` 版本讨论（面板 Issue #%s）。\n\n", s.Name, version, issueRef(s))
	b.WriteString("### 本版本更新\n\n")
	note := "暂无该版本的更新说明。"
	for _, c := range s.Changelog {
		if c.Version == version {
			note = c.Note
			break
		}
	}
	b.WriteString(note)
	b.WriteString("\n")
	return b.String()
}

func issueRef(s registry.Script) string {
	if s.Issue != nil && s.Issue.Number > 0 {
		return fmt.Sprintf("%d", s.Issue.Number)
	}
	return "—"
}
