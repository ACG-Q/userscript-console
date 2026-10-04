// Package projector 处理脚本与 GitHub Issues/版本帖的对账投影。
package projector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/registry"
)

// Env 投影所需的环境配置。
type Env struct {
	Root      string
	RepoOwner string
	PagesBase string
	GHClient  *github.Client
}

// Result 投影结果统计。
type Result struct {
	Active  int
	Deleted int
	NoIssue int
	Created int
	Updated int
	Errors  int
}

// Project 对账投影：遍历 registry，确保每个活跃脚本有关联的 Issue。
func Project(ctx context.Context, env *Env, r *registry.Registry) (Result, error) {
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

	return res, nil
}

// EnsureIssue 为脚本确保有关联 Issue（新建或更新）。
// 注意：此函数不保存 registry，调用方需自行保存。
func EnsureIssue(ctx context.Context, env *Env, r *registry.Registry, s *registry.Script) error {
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

	issueBody := BuildIssueBody(s, env.PagesBase)

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

	s.UpdatedAt = nowRFC3339()
	return nil
}

// TombstoneIssue 将 Issue 标记为墓碑。
func TombstoneIssue(ctx context.Context, ghc *github.Client, nodeID string) error {
	if ghc == nil {
		return fmt.Errorf("GHClient 未配置")
	}
	tombstoneBody := "此脚本已被软删除。\n\n[还原](../commands) 后重新投影即可恢复。"
	if err := ghc.UpdateIssue(ctx, nodeID, "【已删除】脚本", tombstoneBody); err != nil {
		return fmt.Errorf("标记墓碑失败: %w", err)
	}
	return nil
}

// BuildIssueBody 构建 Issue body（Markdown，中文）。
func BuildIssueBody(s *registry.Script, pagesBase string) string {
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

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
