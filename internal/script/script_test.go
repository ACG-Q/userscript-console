package script

import (
	"errors"
	"strings"
	"testing"

	"github.com/acg-q/userscript-console/internal/registry"
)

// baseSrc 无 URL 行的头块夹具（对齐空格风格，模拟人工维护的脚本）。
const baseSrc = `// ==UserScript==
// @name         示例脚本
// @namespace    https://example.com
// @version      1.2.3
// @description  描述
// @author       ACG-Q
// @match        *://*/*
// @match        https://example.com/*
// @grant        none
// ==/UserScript==

(function () { 'use strict'; })();
`

const (
	wantDownloadURL = "https://cdn.example.com/demo.user.js"
	wantUpdateURL   = "https://cdn.example.com/demo.meta.js"
)

// insertBeforeClose 在头块闭合行前插入若干行（测试夹具构造用）。
func insertBeforeClose(src string, extra ...string) string {
	const marker = "// ==/UserScript=="
	i := strings.Index(src, marker)
	if i < 0 {
		panic("夹具缺少头块闭合行")
	}
	return src[:i] + strings.Join(extra, "\n") + "\n" + src[i:]
}

// ── 元数据 ──────────────────────────────────────────────────

func Test元数据提取与头构建往返一致(t *testing.T) {
	s := registry.Script{
		ID:          "demo",
		Type:        registry.TypeSelf,
		Name:        "示例脚本",
		Version:     "1.2.3",
		Description: "描述 <b>保留</b>",
		Author:      "ACG-Q",
		Namespace:   "https://example.com",
		Match:       []string{"*://*/*", "https://example.com/*"},
		Grant:       []string{"none", "GM_setValue"},
	}
	h := BuildHeader(s)
	got, ok := ExtractMeta(h)
	if !ok {
		t.Fatalf("BuildHeader 输出不可解析:\n%s", h)
	}
	if got.Name != s.Name || got.Version != s.Version || got.Description != s.Description ||
		got.Author != s.Author || got.Namespace != s.Namespace {
		t.Errorf("单值字段往返不一致: %+v", got)
	}
	if len(got.Match) != 2 || got.Match[0] != s.Match[0] || got.Match[1] != s.Match[1] {
		t.Errorf("match 多值往返不一致: %#v", got.Match)
	}
	if len(got.Grant) != 2 || got.Grant[0] != s.Grant[0] || got.Grant[1] != s.Grant[1] {
		t.Errorf("grant 多值往返不一致: %#v", got.Grant)
	}
	if got.DownloadURL != "" || got.UpdateURL != "" {
		t.Errorf("BuildHeader 不应填充 URL（由调用方经 EnsureURLs 注入）: %+v", got)
	}

	// 集成：注入 URL 后仍能往返提取到正确值
	withURL := EnsureURLs(h, wantDownloadURL, wantUpdateURL)
	h2, ok := ExtractMeta(withURL)
	if !ok {
		t.Fatalf("注入 URL 后不可解析:\n%s", withURL)
	}
	if h2.DownloadURL != wantDownloadURL || h2.UpdateURL != wantUpdateURL {
		t.Errorf("注入后 URL 往返不一致: %+v", h2)
	}
	if h2.Name != s.Name || len(h2.Match) != 2 {
		t.Errorf("注入后其余字段被破坏: %+v", h2)
	}
}

func Test无头块提取失败(t *testing.T) {
	if _, ok := ExtractMeta("console.log(1);"); ok {
		t.Error("无头块应提取失败")
	}
	if _, ok := ExtractMeta("// ==UserScript==\n// @name x\n"); ok {
		t.Error("缺闭合行应提取失败")
	}
}

// ── EnsureURLs ──────────────────────────────────────────────

