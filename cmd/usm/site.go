package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
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
	DiscThread(ctx context.Context, nodeID string) (github.Thread, error)
}

type siteBuilder struct {
	root      string
	pagesBase string
	version   string // usm 版本（页脚/meta 展示），空→pages normalize 兜底 "dev"
	now       time.Time
	gh        siteGH // nil → 跳过 GitHub 抓取（降级渲染，W1 由 pages.Build 统一记）
}

// Build 实现 commands.SiteBuilder。
func (b *siteBuilder) Build(reg *registry.Registry) (int, bool, []string, error) {
	data, warnings := b.fetchData(reg)

	out, err := pages.Build(reg, pages.Options{
		Out:       filepath.Join(b.root, "dist"),
		PagesBase: b.pagesBase,
		Version:   b.version,
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
// W1 告警由 pages.Build 统一判定（fetchData 只负责回 Data{} 表示降级）：
//   - gh == nil（无 token / 本地降级）→ pages.Data{}；
//   - 有版本帖账本但 0 线程抓到 → pages.Data{}（全灭视为降级）。
//
// 任何单帖抓取失败都降级为告警（站点仍要出），不中断构建。
func (b *siteBuilder) fetchData(reg *registry.Registry) (pages.Data, []string) {
	if b.gh == nil {
		return pages.Data{}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	data := pages.Data{Discussions: map[string]pages.Thread{}}
	var warnings []string
	fetched := 0
	for _, s := range reg.Scripts {
		for _, d := range s.Discussions {
			if d.NodeID == "" {
				continue
			}
			th, err := b.gh.DiscThread(ctx, d.NodeID)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("抓取版本帖 %s 的评论失败: %v", d.NodeID, err))
				continue
			}
			comments := make([]pages.Comment, 0, len(th.Comments))
			for _, c := range th.Comments {
				comments = append(comments, pages.Comment{
					Author:    c.Author,
					Body:      c.Body,
					CreatedAt: c.CreatedAt,
				})
			}
			data.Discussions[d.NodeID] = pages.Thread{
				Comments:  comments,
				HasAnswer: th.HasAnswer,
			}
			fetched++
		}
	}
	if fetched == 0 && hasDiscussions(reg) {
		return pages.Data{}, warnings
	}

	return data, warnings
}

func hasDiscussions(reg *registry.Registry) bool {
	for _, s := range reg.Scripts {
		if len(s.Discussions) > 0 {
			return true
		}
	}
	return false
}

// 站点产物相对数据根的路径契约（PLAN.md C3-15、.gitignore 的部署清单）。
const (
	siteIndexRel      = "dist/index.html"
	siteScriptsJSON   = "dist/scripts.json"
	siteDetailDir     = "dist/scripts"
	siteCommandsDir   = "dist/commands"
	siteDocsDir       = "dist/docs"
	siteWarningsRel   = "dist/build-warnings.txt"
	siteCommandsIndex = "dist/commands/index.html"
)

// validDocName 文档输出键契约：<slug>.html 或恰好一级 <subdir>/<slug>.html
// （子目录白名单 [a-z0-9-]+）；拒绝 ..、\、绝对路径、更深层级。
func validDocName(name string) bool {
	if name == "" || strings.Contains(name, `\`) || strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) == 1 {
		return parts[0] == path.Base(parts[0])
	}
	if len(parts) != 2 {
		return false
	}
	if ok, _ := regexp.MatchString(`^[a-z0-9-]+$`, parts[0]); !ok {
		return false
	}
	return parts[1] != "" && parts[1] == path.Base(parts[1])
}

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
	for name, html := range out.DocHTMLs {
		// 文档输出键进入文件路径：只放行顶层或恰好一级白名单子目录。
		if !validDocName(name) {
			return false, fmt.Errorf("非法文档文件名 %q", name)
		}
		if err := write(filepath.ToSlash(filepath.Join(siteDocsDir, name)), html); err != nil {
			return false, err
		}
	}
	if err := write(siteWarningsRel, strings.Join(out.BuildWarnings, "\n")); err != nil {
		return false, err
	}

	for _, dir := range []string{siteDetailDir, siteCommandsDir, siteDocsDir} {
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
// 递归子目录：dist/docs/commands/ 的陈旧页同样要清。
func staleSiteFiles(root, dir string, keep map[string]struct{}) ([]string, error) {
	base := filepath.Join(root, dir)
	if _, err := os.Stat(base); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", dir, err)
	}
	var stale []string
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".html") {
			return nil
		}
		if _, ok := keep[p]; !ok {
			stale = append(stale, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("遍历 %s 失败: %w", dir, err)
	}
	return stale, nil
}
