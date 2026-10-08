package meta

import "testing"

const src = `// ==UserScript==
// @name         示例脚本
// @namespace    https://example.com
// @version      1.2.3
// @description  描述 <b>保留</b>
// @author       ACG-Q
// @match        *://*/*
// @match        https://example.com/*
// @grant        none
// @downloadURL  https://example.com/d.user.js
// @updateURL    https://example.com/d.user.js
// @icon         https://example.com/i.png
// @run-at       document-start
// ==/UserScript==

(function () { 'use strict'; })();
`

func TestParse(t *testing.T) {
	h, ok := Parse(src)
	if !ok {
		t.Fatal("未识别头块")
	}
	if h.Name != "示例脚本" || h.Version != "1.2.3" || h.Author != "ACG-Q" {
		t.Errorf("单值字段错误: %+v", h)
	}
	if h.Description != "描述 <b>保留</b>" {
		t.Errorf("Description = %q", h.Description)
	}
	if len(h.Match) != 2 || h.Match[1] != "https://example.com/*" {
		t.Errorf("Match = %#v", h.Match)
	}
	if len(h.Grant) != 1 || h.Grant[0] != "none" {
		t.Errorf("Grant = %#v", h.Grant)
	}
	if h.DownloadURL == "" || h.UpdateURL == "" {
		t.Error("URL 字段缺失")
	}
	if len(h.Other) != 2 || h.Other[0].Key != "icon" {
		t.Errorf("Other = %#v", h.Other)
	}
}

func TestParseMissing(t *testing.T) {
	if _, ok := Parse("plain js"); ok {
		t.Error("无头块应 ok=false")
	}
	if _, ok := Parse("// ==UserScript==\n// @name x\n"); ok {
		t.Error("缺闭合应 ok=false")
	}
}

func TestRenderRoundTrip(t *testing.T) {
	h, _ := Parse(src)
	out := h.Render()
	h2, ok := Parse(out)
	if !ok {
		t.Fatalf("Render 输出不可解析:\n%s", out)
	}
	if h2.Name != h.Name || h2.Version != h.Version || len(h2.Match) != len(h.Match) {
		t.Errorf("往返不一致: %+v vs %+v", h2, h)
	}
	// 已知键顺序：name 在 version 前、match 在 downloadURL 前
	ri := index(out, "@name")
	rv := index(out, "@version")
	rd := index(out, "@downloadURL")
	rm := index(out, "@match")
	if ri >= rv || rv >= rm || rm >= rd {
		t.Errorf("渲染顺序错误:\n%s", out)
	}
	// 幂等：Render→Parse→Render 稳定
	if h2.Render() != out {
		t.Error("Render 非幂等")
	}
}

func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestParseCRLF(t *testing.T) {
	h, ok := Parse("// ==UserScript==\r\n// @name crlf\r\n// ==/UserScript==\r\n")
	if !ok || h.Name != "crlf" {
		t.Errorf("CRLF 解析失败: ok=%v name=%q", ok, h.Name)
	}
}

func TestHas(t *testing.T) {
	if !Has(src) {
		t.Error("Has 有效源应返回 true")
	}
	if Has("no header here") {
		t.Error("Has 无效源应返回 false")
	}
}
