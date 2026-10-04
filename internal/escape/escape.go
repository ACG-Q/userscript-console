// Package escape 提供 Markdown 输出所需的最小转义工具。
//
// DR-6/DR-7 口径：HTML 转义由 html/template 上下文自动转义承担（I-3），
// 本包只保留 Python `escaping.py` 中仍需要的表格单元格转义语义。
package escape

import "strings"

// EscapeMdCell 转义 Markdown 表格单元格内容，语义对齐 Python escaping.escape_md_cell：
//
//   - \r\n → 单个空格
//   - 裸 \r / \n → 空格
//   - | → \|
//
// 顺序敏感：先归一换行再转义竖线，保证「换行折叠发生在转义之前」，
// 不会产生跨行的伪转义序列。
func EscapeMdCell(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return s
}

// EscapeMdText 转义任意 Markdown 行内文本中的竖线（不折叠换行）。
// 供不需要表格语义的场景（如链接文本）使用。
func EscapeMdText(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
