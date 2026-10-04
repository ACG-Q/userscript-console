package main

// 本文件覆盖 action.yml 中 $GITHUB_OUTPUT 写入块的等价逻辑。
//
// 背景（真实 CI 事故）：composite action 写多行 output 时若开闭分隔符不一致，
// GitHub 的 file-command 解析器报
//
//	Invalid value. Matching delimiter not found 'USM_EOF_...'
//
// 使整个 step 以 exit code 1 失败。旧写法在 `<<` 后用 `$(date +%s)`，
// 而闭合行是裸 `USM_EOF` —— 两者永不相等。修复方式是只求值一次并复用同一
// `EOF_ID`。这里以 Go 复刻该格式并断言开闭一致，防止回归。

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// ghaFileCommandDelim 模拟 GitHub 的 file-command 解析：
// 返回 name → 是否找到匹配的闭合分隔符。
var ghaDelimRe = regexp.MustCompile(`(?m)^(\w+)<<(\S+)$`)

func parseGHAOutput(raw string) map[string]string {
	out := map[string]string{}
	for _, m := range ghaDelimRe.FindAllStringSubmatch(raw, -1) {
		name, delim := m[1], m[2]
		start := strings.Index(raw, m[0]) + len(m[0]) + 1 // +1 跳过换行
		closing := "\n" + delim
		end := strings.Index(raw[start:], closing)
		if end < 0 {
			continue // 未找到闭合分隔符 —— GitHub 会报 Matching delimiter not found
		}
		out[name] = raw[start : start+end]
	}
	return out
}

// renderOutputBlock 复刻 action.yml 的输出块写法。
// eofOpen/eofClose 分别作为 `<<` 后的分隔符与闭合行，便于构造对照组。
func renderOutputBlock(eofOpen, eofClose, warnings, result string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "changed=false\n")
	fmt.Fprintf(&b, "authorized=true\n")
	fmt.Fprintf(&b, "warnings<<%s\n%s\n%s\n", eofOpen, warnings, eofClose)
	fmt.Fprintf(&b, "result<<%s\n%s\n%s\n", eofOpen, result, eofClose)
	return b.String()
}

// TestGHADelimiterSharedID 修复后的写法：开闭共用同一 EOF_ID，两个块都能解析。
func TestGHADelimiterSharedID(t *testing.T) {
	const id = "USM_EOF_1791114395456386000"
	raw := renderOutputBlock(id, id, "w1\nw2", "line1\nline2")

	parsed := parseGHAOutput(raw)
	if parsed["warnings"] != "w1\nw2" {
		t.Errorf("warnings 块解析错误: %q", parsed["warnings"])
	}
	if parsed["result"] != "line1\nline2" {
		t.Errorf("result 块解析错误: %q", parsed["result"])
	}
	if len(parsed) != 2 {
		t.Errorf("应有 2 个可解析块, 实际 %d: %#v", len(parsed), parsed)
	}
}

// TestGHADelimiterMismatchIsDetected 对照组：旧写法（闭合为裸 USM_EOF）
// 必须解析失败 —— 证明本测试确实能捕获 CI 上的真实故障。
func TestGHADelimiterMismatchIsDetected(t *testing.T) {
	raw := renderOutputBlock("USM_EOF_1791114395", "USM_EOF", "w1\nw2", "line1\nline2")

	parsed := parseGHAOutput(raw)
	if _, ok := parsed["warnings"]; ok {
		t.Error("旧写法不应解析成功（闭合分隔符与起始不一致）")
	}
	if _, ok := parsed["result"]; ok {
		t.Error("旧写法不应解析成功（闭合分隔符与起始不一致）")
	}
	if len(parsed) != 0 {
		t.Errorf("旧写法应产生 0 个可解析块, 实际 %d", len(parsed))
	}
}

// TestGHADelimiterEmptyBody 空 body 也要能正确配对（避免误判为 mismatch）。
func TestGHADelimiterEmptyBody(t *testing.T) {
	const id = "USM_EOF_1"
	raw := renderOutputBlock(id, id, "", "")
	parsed := parseGHAOutput(raw)
	if len(parsed) != 2 {
		t.Fatalf("空 body 应仍能配对, 实际解析 %d 块", len(parsed))
	}
	if parsed["warnings"] != "" || parsed["result"] != "" {
		t.Errorf("空 body 解析结果应为空串: %#v", parsed)
	}
}
