package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/registry"
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

	active := 0
	deleted := 0
	withoutIssue := 0
	created := 0
	updated := 0
	errs := 0

	ctx := context.Background()

	for i := range r.Scripts {
		s := &r.Scripts[i]
		if s.Deleted {
			deleted++
			if s.Issue != nil && env.GHClient != nil {
				if err := tombstoneIssue(ctx, env.GHClient, s.Issue.NodeID); err != nil {
					errs++
					continue
				}
				updated++
			}
			continue
		}
		active++
		if s.Issue == nil {
			withoutIssue++
			if env.GHClient != nil {
				if err := ensureIssue(ctx, env, r, s); err != nil {
					errs++
					continue
				}
				created++
			}
		}
	}

	msg := fmt.Sprintf("📊 投影统计：\n\n"+
		"- 活跃脚本: %d\n"+
		"- 已删除: %d\n"+
		"- 无关联 Issue: %d\n",
		active, deleted, withoutIssue)

	if env.GHClient != nil {
		msg += fmt.Sprintf("\n✅ 本次操作：\n"+
			"- 创建 Issue: %d\n"+
			"- 更新 Issue: %d\n"+
			"- 错误: %d\n",
			created, updated, errs)
	} else {
		msg += "\n⚠️ GitHub API 未配置，仅输出统计"
	}

	return reply(created > 0 || updated > 0, "%s", msg)
}

// ensureIssue 为脚本确保有关联 Issue（新建或更新）。
func ensureIssue(ctx context.Context, env *Env, r *registry.Registry, s *registry.Script) error {
	if env.GHClient == nil {
		return fmt.Errorf("GHClient 未配置")
	}

	existing, err := env.GHClient.ListRepoIssues(ctx, "OPEN")
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

	issueBody := buildIssueBody(s, env.PagesBase)

	if matched != nil {
		if err := env.GHClient.UpdateIssue(ctx, matched.NodeID, titlePrefix+" v"+s.Version, issueBody); err != nil {
			return fmt.Errorf("更新 Issue #%d 失败: %w", matched.Number, err)
		}
		s.Issue = &registry.IssueRef{
			Number: matched.Number,
			NodeID: matched.NodeID,
			URL:    fmt.Sprintf("https://github.com/%s/issues/%d", env.RepoOwner, matched.Number),
		}
	} else {
		iss, err := env.GHClient.CreateIssue(ctx, titlePrefix+" v"+s.Version, issueBody)
		if err != nil {
			return fmt.Errorf("创建 Issue 失败: %w", err)
		}
		s.Issue = &registry.IssueRef{
			Number: iss.Number,
			NodeID: iss.NodeID,
			URL:    fmt.Sprintf("https://github.com/%s/issues/%d", env.RepoOwner, iss.Number),
		}
	}

	now := rfc3339(nowOf(env))
	s.UpdatedAt = now
	_, err = saveReg(env, r)
	return err
}

// tombstoneIssue 将 Issue 标记为墓碑。
func tombstoneIssue(ctx context.Context, ghc *github.Client, nodeID string) error {
	tombstoneBody := "此脚本已被软删除。\n\n[还原](../commands) 后重新投影即可恢复。"
	if err := ghc.UpdateIssue(ctx, nodeID, "【已删除】脚本", tombstoneBody); err != nil {
		return fmt.Errorf("标记墓碑失败: %w", err)
	}
	return nil
}

// buildIssueBody 构建 Issue body（Markdown，中文）。
func buildIssueBody(s *registry.Script, pagesBase string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("**脚本**: %s v%s\n\n", s.Name, s.Version))
	b.WriteString("| 字段 | 值 |\n|---|---|\n")
	b.WriteString(fmt.Sprintf("| ID | `%s` |\n", s.ID))
	b.WriteString(fmt.Sprintf("| 类型 | %s |\n", scriptTypeLabel(s.Type)))
	b.WriteString(fmt.Sprintf("| 状态 | %s |\n", statusLabel(s.Enabled, s.Deleted)))
	if s.Description != "" {
		b.WriteString(fmt.Sprintf("| 描述 | %s |\n", s.Description))
	}
	b.WriteString(fmt.Sprintf("| 作者 | %s |\n", orDash(s.Author)))
	b.WriteString(fmt.Sprintf("| @match | `%s` |\n", strings.Join(s.Match, ", ")))
	b.WriteString(fmt.Sprintf("| @grant | `%s` |\n", strings.Join(s.Grant, ", ")))
	if s.SourceURL != nil {
		b.WriteString(fmt.Sprintf("| 来源 | [%s](%s) |\n", *s.SourceURL, *s.SourceURL))
	}
	if pagesBase != "" {
		distURL := pagesBase + "/dist/" + s.ID + ".user.js"
		b.WriteString(fmt.Sprintf("| 分发 | [%s](%s) |\n", s.ID+".user.js", distURL))
	}
	b.WriteString("\n---\n\n**changelog**:\n\n| 版本 | 日期 | 说明 |\n|---|---|---|\n")
	for _, c := range s.Changelog {
		b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", c.Version, c.Date, c.Note))
	}
	return b.String()
}

func scriptTypeLabel(t string) string {
	switch t {
	case registry.TypeSelf:
		return "self（自写脚本）"
	case registry.TypeSynced:
		return "synced（同步脚本）"
	default:
		return t
	}
}

func statusLabel(enabled, deleted bool) string {
	if deleted {
		return "🗑️ 已删除"
	}
	if enabled {
		return "✅ 启用"
	}
	return "⏸️ 停用"
}
