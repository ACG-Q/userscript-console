// Package sources 识别来源 URL 类型并抓取、解析用户脚本。
//
// 扩展点（SPEC-ARCH-TEST §7「新数据源」）：新增一个数据源 = 新增一个适配器文件
// （实现 Adapter 接口并在 init() 中 register），外加 hostname 白名单条目与
// fixture 回放用例；禁止在 Detect 或既有适配器里加 if/else 特判。
// 忘注册必须让测试红：注册清单断言硬编码期望集合（见 sources_test.go）。
//
// direct 适配器**不注册**——它是 Detect 的兜底，若注册会因 MatchURL 恒真
// 吞掉所有路由（文件按字母序初始化，注册顺序不可依赖，故靠"不注册"区分）。
package sources

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/acg-q/userscript-console/internal/meta"
)

// SourceType 取值（Result.SourceType 与 Adapter.Type 一致）。
const (
	TypeGreasyFork = "greasyfork"
	TypeZone       = "userscript_zone"
	TypeGist       = "github_gist"
	TypeDirect     = "direct"
)

// Result 一次抓取解析的产出：元数据 + 脚本源码。
type Result struct {
	Name, Version, Description, Author string
	Match, Grant                       []string
	Code                               string
	SourceType                         string // greasyfork | userscript_zone | github_gist | direct
}

// Doer 抽象 HTTP 执行者（测试注入 fake，禁止真实网络）。
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Adapter 一个数据源适配器：白名单匹配 + 抓取解析。
type Adapter interface {
	Type() string
	MatchURL(rawurl string) bool
	Fetch(ctx context.Context, d Doer, rawurl string) (*Result, error)
}

// ── 适配器注册表（包内；direct 不注册，仅作 Detect 兜底） ──────────

var registry []Adapter

func register(a Adapter) { registry = append(registry, a) }

// ── host 后缀匹配（白名单防绕过） ───────────────────────────

// normalizeHost 小写归一 + 去 FQDN 尾点（可多个）。
func normalizeHost(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.TrimRight(s, ".")
}

// MatchHostSuffix 判定 host 是否命中白名单 pattern：完全相等，或以 ".pattern"
// 结尾（即 pattern 本身或其子域）。两侧都做小写归一与 FQDN 尾点去除。
//
// 覆盖：greasyfork.org 匹配 www.greasyfork.org；不匹配 evilgreasyfork.org、
// greasyfork.org.evil.com、evil.com/path/greasyfork.org（path 不是 host）。
func MatchHostSuffix(host, pattern string) bool {
	h, p := normalizeHost(host), normalizeHost(pattern)
	if h == "" || p == "" {
		return false
	}
	return h == p || strings.HasSuffix(h, "."+p)
}

// ── 路由 ───────────────────────────────────────────────

// Detect 按注册表白名单选择适配器；无命中时兜底 direct。
// 非 http(s) 或无 host 的 URL 报错。
func Detect(rawurl string) (Adapter, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, fmt.Errorf("sources: URL 无法解析 %q: %w", rawurl, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("sources: 仅支持 http(s) URL，实际 scheme=%q（%s）", u.Scheme, rawurl)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("sources: URL 缺少主机名（%s）", rawurl)
	}
	for _, a := range registry {
		if a.MatchURL(rawurl) {
			return a, nil
		}
	}
	return direct{}, nil // 兜底：任意 http(s) URL 都可尝试按裸脚本抓取
}

// ── HTTP ───────────────────────────────────────────────

const (
	browserUA        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	acceptHeader     = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	acceptLangHeader = "en-US,en;q=0.9"
)

// HTTPGet 执行 GET（浏览器 UA + Accept + Accept-Language，缺 Accept-Language
// 会被 GreasyFork 等站点 403 拦截），ctx 贯穿整个请求生命周期。
// 非 2xx → error 含状态码与 URL；成功返回 body 字节。
func HTTPGet(ctx context.Context, d Doer, rawurl string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sources: 请求未发出（context 已结束）%s: %w", rawurl, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return nil, fmt.Errorf("sources: 构造请求失败 %s: %w", rawurl, err)
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", acceptHeader)
	req.Header.Set("Accept-Language", acceptLangHeader)
	resp, err := d.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sources: 请求失败 %s: %w", rawurl, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("sources: %s 返回非 2xx 状态码 %d", rawurl, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sources: 读取响应失败 %s: %w", rawurl, err)
	}
	return body, nil
}

// ── 页面公共辅助 ──────────────────────────────────────────

// extractUserScriptBlock 从 HTML/文本中锚定截取 ==UserScript==…==/UserScript==
// 头块：以 token 定位（回退到同行 "//" 起点）、截到闭 token 结束（丢弃其后的
// HTML 尾巴）、逐行把 "// " 前缀归一化（裸 @ 行补 "// "），最后
// html.UnescapeString 还原实体。
func extractUserScriptBlock(doc string) (string, bool) {
	open := strings.Index(doc, "==UserScript==")
	if open < 0 {
		return "", false
	}
	lineStart := strings.LastIndex(doc[:open], "\n") + 1
	if c := strings.LastIndex(doc[:open], "//"); c >= lineStart {
		lineStart = c // 行内形如 `<pre>// ==UserScript==` 时从 "//" 起
	}
	rest := doc[lineStart:]
	const closeTok = "==/UserScript=="
	closeIdx := strings.Index(rest, closeTok)
	if closeIdx < 0 {
		return "", false // 有开无闭 = 残块，按缺失处理
	}
	// 起点（开 token 同行 "//" 或行首）截到闭 token 结束，丢掉闭 token 后的 HTML 尾巴。
	block := rest[:closeIdx+len(closeTok)]
	lines := strings.Split(block, "\n")
	for i, ln := range lines {
		t := strings.TrimSpace(strings.TrimRight(ln, "\r"))
		switch {
		case strings.HasPrefix(t, "//"):
			lines[i] = "// " + strings.TrimSpace(t[2:])
		case strings.HasPrefix(t, "@"):
			lines[i] = "// " + t
		default:
			lines[i] = t
		}
	}
	return html.UnescapeString(strings.Join(lines, "\n")), true
}

// 页面元数据兜底值（meta 标签 / title / og）。
// resultFromHeader 把 meta 头转成 Result（各适配器公共组装）。
func resultFromHeader(h meta.Header, code, sourceType string) *Result {
	return &Result{
		Name:        h.Name,
		Version:     h.Version,
		Description: h.Description,
		Author:      h.Author,
		Match:       h.Match,
		Grant:       h.Grant,
		Code:        code,
		SourceType:  sourceType,
	}
}
