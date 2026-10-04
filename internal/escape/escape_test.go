package escape

import "testing"

func TestEscapeMdCell(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"空串", "", ""},
		{"纯文本不动", "hello 世界", "hello 世界"},
		{"竖线转义", "a|b", `a\|b`},
		{"多条竖线", "|x|", `\|x\|`},
		{"CRLF 折叠为单空格", "a\r\nb", "a b"},
		{"裸 LF 折叠", "a\nb", "a b"},
		{"裸 CR 折叠", "a\rb", "a b"},
		{"顺序敏感：先折叠换行再转义竖线", "a|b\r\nc|d", `a\|b c\|d`},
		{"已是转义输入按朴素语义再转义（输入应为原始值）", `x\|y`, `x\\|y`},
		{"表格单元格典型内容", "版本 1.0.1\n手动更新 | 修复", `版本 1.0.1 手动更新 \| 修复`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EscapeMdCell(tt.in); got != tt.want {
				t.Errorf("EscapeMdCell(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestEscapeMdCellAppliesOnce 明确语义：本函数按朴素替换实现（与 Python 版一致），
// 只允许对**原始内容**应用一次；调用方不得对已转义结果二次转义。
func TestEscapeMdCellAppliesOnce(t *testing.T) {
	in := "a|b\n多行|内容"
	want := `a\|b 多行\|内容`
	if got := EscapeMdCell(in); got != want {
		t.Errorf("EscapeMdCell(%q) = %q, want %q", in, got, want)
	}
}

func TestEscapeMdText(t *testing.T) {
	if got := EscapeMdText("a|b\nc"); got != "a\\|b\nc" {
		t.Errorf("EscapeMdText 折叠了换行或漏转竖线: %q", got)
	}
}
