package sources

import (
	"context"
	"fmt"

	"github.com/acg-q/userscript-console/internal/meta"
)

// direct 兜底适配器：把 URL 当裸用户脚本源码抓取，严格校验元数据头。
//
// 注意：本适配器**不注册**进 registry（MatchURL 恒真，注册即吞掉所有路由），
// 只由 Detect 在白名单全部未命中时返回。
type direct struct{}

func (direct) Type() string { return TypeDirect }

// MatchURL 恒 true——但仅作 Detect 兜底语义，不代表白名单。
func (direct) MatchURL(string) bool { return true }

func (direct) Fetch(ctx context.Context, d Doer, rawurl string) (*Result, error) {
	code, err := HTTPGet(ctx, d, rawurl)
	if err != nil {
		return nil, fmt.Errorf("direct: 抓取失败 %s: %w", rawurl, err)
	}
	h, ok := meta.Parse(string(code))
	if !ok {
		return nil, fmt.Errorf("direct: %s 不是用户脚本（缺少 ==UserScript== 元数据头）", rawurl)
	}
	return resultFromHeader(h, string(code), TypeDirect), nil
}
