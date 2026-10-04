package issuepage

import (
	"strings"
	"testing"

	"github.com/acg-q/userscript-console/internal/escape"
	"github.com/acg-q/userscript-console/internal/registry"
)

func sampleScript() registry.Script {
	src := "https://example.com/a.user.js"
	return registry.Script{
		ID:            "3f45ee3c-0000-4000-8000-000000000001",
		Type:          registry.TypeSelf,
		Name:          "示例脚本",
		Version:       "1.2.3",
		Description:   "一段描述",
		Author:        "ACG-Q",
		Namespace:     "https://ns.example.com",
		Match:         []string{"*://*/*", "https://ex.com/*"},
		Grant:         []string{"none"},
		Enabled:       true,
		Documentation: "",
		Changelog: []registry.ChangelogEntry{
			{Version: "1.2.3", Date: "2026-10-05", Note: "修复|管道"},
			{Version: "1.2.2", Date: "2026-10-04", Note: "第二条"},
			{Version: "1.2.1", Date: "2026-10-03", Note: "第三条"},
			{Version: "1.2.0", Date: "2026-10-02", Note: "第四条不应出现"},
		},
		Discussions: []registry.DiscussionEntry{},
		SourceURL:   &src,
	}
}

func TestMarker形态(t *testing.T) {
	if got := Marker("abc"); got != "<!-- script-id: abc -->" {
		t.Errorf("Marker = %q", got)
	}
	// marker 经 EscapeMdCell 不被破坏（不变量 5）
	if got := escape.EscapeMdCell(Marker("abc")); got != Marker("abc") {
		t.Errorf("EscapeMdCell 破坏了 marker: %q", got)
	}
}

func TestScriptIDFromBody(t *testing.T) {
	s := sampleScript()
	body := BuildBody(s)

	if id, ok := ScriptIDFromBody(body); !ok || id != s.ID {
		t.Errorf("BuildBody 回读 = (%q, %v)", id, ok)
	}
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"首行空行非marker", "\n" + Marker("x"), false},
		{"marker在第二行", "标题\n" + Marker("x"), false},
		{"CRLF首行", Marker("x") + "\r\n正文", true},
		{"首行前置空格容忍（trim 后恰等 marker）", "  " + Marker("x"), true},
		{"id含空格非法", "<!-- script-id: a b -->\n", false},
		{"id含尖括号非法", "<!-- script-id: a<b -->\n", false},
		{"id含连续连字符非法", "<!-- script-id: a--b -->\n", false},
		{"id为空非法", "<!-- script-id:  -->\n", false},
		{"marker无空位", "<!-- script-id: -->\n", false},
		{"无marker", "普通正文", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := ScriptIDFromBody(tt.body); ok != tt.want {
				t.Errorf("ScriptIDFromBody = %v, want %v", ok, tt.want)
			}
		})
	}
	// 墓碑不得回读
	if _, ok := ScriptIDFromBody(TombstoneBody(s)); ok {
		t.Error("墓碑正文不应含有效 marker")
	}
}

func TestHasMarker(t *testing.T) {
	if !HasMarker("前文\n" + Marker("abc") + "\n后文") {
		t.Error("任意位置应检出")
	}
	if HasMarker("没有标记") {
		t.Error("误检出")
	}
	if HasMarker("<!-- script-id: bad id -->") {
		t.Error("非法 id 不应检出")
	}
}

func TestBuildTitle状态后缀(t *testing.T) {
	s := sampleScript()
	tests := []struct {
		name    string
		mutate  func(*registry.Script)
		wantSub string
	}{
		{"活跃", func(s *registry.Script) {}, "示例脚本 · v1.2.3"},
		{"停用", func(s *registry.Script) { s.Enabled = false }, "[已停用]"},
		{"删除", func(s *registry.Script) { s.Deleted = true }, "[已删除]"},
		{"双状态叠加顺序固定", func(s *registry.Script) { s.Enabled = false; s.Deleted = true }, "[已停用] [已删除]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := s
			tt.mutate(&c)
			got := BuildTitle(c)
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("BuildTitle = %q, 缺 %q", got, tt.wantSub)
			}
		})
	}
	// 墓碑标题对已删除脚本与 BuildTitle 恒等（利于 projector 判等）
	c := s
	c.Deleted = true
	if TombstoneTitle(s) != BuildTitle(c) {
		t.Errorf("TombstoneTitle ≠ BuildTitle: %q vs %q", TombstoneTitle(s), BuildTitle(c))
	}
}