func TestEnsureURLs注入与幂等(t *testing.T) {
	wrongSrc := insertBeforeClose(baseSrc,
		"// @downloadURL  https://wrong.example.com/old.user.js",
		"// @updateURL    https://wrong.example.com/old.meta.js")
	rightSrc := insertBeforeClose(baseSrc,
		"// @downloadURL  "+wantDownloadURL,
		"// @updateURL    "+wantUpdateURL)
	dupSrc := insertBeforeClose(baseSrc,
		"// @downloadURL https://wrong1.example.com/a.user.js",
		"// @downloadURL https://wrong2.example.com/b.user.js")
	crlfSrc := strings.ReplaceAll(baseSrc, "\n", "\r\n")

	cases := []struct {
		name  string
		src   string
		dl    string
		ul    string
		check func(t *testing.T, out string)
	}{
		{
			name: "两缺都补加",
			src:  baseSrc,
			dl:   wantDownloadURL,
			ul:   wantUpdateURL,
			check: func(t *testing.T, out string) {
				h, ok := ExtractMeta(out)
				if !ok {
					t.Fatalf("注入后不可解析:\n%s", out)
				}
				if h.DownloadURL != wantDownloadURL || h.UpdateURL != wantUpdateURL {
					t.Errorf("URL 未正确注入: %+v", h)
				}
				iGrant := strings.Index(out, "@grant")
				iDL := strings.Index(out, "@downloadURL")
				iUL := strings.Index(out, "@updateURL")
				iClose := strings.Index(out, "==/UserScript==")
				if !(iGrant >= 0 && iGrant < iDL && iDL < iUL && iUL < iClose) {
					t.Errorf("补加行位置错误（应排在既有字段后、闭合行前）:\n%s", out)
				}
			},
		},
		{
			name: "错误值原位替换",
			src:  wrongSrc,
			dl:   wantDownloadURL,
			ul:   wantUpdateURL,
			check: func(t *testing.T, out string) {
				h, ok := ExtractMeta(out)
				if !ok {
					t.Fatalf("替换后不可解析:\n%s", out)
				}
				if h.DownloadURL != wantDownloadURL || h.UpdateURL != wantUpdateURL {
					t.Errorf("错误 URL 未被替换: %+v", h)
				}
				if strings.Contains(out, "wrong.example.com") {
					t.Errorf("旧值残留:\n%s", out)
				}
				if strings.Count(out, "@downloadURL") != 1 || strings.Count(out, "@updateURL") != 1 {
					t.Errorf("替换后行数变化:\n%s", out)
				}
			},
		},
		{
			name: "值正确则逐字节不变",
			src:  rightSrc,
			dl:   wantDownloadURL,
			ul:   wantUpdateURL,
			check: func(t *testing.T, out string) {
				if out != rightSrc {
					t.Errorf("值已正确不应改动（含对齐空格风格）:\n得到:\n%s", out)
				}
			},
		},
		{
			name: "无头块原样返回",
			src:  "console.log(1);\n",
			dl:   wantDownloadURL,
			ul:   wantUpdateURL,
			check: func(t *testing.T, out string) {
				if out != "console.log(1);\n" {
					t.Errorf("无头块应原样返回: %q", out)
				}
			},
		},
		{
			name: "空参数视为不作为",
			src:  wrongSrc,
			dl:   "",
			ul:   "",
			check: func(t *testing.T, out string) {
				if out != wrongSrc {
					t.Errorf("空参数不应改动任何行:\n%s", out)
				}
			},
		},
		{
			name: "空参数且缺失时不补加",
			src:  baseSrc,
			dl:   "",
			ul:   "",
			check: func(t *testing.T, out string) {
				if out != baseSrc {
					t.Errorf("空参数不应补加任何行:\n%s", out)
				}
			},
		},
		{
			name: "重复 URL 行均改写为同一值",
			src:  dupSrc,
			dl:   wantDownloadURL,
			ul:   wantUpdateURL,
			check: func(t *testing.T, out string) {
				if strings.Count(out, wantDownloadURL) != 2 {
					t.Errorf("重复行应均被改写:\n%s", out)
				}
				if strings.Contains(out, "wrong1.example.com") || strings.Contains(out, "wrong2.example.com") {
					t.Errorf("旧值残留:\n%s", out)
				}
				h, ok := ExtractMeta(out)
				if !ok || h.DownloadURL != wantDownloadURL || h.UpdateURL != wantUpdateURL {
					t.Errorf("改写后提取结果错误: %+v (ok=%v)", h, ok)
				}
			},
		},
		{
			name: "CRLF 行尾保持",
			src:  crlfSrc,
			dl:   wantDownloadURL,
			ul:   wantUpdateURL,
			check: func(t *testing.T, out string) {
				lines := strings.Split(out, "\n")
				start, end, ok := findHeader(lines)
				if !ok {
					t.Fatalf("CRLF 头块定位失败:\n%s", out)
				}
				for i := start; i <= end; i++ {
					if !strings.HasSuffix(lines[i], "\r") {
						t.Errorf("头块第 %d 行丢失 \\r: %q", i, lines[i])
					}
				}
				h, ok := ExtractMeta(out)
				if !ok || h.DownloadURL != wantDownloadURL || h.UpdateURL != wantUpdateURL {
					t.Errorf("CRLF 注入后提取错误: %+v (ok=%v)", h, ok)
				}
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out := EnsureURLs(tt.src, tt.dl, tt.ul)
			tt.check(t, out)
			// 幂等契约：两次调用结果一致
			if again := EnsureURLs(out, tt.dl, tt.ul); again != out {
				t.Errorf("非幂等：第二次调用结果不同:\n第一次:\n%s\n第二次:\n%s", out, again)
			}
		})
	}
}

