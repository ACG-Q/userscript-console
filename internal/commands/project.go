package commands

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

	if env.RepoOwner == "" {
		return fail("project 命令需要配置 GH_REPO_OWNER 环境变量")
	}

	// 简化实现：仅输出统计信息
	active := 0
	deleted := 0
	withoutIssue := 0
	for i := range r.Scripts {
		s := &r.Scripts[i]
		if s.Deleted {
			deleted++
			continue
		}
		active++
		if s.Issue == nil {
			withoutIssue++
		}
	}

	return reply(false,
		"📊 投影统计：\n\n"+
			"- 活跃脚本: %d\n"+
			"- 已删除: %d\n"+
			"- 无关联 Issue: %d\n\n"+
			"⚠️ 完整投影功能待实现（需要 GitHub API 集成）",
		active, deleted, withoutIssue)
}
