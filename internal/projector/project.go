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
	RepoName  string // owner/repo；Issue URL 需要完整仓库路径，仅 owner 会拼出 404
	PagesBase string
	DistSeg   string // 分发目录 URL 段（设计 §2 D5，跟随 --dist-dir）；零值 → "dist"
	GHClient  *github.Client
}

// distSeg 返回分发目录 URL 段；零值兜底 "dist"（默认布局）。
func (e *Env) distSeg() string {
	if e.DistSeg == "" {
		return "dist"
	}
	return e.DistSeg
}

// issueURL 生成 Issue 链接：优先 RepoName（owner/repo），退回 RepoOwner 兼容旧配置。
func issueURL(env *Env, number int) string {
	repo := env.RepoName
	if repo == "" {
		repo = env.RepoOwner
	}
	return fmt.Sprintf("https://github.com/%s/issues/%d", repo, number)
}

// Result 投影结果统计。
type Result struct {
	Active   int
	Deleted  int
	NoIssue  int
	Created  int
	Updated  int
	Errors   int
	Reopened int // 回填/判等时重开了已关闭 Issue
	Posted   int // 本轮发布版本帖数
	Skipped  int // 账本末条==当前，跳过发帖数
}

// Project 对账投影：阶段一 Issue 对账（回填/判等刷新），阶段二版本帖发牌（账本幂等）。
func Project(ctx context.Context, env *Env, r *registry.Registry) (Result, error) {
	res := reconcileIssues(ctx, env, r)
	posted, skipped, errs := publishDiscussions(ctx, env, r)
	res.Posted, res.Skipped = posted, skipped
	res.Errors += errs
	return res, nil
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
// distSeg 是分发目录 URL 段（默认 "dist"，自定义 --dist-dir 时跟随，设计 §2 D5）。
func BuildIssueBody(s *registry.Script, pagesBase, distSeg string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**脚本**: %s v%s\n\n", s.Name, s.Version)
	b.WriteString("| 字段 | 值 |\n|---|---|\n")
	fmt.Fprintf(&b, "| ID | `%s` |\n", s.ID)
	fmt.Fprintf(&b, "| 类型 | %s |\n", scriptTypeLabel(s.Type))
	fmt.Fprintf(&b, "| 状态 | %s |\n", statusLabel(s.Enabled, s.Deleted))
	if s.Description != "" {
		fmt.Fprintf(&b, "| 描述 | %s |\n", s.Description)
	}
	fmt.Fprintf(&b, "| 作者 | %s |\n", orDash(s.Author))
	fmt.Fprintf(&b, "| @match | `%s` |\n", strings.Join(s.Match, ", "))
	fmt.Fprintf(&b, "| @grant | `%s` |\n", strings.Join(s.Grant, ", "))
	if s.SourceURL != nil {
		fmt.Fprintf(&b, "| 来源 | [%s](%s) |\n", *s.SourceURL, *s.SourceURL)
	}
	if pagesBase != "" {
		distURL := pagesBase + "/" + strings.Trim(strings.ReplaceAll(distSeg, `\`, "/"), "/") + "/" + s.ID + ".user.js"
		fmt.Fprintf(&b, "| 分发 | [%s](%s) |\n", s.ID+".user.js", distURL)
	}
	b.WriteString("\n---\n\n**changelog**:\n\n| 版本 | 日期 | 说明 | 帖子 |\n|---|---|---|---|\n")
	for _, c := range s.Changelog {
		fmt.Fprintf(&b, "| v%s | %s | %s | %s |\n", c.Version, c.Date, c.Note, discussionLinkFor(s, c.Version))
	}
	return b.String()
}

// discussionLinkFor 版本 → 账本帖子链接；无条目返回 —。
func discussionLinkFor(s *registry.Script, version string) string {
	for _, d := range s.Discussions {
		if d.Version == version {
			return fmt.Sprintf("[讨论 #%d →](%s)", d.Number, d.URL)
		}
	}
	return "—"
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
