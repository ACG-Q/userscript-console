package projector

import (
	"context"

	"github.com/acg-q/userscript-console/internal/registry"
)

// reconcileIssues 阶段一：遍历 registry 做 Issue 对账。
// 本任务为骨架搬移（逻辑与原 Project 循环一致）；四分支扩展在任务 3/4 落地。
func reconcileIssues(ctx context.Context, env *Env, r *registry.Registry) Result {
	var res Result

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
				if err := EnsureIssue(ctx, env, r, s); err != nil {
					res.Errors++
					continue
				}
				res.Created++
			}
		}
	}

	return res
}