// ── SyncVersion ─────────────────────────────────────────────

func TestSyncVersion替换与幂等(t *testing.T) {
	noVersionSrc := strings.Replace(baseSrc, "// @version      1.2.3\n", "", 1)
	alignedSrc := strings.Replace(baseSrc, "// @version      1.2.3", "// @version  1.2.3", 1)

	cases := []struct {
		name    string
		src     string
		version string
		check   func(t *testing.T, out string)
	}{
		{
			name:    "替换旧版本行",
			src:     baseSrc,
			version: "2.0.0",
			check: func(t *testing.T, out string) {
				h, ok := ExtractMeta(out)
				if !ok {
					t.Fatalf("替换后不可解析:\n%s", out)
				}
				if h.Version != "2.0.0" {
					t.Errorf("版本未替换: %+v", h)
				}
				if strings.Contains(out, "1.2.3") {
					t.Errorf("旧版本残留:\n%s", out)
				}
			},
		},
		{
			name:    "缺版本行则补加",
			src:     noVersionSrc,
			version: "3.0.0",
			check: func(t *testing.T, out string) {
				h, ok := ExtractMeta(out)
				if !ok || h.Version != "3.0.0" {
					t.Fatalf("补加版本失败: %+v (ok=%v)", h, ok)
				}
				iVer := strings.Index(out, "@version")
				iClose := strings.Index(out, "==/UserScript==")
				if iVer < 0 || iVer > iClose {
					t.Errorf("版本行应在头块内:\n%s", out)
				}
			},
		},
		{
			name:    "无头块原样返回",
			src:     "console.log(1);\n",
			version: "2.0.0",
			check: func(t *testing.T, out string) {
				if out != "console.log(1);\n" {
					t.Errorf("无头块应原样返回: %q", out)
				}
			},
		},
		{
			name:    "空版本视为不作为",
			src:     baseSrc,
			version: "",
			check: func(t *testing.T, out string) {
				if out != baseSrc {
					t.Errorf("空版本不应改动:\n%s", out)
				}
			},
		},
		{
			name:    "值已正确则逐字节不变",
			src:     alignedSrc,
			version: "1.2.3",
			check: func(t *testing.T, out string) {
				if out != alignedSrc {
					t.Errorf("版本已正确不应改动:\n%s", out)
				}
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out := SyncVersion(tt.src, tt.version)
			tt.check(t, out)
			if again := SyncVersion(out, tt.version); again != out {
				t.Errorf("非幂等：第二次调用结果不同:\n第一次:\n%s\n第二次:\n%s", out, again)
			}
		})
	}
}

// ── IncrementVersion ────────────────────────────────────────

func Test版本自增全分支(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"普通三段递增", "1.2.3", "1.2.4", false},
		{"容忍小写v前缀", "v1.2.3", "v1.2.4", false},
		{"容忍大写V前缀", "V2.0.0", "V2.0.1", false},
		{"末段进位", "1.9", "1.10", false},
		{"单段递增", "41", "42", false},
		{"段数保持", "1.2", "1.3", false},
		{"不补零", "1.09", "1.10", false},
		{"零值起步", "0.0.0", "0.0.1", false},
		{"前缀加进位", "v0.9.9", "v0.9.10", false},
		{"空串报错", "", "", true},
		{"仅前缀报错", "v", "", true},
		{"纯文字报错", "abc", "", true},
		{"末段非数字报错", "1.2.x", "", true},
		{"首段非数字报错", "x.1", "", true},
		{"中段非数字报错", "1.x.3", "", true},
		{"空分段报错", "1..2", "", true},
		{"尾部分隔报错", "1.2.", "", true},
		{"首部分隔报错", ".5", "", true},
		{"负数分段报错", "1.-2", "", true},
		{"末段溢出报错", "99999999999999999999", "", true},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IncrementVersion(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("版本 %q 期望报错，实际得到 %q", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("版本 %q 报错: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("IncrementVersion(%q) = %q，期望 %q", tt.in, got, tt.want)
			}
		})
	}
}

