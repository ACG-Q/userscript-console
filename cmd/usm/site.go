package main

import (
	"context"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/pages"
	"github.com/acg-q/userscript-console/internal/registry"
)

// siteBuilder 把 internal/pages 接到 /build：抓取 GitHub 数据 → 渲染整站 →
// 落盘（含陈旧产物清理与 build-warnings.txt）。
//
// 该类型放在 cmd/usm 而非 internal/commands，是 SPEC-ARCH-TEST §1 的依赖方向
// 要求：commands 不得反向 import pages，故以 commands.SiteBuilder 接口注入。
// siteGH 站点抓取所需的 GitHub 能力子集。声明为接口是为了测试可注入 fake
// （*github.Client 天然满足），同时避免把 nil 指针塞进接口导致判空失效。
type siteGH interface {
	IssueStats(ctx context.Context, nodeIDs []string) ([]github.Stats, error)
	DiscussionComments(ctx context.Context, nodeID string) ([]github.Comment, error)
}

type siteBuilder struct {
	root      string
	pagesBase string
	now       time.Time
	gh        siteGH // nil → 跳过 GitHub 抓取（降级渲染，pages 记 W1）
}

// Build 实现 commands.SiteBuilder。
func (b *siteBuilder) Build(reg *registry.Registry) (int, bool, []string, error) {
	data, warnings := b.fetchData(reg)

	out, err := pages.Build(reg, pages.Options{
		Out:       filepath.Join(b.root, "dist"),
		PagesBase: b.pagesBase,
		Now:       b.now,
	}, data)
	if err != nil {
		return 0, false, nil, fmt.Errorf("渲染站点: %w", err)
	}

	changed, err := writeSite(b.root, out)
	if err != nil {
		return 0, false, nil, err
	}
	return out.Pages, changed, append(warnings, out.BuildWarnings...), nil
}

// fetchData 抓取详情页需要的 GitHub 侧数据。
// 任何抓取失败都降级为告警（站点仍要出），不中断构建。
func (b *siteBuilder) fetchData(reg *registry.Registry) (pages.Data, []string) {
	if b.gh == nil {
		return pages.Data{}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	data := pages.Data{
		IssueStats:         map[string]int{},
		DiscussionComments: map[string][]pages.Comment{},
	}
	var warnings []string

	var issueNodes []string
	for _, s := range reg.Scripts {
		if s.Issue != nil && s.Issue.NodeID != "" {
			issueNodes = append(issueNodes, s.Issue.NodeID)
		}
	}
	if len(issueNodes) > 0 {
		stats, err := b.gh.IssueStats(ctx, issueNodes)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("抓取 Issue 评论数失败: %v", err))
		} else {
			for _, st := range stats {
				data.IssueStats[st.NodeID] = st.Comments
			}
		}
	}

	for _, s := range reg.Scripts {
		for _, d := range s.Discussions {
			if d.NodeID == "" {
				continue
			}
			cs, err := b.gh.DiscussionComments(ctx, d.NodeID)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("抓取版本帖 %s 的评论失败: %v", d.NodeID, err))
				continue
			}
			comments := make([]pages.Comment, 0, len(cs))
			for _, c := range cs {
				comments = append(comments, pages.Comment{
					Author:    c.Author,
					Body:      template.HTML(c.Body),
					CreatedAt: c.CreatedAt,
				})
			}
			data.DiscussionComments[d.NodeID] = comments
		}
	}

	return data, warnings
}

// 站点产物相对数据根的路径契约（PLAN.md C3-15、.gitignore 的部署清单）。
const (
	siteIndexRel      = "dist/index.html"
	siteScriptsJSON   = "dist/scripts.json"
	siteDetailDir     = "dist/scripts"
	siteCommandsDir   = "dist/commands"
	siteWarningsRel   = "dist/build-warnings.txt"
	siteCommandsIndex = "dist/commands/index.html"
)

// writeSite 落盘整站产物：内容有差异才写（幂等 → 相同输入第二次构建 changed=false），
// 并清掉本轮未再生成的陈旧页面（否则删除脚本后详情页仍可访问）。
func writeSite(root string, out pages.Outcome) (bool, error) {
	changed := false
	keep := map[string]struct{}{}

	write := func(rel, content string) error {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if cur, err := os.ReadFile(path); err == nil && string(cur) == content {
			keep[path] = struct{}{}
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("创建 %s 目录失败: %w", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", rel, err)
		}
		changed = true
		keep[path] = struct{}{}
		return nil
	}

	if err := write(siteIndexRel, out.IndexHTML); err != nil {
		return false, err
	}
	if err := write(siteScriptsJSON, out.ScriptsJSON); err != nil {
		return false, err
	}
	for id, html := range out.DetailHTMLs {
		// ID 进入文件路径，先挡掉路径穿越（registry 是可被 comment 写入的外部输入）。
		if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
			return false, fmt.Errorf("非法脚本 ID %q 不能作为详情页文件名", id)
		}
		if err := write(filepath.ToSlash(filepath.Join(siteDetailDir, id+".html")), html); err != nil {
			return false, err
		}
	}
	for n, html := range out.CommandPages {
		if err := write(fmt.Sprintf("%s/page-%d.html", siteCommandsDir, n), html); err != nil {
			return false, err
		}
	}
	if out.CommandsIndex != "" {
		if err := write(siteCommandsIndex, out.CommandsIndex); err != nil {
			return false, err
		}
	}
	if err := write(siteWarningsRel, strings.Join(out.BuildWarnings, "\n")); err != nil {
		return false, err
	}

	for _, dir := range []string{siteDetailDir, siteCommandsDir} {
		stale, err := staleSiteFiles(root, dir, keep)
		if err != nil {
			return changed, err
		}
		for _, p := range stale {
			if err := os.Remove(p); err != nil {
				return changed, fmt.Errorf("清理陈旧产物 %s 失败: %w", p, err)
			}
			changed = true
		}
	}

	return changed, nil
}

// staleSiteFiles 列出 dir 下本轮未生成、且属于站点产物形态的陈旧文件。
// 只认 .html：dist/ 根另有 .user.js 分发产物，不能误伤。
func staleSiteFiles(root, dir string, keep map[string]struct{}) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", dir, err)
	}
	var stale []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".html") {
			continue
		}
		path := filepath.Join(root, dir, e.Name())
		if _, ok := keep[path]; ok {
			continue
		}
		stale = append(stale, path)
	}
	return stale, nil
}
