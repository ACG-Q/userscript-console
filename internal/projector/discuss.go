package projector

import (
	"context"
	"fmt"

	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/issuepage"
	"github.com/acg-q/userscript-console/internal/registry"
)

// selectCategory 版本帖分类选择：slug q-a（Q&A）优先，否则列表第一个。
// 注：GitHub GraphQL DiscussionCategory 无 isDefault 字段（真实 schema 核验）。
func selectCategory(cats []github.Category) string {
	for _, c := range cats {
		if c.Slug == "q-a" {
			return c.NodeID
		}
	}
	return cats[0].NodeID
}

// EnsureDiscussionPost 为脚本创建版本帖并追加发牌账本（账本幂等的唯一写入点）。
// 失败返回 error 且不追加账本，保证可重试。
func EnsureDiscussionPost(ctx context.Context, env *Env, s *registry.Script, categoryID string) error {
	title := issuepage.DiscussionTitle(*s, s.Version)
	body := issuepage.DiscussionBody(*s, s.Version)
	d, err := env.GHClient.CreateDiscussion(ctx, categoryID, title, body)
	if err != nil {
		return fmt.Errorf("发版本帖失败: %w", err)
	}
	s.Discussions = append(s.Discussions, registry.DiscussionEntry{
		Version:   s.Version,
		Number:    d.Number,
		NodeID:    d.NodeID,
		URL:       d.URL,
		CreatedAt: d.CreatedAt,
	})
	return nil
}

// publishDiscussions 阶段二发牌：账本末条==当前版本则跳过（幂等），否则发帖并记账。
// 分类查询整个阶段只发一次（失败也缓存），单脚本失败不影响其余脚本。
func publishDiscussions(ctx context.Context, env *Env, r *registry.Registry) (posted, skipped, errs int) {
	if env.GHClient == nil {
		return 0, 0, 0
	}
	var catID string
	var catErr error
	catLoaded := false
	for i := range r.Scripts {
		s := &r.Scripts[i]
		if s.Deleted {
			continue
		}
		if n := len(s.Discussions); n > 0 && s.Discussions[n-1].Version == s.Version {
			skipped++
			continue
		}
		if !catLoaded {
			cats, err := env.GHClient.DiscussionCategories(ctx)
			if err != nil {
				catErr = fmt.Errorf("读取分类失败: %w", err)
			} else {
				catID = selectCategory(cats)
			}
			catLoaded = true
		}
		if catErr != nil {
			errs++
			continue
		}
		if err := EnsureDiscussionPost(ctx, env, s, catID); err != nil {
			errs++
			continue
		}
		posted++
	}
	return posted, skipped, errs
}
