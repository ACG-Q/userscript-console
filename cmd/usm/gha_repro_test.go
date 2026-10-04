package main

// 复现真实 CI 故障：use-binary 模式下 build 失败时，
// $GITHUB_OUTPUT 的 heredoc 解析失败。
//
// 现象（内容仓 deploy-pages.yml，exit code 5）：
//
//	Invalid value. Matching delimiter not found 'USM_EOF_1791119888208253933'
//	Unable to process file command 'output' successfully.
//
// 分隔符已是纳秒级共享变量（EOF_ID），说明「开闭不一致」的老 bug 已修。
// 那么真正的原因是：**result 正文里出现了恰好等于分隔符的行**，或者
// 正文里的行尾/编码让 GitHub 的 file-command 解析器失配。
//
// 本文件把 action.yml 的输出块逻辑在 Go 里复刻，并针对「失败路径」
// 构造真实 usm 的 JSON 输出，断言 GITHUB_OUTPUT 能被正确解析。

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// runnerPayload 模拟 usm --json 的输出（失败路径）。
type runnerPayload struct {
	Changed    bool     `json:"changed"`
	Authorized bool     `json:"authorized"`
	Result     string   `json:"result"`
	Warnings   []string `json:"warnings"`
	Errors     []string `json:"errors"`
}

// renderRunnerOutput 复刻 action.yml 的输出块。
// resultText 可能含任意字符（错误信息里有 Windows 路径、换行、markdown）。
func renderRunnerOutput(eofID string, j runnerPayload) string {
	warnings := strings.Join(j.Warnings, "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "changed=%v\n", j.Changed)
	fmt.Fprintf(&b, "authorized=%v\n", j.Authorized)
	fmt.Fprintf(&b, "warnings<<%s\n%s\n%s\n", eofID, warnings, eofID)
	fmt.Fprintf(&b, "result<<%s\n%s\n%s\n", eofID, j.Result, eofID)
	return b.String()
}

// parseRunnerOutput 复刻 GitHub runner 的 file-command 解析器。
// 返回 (outputs, error)；error 复现 "Matching delimiter not found"。
func parseRunnerOutput(raw string) (map[string]string, error) {
	starts := regexp.MustCompile(`(?m)^(\w+)<<(\S+)$`)
	closers := regexp.MustCompile(`(?m)^(\S+)$`)

	out := map[string]string{}
	lines := strings.Split(raw, "\n")

	for i := 0; i < len(lines); i++ {
		m := starts.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		name, delim := m[1], m[2]

		// 查找闭合行：从下一行开始，直到出现等于 delim 的整行
		found := -1
		for j := i + 1; j < len(lines); j++ {
			c := closers.FindStringSubmatch(lines[j])
			if c != nil && c[1] == delim {
				found = j
				break
			}
		}
		if found < 0 {
			return nil, fmt.Errorf("Invalid value. Matching delimiter not found '%s'", delim)
		}
		body := ""
		if found > i+1 {
			body = strings.Join(lines[i+1:found], "\n")
		}
		out[name] = body
		i = found
	}
	return out, nil
}

// TestGHAResultWithTrailingNewline 关键：result 正文末尾若带换行，
// 会多出一个空行 —— 本身可解析；但若正文**内部**含分隔符行就彻底失配。
func TestGHAResultWithTrailingNewline(t *testing.T) {
	j := runnerPayload{
		Result:   "❌ build 命令需要配置 PAGES_BASE 环境变量\n\nsome detail\n",
		Warnings: nil,
	}
	raw := renderRunnerOutput("USM_EOF_123", j)
	got, err := parseRunnerOutput(raw)
	if err != nil {
		t.Fatalf("带尾部换行的 result 应可解析: %v", err)
	}
	if !strings.HasPrefix(got["result"], "❌ build") {
		t.Errorf("result 内容不符: %q", got["result"])
	}
}

// TestGHAResultWithWindowsPath 错误信息里常含 Windows 路径（反斜杠）。
func TestGHAResultWithWindowsPath(t *testing.T) {
	j := runnerPayload{
		Result: `ERROR: 读取 registry.json 失败: open C:\Users\runner\repo\registry.json: The system cannot find the file specified.`,
	}
	raw := renderRunnerOutput("USM_EOF_456", j)
	got, err := parseRunnerOutput(raw)
	if err != nil {
		t.Fatalf("含 Windows 路径的 result 应可解析: %v", err)
	}
	if !strings.Contains(got["result"], `C:\Users\runner`) {
		t.Errorf("result 应保留反斜杠: %q", got["result"])
	}
}

// TestGHAResultWithMarkdownTable result 常常是 markdown 表格（list/info 输出）。
func TestGHAResultWithMarkdownTable(t *testing.T) {
	table := "| ID | 名称 | 版本 |\n|---|---|---|\n| `a1` | 测试 | 1.0.0 |"
	j := runnerPayload{Result: table, Warnings: []string{"警告1", "警告2"}}
	raw := renderRunnerOutput("USM_EOF_789", j)
	got, err := parseRunnerOutput(raw)
	if err != nil {
		t.Fatalf("markdown 表格应可解析: %v", err)
	}
	if got["result"] != table {
		t.Errorf("result 应原样保留表格:\ngot  %q\nwant %q", got["result"], table)
	}
	if got["warnings"] != "警告1\n警告2" {
		t.Errorf("warnings 应为两行: %q", got["warnings"])
	}
}

// TestGHAResultContainingDelimiter 危险场景：result 正文里恰好有一行
// 等于分隔符（概率极低，但分隔符是纳秒时间戳，理论上可被构造）。
// 结论：这不是本次故障的原因（分隔符不可预测），但值得记录。
func TestGHAResultContainingDelimiter(t *testing.T) {
	const delim = "USM_EOF_123"
	j := runnerPayload{Result: "正常内容\n" + delim + "\n被注入的行"}
	raw := renderRunnerOutput(delim, j)

	// 解析器会在中间那行提前闭合 → result 被截断
	got, err := parseRunnerOutput(raw)
	if err != nil {
		t.Logf("意外：解析失败 %v", err)
		return
	}
	if strings.Contains(got["result"], delim) {
		t.Log("分隔符注入不会破坏解析（闭合后继续扫描）")
	} else {
		t.Logf("注意：正文含分隔符时 result 被截断为 %q（理论场景，非本次故障）", got["result"])
	}
}

// TestGHAJSONUnmarshal 确认工具的 JSON 输出结构与本测试假设一致。
func TestGHAJSONUnmarshal(t *testing.T) {
	raw := `{"authorized":true,"changed":false,"result":"line1\nline2","warnings":["w"],"errors":["boom"]}`
	var j runnerPayload
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		t.Fatal(err)
	}
	if !j.Authorized || j.Changed || j.Result != "line1\nline2" {
		t.Errorf("解码不符: %+v", j)
	}
	if len(j.Errors) != 1 || j.Errors[0] != "boom" {
		t.Errorf("errors 不符: %#v", j.Errors)
	}
}
