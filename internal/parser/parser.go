// Package parser 实现命令面板评论的解析：首行 `/cmd args` 与围栏代码块提取。
//
// 行为契约（SPEC-CLI §1 判定顺序第 3 步的上游）：
//   - 命令来自首个非空行，必须以 `/` 开头；
//   - 围栏代码块（``` 或 ~~~）可出现在正文任意位置，供 `/add` 等命令取源码；
//   - RemoveCodeBlocks 用于生成投影/统计时剥离源码块。
package parser

import (
	"strings"
)

// Comment 解析结果。
type Comment struct {
	Raw     string   // 原文
	Command string   // 首个 token，不含前导 /
	Args    string   // 首行剩余部分（trim 后）
	Code    []string // 围栏代码块内容（按出现顺序，不含围栏行）
	Ok      bool     // 是否识别出命令
}

// Parse 解析评论。Ok=false 表示「未识别命令」（调用方回帖文案，不报错）。
func Parse(body string) Comment {
	c := Comment{Raw: body}
	first, ok := firstLine(body)
	if !ok || !strings.HasPrefix(first, "/") {
		c.Code = ExtractCodeBlocks(body)
		return c
	}
	line := strings.TrimSpace(strings.TrimPrefix(first, "/"))
	if line == "" {
		return c // 只有 "/"，视为未识别
	}
	// 命令字符集：字母/数字/-（不吞中文与标点，防止 "/你好" 被当命令）
	i := 0
	for i < len(line) {
		ch := line[i]
		isCmdChar := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-'
		if !isCmdChar {
			break
		}
		i++
	}
	if i == 0 {
		c.Code = ExtractCodeBlocks(body)
		return c
	}
	c.Command = line[:i]
	c.Args = strings.TrimSpace(line[i:])
	c.Code = ExtractCodeBlocks(body)
	c.Ok = true
	return c
}

// firstLine 返回首个非空白行（不含换行符）。
func firstLine(body string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line), true
		}
	}
	return "", false
}

const fenceChars = "`~"

// ExtractCodeBlocks 提取全部围栏代码块内容（有序）。
// 规则：围栏行 = 可选缩进 + 至少 3 个 ` 或 ~（同一字符）+ 可选信息串；
// 关闭围栏必须同字符且长度不小于开启围栏；未闭合则截取到文末。
func ExtractCodeBlocks(body string) []string {
	var blocks []string
	lines := strings.Split(body, "\n")
	i := 0
	for i < len(lines) {
		fence, ch, ok := openFence(strings.TrimRight(lines[i], "\r"))
		if !ok {
			i++
			continue
		}
		var sb strings.Builder
		i++
		closed := false
		for i < len(lines) {
			line := strings.TrimRight(lines[i], "\r")
			if closeFence(line, ch, len(fence)) {
				closed = true
				i++
				break
			}
			sb.WriteString(line)
			sb.WriteByte('\n')
			i++
		}
		_ = closed // 未闭合视作延伸到文末
		blocks = append(blocks, strings.TrimRight(sb.String(), "\n"))
	}
	return blocks
}

// RemoveCodeBlocks 移除全部围栏代码块，保留其余文本。
func RemoveCodeBlocks(body string) string {
	lines := strings.Split(body, "\n")
	var out []string
	i := 0
	for i < len(lines) {
		raw := strings.TrimRight(lines[i], "\r")
		_, ch, ok := openFence(raw)
		if !ok {
			out = append(out, raw)
			i++
			continue
		}
		fenceLen := fenceRunLen(raw, ch)
		i++
		for i < len(lines) {
			line := strings.TrimRight(lines[i], "\r")
			if closeFence(line, ch, fenceLen) {
				i++
				break
			}
			i++
		}
	}
	return strings.Join(out, "\n")
}

// openFence 判断是否为开启围栏行，返回围栏字符序列、字符与是否成立。
func openFence(line string) (fence string, ch byte, ok bool) {
	t := strings.TrimLeft(line, " \t")
	if t == "" {
		return "", 0, false
	}
	c := t[0]
	if !strings.ContainsRune(fenceChars, rune(c)) {
		return "", 0, false
	}
	n := fenceRunLen(t, c)
	if n < 3 {
		return "", 0, false
	}
	return t[:n], c, true
}

func fenceRunLen(s string, ch byte) int {
	n := 0
	for n < len(s) && s[n] == ch {
		n++
	}
	return n
}

// closeFence：同字符、长度 ≥ 开启围栏、其后只能是空白。
func closeFence(line string, ch byte, openLen int) bool {
	t := strings.TrimLeft(line, " \t")
	n := fenceRunLen(t, ch)
	if n < openLen {
		return false
	}
	return strings.TrimSpace(t[n:]) == ""
}
