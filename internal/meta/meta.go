// Package meta 解析与生成用户脚本的 ==UserScript== 元数据头。
//
// 该头是 `/add` `/up` `sources` `pages` 的公共基础：
// 本包只做「文本 ↔ 结构」转换，不涉及文件路径与业务规则（那些在 script 包）。
package meta

import (
	"strings"
)

// 已知键（渲染时按此顺序输出；大小写按习惯形态）。
var knownOrder = []string{
	"name", "namespace", "version", "description", "author",
	"match", "grant", "downloadURL", "updateURL",
}

// 习惯形态的键名（用于渲染）。
var canonicalKey = map[string]string{
	"name":        "@name",
	"namespace":   "@namespace",
	"version":     "@version",
	"description": "@description",
	"author":      "@author",
	"match":       "@match",
	"grant":       "@grant",
	"downloadURL": "@downloadURL",
	"updateURL":   "@updateURL",
}

// Header 结构化元数据头。未出现的单值字段为空串；多值字段未出现为 nil。
type Header struct {
	Name        string
	Namespace   string
	Version     string
	Description string
	Author      string
	Match       []string
	Grant       []string
	DownloadURL string
	UpdateURL   string
	// Other 保序承载未知键（key 不含 @，多值原样追加），保证解析→渲染不丢字段。
	Other []KV
}

type KV struct {
	Key   string
	Value string
}

const (
	openToken  = "==UserScript=="
	closeToken = "==/UserScript=="
)

// Parse 从脚本源码中提取元数据头。找不到完整块 → ok=false（返回零值与已解析的部分信息除外）。
func Parse(src string) (Header, bool) {
	var h Header
	lines := strings.Split(src, "\n")
	start, end := -1, -1
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
		return h, false
	}
	for _, ln := range lines[start+1 : end] {
		t := strings.TrimSpace(strings.TrimSuffix(ln, "\r"))
		t = strings.TrimPrefix(t, "//")
		t = strings.TrimSpace(t)
		if !strings.HasPrefix(t, "@") {
			continue
		}
		t = t[1:] // 去 @
		sp := strings.IndexAny(t, " \t")
		var key, val string
		if sp < 0 {
			key, val = t, ""
		} else {
			key, val = t[:sp], strings.TrimSpace(t[sp+1:])
		}
		switch strings.ToLower(key) {
		case "name":
			h.Name = val
		case "namespace":
			h.Namespace = val
		case "version":
			h.Version = val
		case "description":
			h.Description = val
		case "author":
			h.Author = val
		case "downloadurl":
			h.DownloadURL = val
		case "updateurl":
			h.UpdateURL = val
		case "match":
			h.Match = append(h.Match, val)
		case "grant":
			h.Grant = append(h.Grant, val)
		default:
			h.Other = append(h.Other, KV{Key: key, Value: val})
		}
	}
	return h, true
}

// Render 输出规范化的头块（含开闭行）。空头也输出完整骨架。
func (h Header) Render() string {
	var b strings.Builder
	b.WriteString("// " + openToken + "\n")
	single := []struct{ key, val string }{
		{"name", h.Name},
		{"namespace", h.Namespace},
		{"version", h.Version},
		{"description", h.Description},
		{"author", h.Author},
	}
	for _, kv := range single {
		if kv.val != "" {
			b.WriteString("// " + canonicalKey[kv.key] + " " + kv.val + "\n")
		}
	}
	for _, v := range h.Match {
		b.WriteString("// @match " + v + "\n")
	}
	for _, v := range h.Grant {
		b.WriteString("// @grant " + v + "\n")
	}
	if h.DownloadURL != "" {
		b.WriteString("// @downloadURL " + h.DownloadURL + "\n")
	}
	if h.UpdateURL != "" {
		b.WriteString("// @updateURL " + h.UpdateURL + "\n")
	}
	for _, kv := range h.Other {
		b.WriteString("// @" + kv.Key + " " + kv.Value + "\n")
	}
	b.WriteString("// " + closeToken)
	return b.String()
}

// HasReports 头块是否存在。
func Has(src string) bool {
	_, ok := Parse(src)
	return ok
}
