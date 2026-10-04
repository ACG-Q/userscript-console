package sources

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/acg-q/userscript-console/internal/meta"
)

func init() { register(greasyforkAdapter{}) }

// greasyforkAdapter Greasy Fork / Sleazy Fork：一级抓 `<host>/scripts/<id>.code.user.js`
// （严格校验头），失败回退二级抓页面 HTML（内嵌头块 → meta/title 兜底）。
type greasyforkAdapter struct{}

func (greasyforkAdapter) Type() string { return TypeGreasyFork }

// greasyforkHosts 白名单（MatchHostSuffix 含子域）。
var greasyforkHosts = []string{"greasyfork.org", "sleazyfork.org"}

func (greasyforkAdapter) MatchURL(rawurl string) bool {
	u, err := url.Parse(rawurl)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := u.Hostname()
	for _, p := range greasyforkHosts {
		if MatchHostSuffix(host, p) {
			return true
		}
	}
	return false
}

// greasyforkIDRe 从路径提取脚本数字 id（兼容语言路径前缀 /zh-CN/scripts/123）。
var greasyforkIDRe = regexp.MustCompile(`/scripts/(\d+)`)

// greasyforkTitleRe 去掉页面标题的站点后缀（如 "Foo | Greasy Fork"）。
var greasyforkTitleRe = regexp.MustCompile(`(?i)\s*[|·—–-]\s*Greasy Fork\s*$`)

func (greasyforkAdapter) Fetch(ctx context.Context, d Doer, rawurl string) (*Result, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, fmt.Errorf("greasyfork: URL 无法解析 %s: %w", rawurl, err)
	}
	id := ""
	if m := greasyforkIDRe.FindStringSubmatch(u.Path); m != nil {
		id = m[1]
	}

	// ── 一级：推导 .code.user.js 直链（丢弃 locale 查询与语言路径前缀） ──
	var firstErr error
	if id != "" {
		codeURL := u.Scheme + "://" + u.Host + "/scripts/" + id + ".code.user.js"
		if body, err := HTTPGet(ctx, d, codeURL); err != nil {
			firstErr = fmt.Errorf("一级脚本直链失败 %s: %w", codeURL, err)
		} else if h, ok := meta.Parse(string(body)); !ok {
			firstErr = fmt.Errorf("一级脚本直链 %s 缺少 ==UserScript== 头", codeURL)
		} else {
			return resultFromHeader(h, string(body), TypeGreasyFork), nil
		}
	}

	// ── 二级：抓原页面 HTML ──
	pageBody, pageErr := HTTPGet(ctx, d, rawurl)
	if pageErr != nil {
		secondErr := fmt.Errorf("二级页面失败 %s: %w", rawurl, pageErr)
		if firstErr != nil {
			return nil, fmt.Errorf("greasyfork: %w；%w", firstErr, secondErr) // 组合错误，%w 链两级
		}
		return nil, fmt.Errorf("greasyfork: %w", secondErr) // 无 id：只走二级
	}
	page := string(pageBody)

	// 二级首选：页面内嵌 ==UserScript== 头块。
	if block, ok := extractUserScriptBlock(page); ok {
		if h, ok := meta.Parse(block); ok {
			return resultFromHeader(h, block, TypeGreasyFork), nil
		}
	}

	// 二级兜底：meta 标签 / title 填元数据（无脚本源码，Code 为空）。
	pm := parsePageMeta(page)
	res := &Result{
		Name:        strings.TrimSpace(greasyforkTitleRe.ReplaceAllString(pm.Title, "")),
		Description: pm.Description,
		Author:      pm.Author,
		SourceType:  TypeGreasyFork,
	}
	if res.Name == "" && res.Description == "" && res.Author == "" {
		secondErr := fmt.Errorf("二级页面失败 %s: 页面既无内嵌头块也无可用 meta/title", rawurl)
		if firstErr != nil {
			return nil, fmt.Errorf("greasyfork: %w；%w", firstErr, secondErr)
		}
		return nil, fmt.Errorf("greasyfork: %w", secondErr)
	}
	return res, nil
}
