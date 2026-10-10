package commands

import (
	"context"
	"fmt"

	"github.com/acg-q/userscript-console/internal/projector"
)

func init() {
	Register(Command{
		Name:  "project",
		Help:  "registry → Issues/版本帖 对账投影",
		Usage: "/project",
		Run:   runProject,
	})
}

// runProject 对账投影：遍历 registry，确保每个活跃脚本有关联的 Issue/版本帖。
func runProject(env *Env, args string, codeBlocks []string) (Result, error) {
	r, err := loadReg(env)
	if err != nil {
		return Result{}, err
	}

	if env.RepoOwner == "" && env.GHClient == nil {
		return fail("project 命令需要配置 GH_REPO_OWNER 或 GH_CLIENT")
	}

	ctx := context.Background()
	penv := &projector.Env{
		Root:      env.Root,
		RepoOwner: env.RepoOwner,
		RepoName:  env.RepoName,
		PagesBase: env.PagesBase,
		DistSeg:   env.layout().DistSeg(),
		GHClient:  env.GHClient,
	}

	res, err := projector.Project(ctx, penv, r)
	if err != nil {
		return Result{}, fmt.Errorf("投影失败: %w", err)
	}

	// 保存 registry（确保 Issue 更新已写入）
	_, err = saveReg(env, r)
	if err != nil {
		return Result{}, fmt.Errorf("保存 registry 失败: %w", err)
	}

	msg := fmt.Sprintf("📊 投影统计：\n\n"+
		"- 活跃脚本: %d\n"+
		"- 已删除: %d\n"+
		"- 无关联 Issue: %d\n",
		res.Active, res.Deleted, res.NoIssue)

	if env.GHClient != nil {
		msg += fmt.Sprintf("\n✅ 本次操作：\n"+
			"- 创建 Issue: %d\n"+
			"- 更新 Issue: %d\n"+
			"- 错误: %d\n",
			res.Created, res.Updated, res.Errors)
	} else {
		msg += "\n⚠️ GitHub API 未配置，仅输出统计"
	}

	return reply(res.Created > 0 || res.Updated > 0, "%s", msg)
}