func TestBuildBody首行与回读(t *testing.T) {
	s := sampleScript()
	body := BuildBody(s)
	first, _, _ := strings.Cut(body, "\n")
	if first != Marker(s.ID) {
		t.Errorf("首行 = %q, want %q", first, Marker(s.ID))
	}
	if id, ok := ScriptIDFromBody(body); !ok || id != s.ID {
		t.Errorf("回读失败: (%q, %v)", id, ok)
	}
}

func TestBuildBody确定性(t *testing.T) {
	s := sampleScript()
	if BuildBody(s) != BuildBody(s) {
		t.Error("两次调用输出不一致（I-1）")
	}
	if BuildTitle(s) != BuildTitle(s) {
		t.Error("标题非确定性")
	}
}

func TestBuildBody内容要素(t *testing.T) {
	s := sampleScript()
	body := BuildBody(s)
	for _, want := range []string{s.Name, s.Version, s.Author, s.Description, s.ID, "已启用", "*://*/*"} {
		if !strings.Contains(body, want) {
			t.Errorf("正文缺 %q", want)
		}
	}
	// changelog 只取前 3 条、新→旧
	if !strings.Contains(body, "1.2.3") || !strings.Contains(body, "1.2.2") || !strings.Contains(body, "1.2.1") {
		t.Error("缺前 3 条 changelog")
	}
	if strings.Contains(body, "1.2.0") {
		t.Error("第 4 条不应出现")
	}
	if !strings.Contains(body, "共 4 条") {
		t.Error("缺总数注记")
	}
	// 软删除状态行
	c := s
	c.Deleted = true
	if !strings.Contains(BuildBody(c), "已软删除") {
		t.Error("软删除状态缺失")
	}
}

func Test表格转义生效(t *testing.T) {
	s := sampleScript()
	s.Match = []string{"a|b\nc"}
	body := BuildBody(s)
	if !strings.Contains(body, `| 1 | a\|b c |`) {
		t.Errorf("match 单元格未转义:\n%s", body)
	}
	if !strings.Contains(body, `修复\|管道`) {
		t.Errorf("changelog note 未转义:\n%s", body)
	}
}

func TestTombstone无marker(t *testing.T) {
	s := sampleScript()
	tb := TombstoneBody(s)
	if strings.Contains(tb, "script-id") {
		t.Error("墓碑含 script-id 字样")
	}
	if strings.Contains(tb, s.ID) {
		t.Error("墓碑不应写原始 ID")
	}
	if _, ok := ScriptIDFromBody(tb); ok {
		t.Error("墓碑首行不得是 marker")
	}
	if !strings.Contains(tb, "已下架") {
		t.Error("缺下架标注")
	}
	// 确定性
	if tb != TombstoneBody(s) {
		t.Error("墓碑非确定性")
	}
}

func TestDiscussion标题与正文(t *testing.T) {
	s := sampleScript()
	title := DiscussionTitle(s, "1.2.3")
	if title != "示例脚本 · v1.2.3 版本讨论" {
		t.Errorf("标题 = %q", title)
	}
	body := DiscussionBody(s, "1.2.3")
	if HasMarker(body) || strings.Contains(body, "script-id") {
		t.Error("版本帖正文不得含 marker")
	}
	if !strings.Contains(body, "修复|管道") {
		t.Errorf("应含该版本 changelog note（非表格，不转义）:\n%s", body)
	}
	// 未命中版本 → 占位
	body2 := DiscussionBody(s, "9.9.9")
	if !strings.Contains(body2, "暂无该版本的更新说明") {
		t.Errorf("未命中占位缺失:\n%s", body2)
	}
}