// ── changelog ───────────────────────────────────────────────

func TestChangelog头插顺序与幂等(t *testing.T) {
	base := []registry.ChangelogEntry{
		{Version: "1.0.2", Date: "2026-01-02", Note: "第二版"},
		{Version: "1.0.1", Date: "2026-01-01", Note: "首版"},
	}

	// 头插后保持新→旧顺序
	out := PrependChangelog(base, "1.0.3", "2026-01-03", "第三版")
	wantOrder := []string{"1.0.3", "1.0.2", "1.0.1"}
	if len(out) != len(wantOrder) {
		t.Fatalf("长度错误: %d，期望 %d", len(out), len(wantOrder))
	}
	for i, w := range wantOrder {
		if out[i].Version != w {
			t.Errorf("第 %d 条 version = %q，期望 %q", i, out[i].Version, w)
		}
	}
	if out[0].Date != "2026-01-03" || out[0].Note != "第三版" {
		t.Errorf("头部条目内容错误: %+v", out[0])
	}

	// 入参不被原地篡改
	if len(base) != 2 || base[0].Version != "1.0.2" {
		t.Errorf("入参切片被篡改: %+v", base)
	}

	// 同版本头部 → 幂等（不重复追加）
	again := PrependChangelog(out, "1.0.3", "2026-01-03", "第三版")
	if len(again) != 3 {
		t.Errorf("同版本头插应幂等，长度 %d，期望 3", len(again))
	}
	for i := range again {
		if again[i] != out[i] {
			t.Errorf("第 %d 条内容变化: %+v → %+v", i, out[i], again[i])
		}
	}

	// 空列表
	empty := PrependChangelog(nil, "0.1.0", "2026-01-01", "首个版本")
	if len(empty) != 1 || empty[0].Version != "0.1.0" {
		t.Errorf("空列表头插错误: %+v", empty)
	}

	// date 由调用方传入，不自动取 now（空串原样保留）
	noDate := PrependChangelog(nil, "0.1.1", "", "无日期")
	if len(noDate) != 1 || noDate[0].Date != "" {
		t.Errorf("空 date 应原样保留，不取 now: %+v", noDate)
	}
}

// ── 路径 ────────────────────────────────────────────────────

func Test路径契约精确字符串(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"自写源码", SelfSourcePath("demo"), "scripts/self/demo/index.js"},
		{"同步源码", SyncedSourcePath("demo"), "scripts/synced/demo/script.user.js"},
		{"分发产物", DistPath("demo"), "dist/demo.user.js"},
		{"自写文档", DocPath("demo-script"), "scripts/self/demo-script/README.md"},
		{"synced形态无文档", DocPath("0123456789ab"), ""},
		{"大写形态不误判为synced", DocPath("ABCDEF123456"), "scripts/self/ABCDEF123456/README.md"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("得到 %q，期望 %q", tt.got, tt.want)
			}
		})
	}
}

// ── Format（D-03 stub） ─────────────────────────────────────

func Test格式美化返回未实现Stub(t *testing.T) {
	src := "const a = 1;\n(function () {})();\n"
	out, err := Format(src)
	if !errors.Is(err, ErrFormatNotImplemented) {
		t.Errorf("期望 ErrFormatNotImplemented，实际: %v", err)
	}
	if out != src {
		t.Errorf("stub 应原样返回源码，得到 %q", out)
	}
	// 同输入两次输出一致（降级路径稳定）
	out2, err2 := Format(src)
	if out2 != src || !errors.Is(err2, ErrFormatNotImplemented) {
		t.Errorf("二次调用结果不稳定: %q / %v", out2, err2)
	}
}
