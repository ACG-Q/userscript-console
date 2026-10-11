package projector

import (
	"context"
	"fmt"
	"strings"

	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/registry"
)

// allIssuesLoader 返回全量 Issue 加载器（states=null 即 ALL，复用已关闭 Issue 防重复建卡）。
// 结果与错误均缓存：整个对账阶段只拉一次列表，后续脚本共享同一份快照。
func allIssuesLoader(env *Env) func(context.Context) ([]github.Issue, error) {
	var (
		called bool
		cache  []github.Issue
		err    error
	)
	return func(ctx context.Context) ([]github.Issue, error) {
		if !called {
			cache, err = env.GHClient.ListRepoIssues(ctx, "ALL")
			called = true
		}
		return cache, err
	}
}

// reconcileIssues 阶段一：遍历 registry 做 Issue 对账（回填/判等/墓碑四分支）。
func reconcileIssues(ctx context.Context, env *Env, r *registry.Registry) Result {
	var res Result
	loadIssues := allIssuesLoader(env)

	for i := range r.Scripts {
		s := &r.Scripts[i]
		if s.Deleted {
			res.Deleted++
			if s.Issue != nil && env.GHClient != nil {
				if err := TombstoneIssue(ctx, env.GHClient, s.Issue.NodeID); err != nil {
					res.Errors++
					continue
				}
				res.Updated++
			}
			continue
		}
		res.Active++
		if s.Issue == nil {
			res.NoIssue++
			if env.GHClient != nil {
				if err := backfillIssue(ctx, env, s, loadIssues, &res); err != nil {
					res.Errors++
					continue
				}
			}
		}
	}

	return res
}

// backfillIssue 回填无 Issue 关联的脚本：全量列表按标题前缀匹配（含已关闭），
// 命中则重开（如需）并更新，未命中才创建。每步成功即 bump UpdatedAt；错误原样上抛。
// loadIssues 由 reconcileIssues 注入，保证多脚本共享一次列表快照。
func backfillIssue(ctx context.Context, env *Env, s *registry.Script, loadIssues func(context.Context) ([]github.Issue, error), res *Result) error {
	existing, err := loadIssues(ctx)
	if err != nil {
		return fmt.Errorf("列出 Issue 失败: %w", err)
	}

	titlePrefix := fmt.Sprintf("[%s]", s.Name)
	var matched *github.Issue
	for i := range existing {
		if strings.HasPrefix(existing[i].Title, titlePrefix) {
			matched = &existing[i]
			break
		}
	}

	issueBody := BuildIssueBody(s, env.PagesBase, env.distSeg())

	switch {
	case matched != nil:
		if strings.EqualFold(matched.State, "CLOSED") {
			if err := env.GHClient.ReopenIssue(ctx, matched.NodeID); err != nil {
				return fmt.Errorf("重开 Issue #%d 失败: %w", matched.Number, err)
			}
			res.Reopened++
		}
		if err := env.GHClient.UpdateIssue(ctx, matched.NodeID, titlePrefix+" v"+s.Version, issueBody); err != nil {
			return fmt.Errorf("更新 Issue #%d 失败: %w", matched.Number, err)
		}
		res.Updated++
		s.Issue = &registry.IssueRef{
			Number: matched.Number,
			NodeID: matched.NodeID,
			URL:    issueURL(env, matched.Number),
		}
	default:
		iss, err := env.GHClient.CreateIssue(ctx, titlePrefix+" v"+s.Version, issueBody)
		if err != nil {
			return fmt.Errorf("创建 Issue 失败: %w", err)
		}
		res.Created++
		s.Issue = &registry.IssueRef{
			Number: iss.Number,
			NodeID: iss.NodeID,
			URL:    issueURL(env, iss.Number),
		}
	}

	s.UpdatedAt = nowRFC3339()
	return nil
}
