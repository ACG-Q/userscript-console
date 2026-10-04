package sources

import (
	"context"
	"fmt"
	"net/url"

	"github.com/acg-q/userscript-console/internal/meta"
)

func init() { register(zoneAdapter{}) }

// zoneAdapter userscript.zone：抓页面 HTML 提取内嵌 ==UserScript== 头块解析。
type zoneAdapter struct{}

func (zoneAdapter) Type() string { return TypeZone }

var zoneHosts = []string{"userscript.zone"}

func (zoneAdapter) MatchURL(rawurl string) bool {
	u, err := url.Parse(rawurl)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := u.Hostname()
	for _, p := range zoneHosts {
		if MatchHostSuffix(host, p) {
			return true
		}
	}
	return false
}

func (zoneAdapter) Fetch(ctx context.Context, d Doer, rawurl string) (*Result, error) {
	body, err := HTTPGet(ctx, d, rawurl)
	if err != nil {
		return nil, fmt.Errorf("userscript.zone: 抓取页面失败 %s: %w", rawurl, err)
	}
	block, ok := extractUserScriptBlock(string(body))
	if !ok {
		return nil, fmt.Errorf("userscript.zone: 页面 %s 未找到 ==UserScript== 元数据头", rawurl)
	}
	h, ok := meta.Parse(block)
	if !ok {
		return nil, fmt.Errorf("userscript.zone: 页面 %s 内嵌头块解析失败", rawurl)
	}
	return resultFromHeader(h, block, TypeZone), nil
}
