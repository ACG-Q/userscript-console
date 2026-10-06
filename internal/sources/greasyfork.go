package sources

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/acg-q/userscript-console/internal/meta"
)

func init() { register(greasyforkAdapter{}) }

// greasyforkAdapter Greasy Fork / Sleazy Fork：一级走 update 子域按 ID 直取
// 源码（生产由 nginx 直出、不进主站 WAF），失败回退页面链：内嵌头块 →
// 安装直链 → /code 源码页。全部入口都拿不到含 ==UserScript== 头的源码即
// 报错（不产出 Code 为空的结果，防空脚本入库）。
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

// greasyforkInstallLinkRe 页面中的安装直链（<a href="https://update.…org/…">，
// 实测 update 子域对直链请求直出 .user.js 源码，与一级同源）。
var greasyforkInstallLinkRe = regexp.MustCompile(`(?i)href="(https://update\.(?:greasyfork|sleazyfork)\.org/[^"]+)"`)

// updateHostFor 主域 → update 直连子域（两侧各自独立，均已实测 200）。
func updateHostFor(host string) string {
	if MatchHostSuffix(host, "sleazyfork.org") {
		return "update.sleazyfork.org"
	}
	return "update.greasyfork.org"
}

func (greasyforkAdapter) Fetch(ctx context.Context, d Doer, rawurl string) (*Result, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, fmt.Errorf("greasyfork: URL 无法解析 %s: %w", rawurl, err)
	}
	id := ""
	if m := greasyforkIDRe.FindStringSubmatch(u.Path); m != nil {
		id = m[1]
	}

	// ── 一级：update 子域直链（丢弃 locale 查询与语言路径前缀） ──
	var firstErr error
	if id != "" {
		codeURL := "https://" + updateHostFor(u.Hostname()) + "/scripts/" + id + ".user.js"
		if body, err := HTTPGet(ctx, d, codeURL); err != nil {
			firstErr = fmt.Errorf("一级 update直链失败 %s: %w", codeURL, err)
		} else if h, ok := meta.Parse(string(body)); !ok {
			firstErr = fmt.Errorf("一级 update直链 %s 缺少 ==UserScript== 头", codeURL)
		} else {
			return resultFromHeader(h, string(body), TypeGreasyFork), nil
		}
	}

	// ── 二级：页面链（内嵌头块 → 安装直链 → /code 页），逐级容错 ──
	pageBody, pageErr := HTTPGet(ctx, d, rawurl)
	if pageErr != nil {
		secondErr := fmt.Errorf("二级页面失败 %s: %w", rawurl, pageErr)
		if firstErr != nil {
			return nil, fmt.Errorf("greasyfork: %w；%w", firstErr, secondErr) // 组合错误，%w 链两级
		}
		return nil, fmt.Errorf("greasyfork: %w", secondErr) // 无 id：只走二级
	}
	page := string(pageBody)
	stages := []string{}

	// 2a 页面内嵌 ==UserScript== 头块。
	if block, ok := extractUserScriptBlock(page); ok {
		if h, ok := meta.Parse(block); ok {
			return resultFromHeader(h, block, TypeGreasyFork), nil
		}
		stages = append(stages, "页面内嵌头块解析失败")
	} else {
		stages = append(stages, "页面无内嵌头块")
	}

	// 2b 页面安装直链（update 子域 .user.js）。
	if m := greasyforkInstallLinkRe.FindStringSubmatch(page); m != nil {
		installURL := html.UnescapeString(m[1]) // href 属性可能含 HTML 实体
		if body, err := HTTPGet(ctx, d, installURL); err != nil {
			stages = append(stages, fmt.Sprintf("安装直链失败 %s: %v", installURL, err))
		} else if h, ok := meta.Parse(string(body)); !ok {
			stages = append(stages, fmt.Sprintf("安装直链 %s 缺少 ==UserScript== 头", installURL))
		} else {
			return resultFromHeader(h, string(body), TypeGreasyFork), nil
		}
	} else {
		stages = append(stages, "页面无安装直链")
	}

	// 2c /code 源码页（pre 块同样锚定 ==UserScript==，复用公共截取）。
	if strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/code") {
		stages = append(stages, "code页 目标已是code页")
	} else {
		cu := *u
		cu.Path = strings.TrimRight(u.Path, "/") + "/code"
		cu.RawQuery, cu.Fragment = "", ""
		codeURL := cu.String()
		if body, err := HTTPGet(ctx, d, codeURL); err != nil {
			stages = append(stages, fmt.Sprintf("code页失败 %s: %v", codeURL, err))
		} else if block, ok := extractUserScriptBlock(string(body)); ok {
			if h, ok := meta.Parse(block); ok {
				return resultFromHeader(h, block, TypeGreasyFork), nil
			}
			stages = append(stages, fmt.Sprintf("code页 %s 头块解析失败", codeURL))
		} else {
			stages = append(stages, fmt.Sprintf("code页 %s 无头块", codeURL))
		}
	}

	// 全部入口失败：聚合二级各阶段上下文，与一级组合。
	secondErr := fmt.Errorf("二级页面链失败 %s: %s", rawurl, strings.Join(stages, "；"))
	if firstErr != nil {
		return nil, fmt.Errorf("greasyfork: %w；%w", firstErr, secondErr)
	}
	return nil, fmt.Errorf("greasyfork: %w", secondErr)
}
