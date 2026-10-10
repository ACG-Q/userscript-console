package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/sources"
)

func init() {
	Register(Command{
		Name:  "sync",
		Help:  "同步脚本（拉取最新代码并更新 registry）",
		Usage: "/sync <id|名称|来源URL> | /sync-all",
		Run:   runSync,
	})
	Register(Command{
		Name:  "sync-all",
		Help:  "批量同步所有启用的 synced 脚本（/sync all 的别名）",
		Usage: "/sync-all",
		Run: func(env *Env, args string, codeBlocks []string) (Result, error) {
			return runSync(env, "all", codeBlocks)
		},
	})
}

// runSync 同步单个脚本或全部脚本。
func runSync(env *Env, args string, codeBlocks []string) (Result, error) {
	r, err := loadReg(env)
	if err != nil {
		return Result{}, err
	}

	key := strings.TrimSpace(args)
	if key == "all" || key == "" {
		return runSyncAll(env, r)
	}

	return runSyncOne(env, r, key)
}

// applySynced 把抓取结果写回条目（版本/元数据/时间戳/changelog），
// 返回更新前的版本号，供回帖文案与 changelog note 使用。
//
// /sync 与 /sync-all 共用同一份落库逻辑：原先两处各写一遍，
// 导致批量同步漏掉了 Author 与 changelog 截断。
func applySynced(s *registry.Script, src *sources.Result, now time.Time) string {
	oldVersion := s.Version
	s.Version = src.Version
	s.Description = src.Description
	s.Author = src.Author
	s.Match = src.Match
	s.Grant = src.Grant
	s.UpdatedAt = rfc3339(now)
	s.LastSyncedAt = stringPtr(rfc3339(now))
	s.Changelog = append([]registry.ChangelogEntry{
		{Version: src.Version, Date: dateStr(now), Note: fmt.Sprintf("同步更新 v%s → v%s", oldVersion, src.Version)},
	}, s.Changelog...)
	if len(s.Changelog) > 10 {
		s.Changelog = s.Changelog[:10]
	}
	return oldVersion
}

// runSyncOne 同步单个脚本。
func runSyncOne(env *Env, r *registry.Registry, key string) (Result, error) {
	s := findEntry(r, key)
	if s == nil {
		return fail("未找到脚本 %q", key)
	}

	if s.Type != registry.TypeSynced {
		return fail("脚本 %q 是自写脚本，无法同步", s.Name)
	}

	if s.SourceURL == nil {
		return fail("脚本 %q 缺少来源 URL", s.Name)
	}

	if !s.Enabled {
		return fail("脚本 %q 已停用，无法同步", s.Name)
	}

	src, err := fetchSource(env, *s.SourceURL)
	if err != nil {
		return Result{}, fmt.Errorf("同步失败: %w", err)
	}

	if src.Version == s.Version {
		return reply(false, "脚本 %q 已是最新版本 v%s", s.Name, s.Version)
	}

	oldVersion := applySynced(s, src, nowOf(env))

	changed, err := saveReg(env, r)
	if err != nil {
		return Result{}, err
	}

	if err := env.FS().WriteSource(s.ID, s.Type, src.Code); err != nil {
		return Result{}, fmt.Errorf("写入脚本文件失败: %w", err)
	}
	if err := writeDist(env, s, src.Code); err != nil {
		return Result{}, fmt.Errorf("写入分发产物失败: %w", err)
	}

	return reply(changed, "✅ 已同步脚本 %q v%s → v%s", s.Name, oldVersion, src.Version)
}

// runSyncAll 批量同步所有启用的 synced 脚本。
//
// 必须按指针改写 r.Scripts 内的条目：原先复制出 []registry.Script 副本后
// 只改副本，saveReg 写回的 registry 与改动前完全一致 —— 版本号永远不更新，
// 属于静默数据丢弃。同步到的源码也要写盘，否则 registry 说 v2、文件还是 v1。
func runSyncAll(env *Env, r *registry.Registry) (Result, error) {
	targets := make([]*registry.Script, 0, len(r.Scripts))
	for i := range r.Scripts {
		s := &r.Scripts[i]
		if s.Type == registry.TypeSynced && s.Enabled && !s.Deleted && s.SourceURL != nil {
			targets = append(targets, s)
		}
	}

	if len(targets) == 0 {
		return reply(false, "📭 没有需要同步的脚本")
	}

	updated := 0
	failed := 0
	updatedEntries := make([]*registry.Script, 0, len(targets))
	syncedSrc := make([]*sources.Result, 0, len(targets))
	for _, s := range targets {
		src, err := fetchSource(env, *s.SourceURL)
		if err != nil {
			failed++ // 跳过失败的，继续其他
			continue
		}
		if src.Version == s.Version {
			continue
		}
		applySynced(s, src, nowOf(env))
		updated++
		updatedEntries = append(updatedEntries, s)
		syncedSrc = append(syncedSrc, src)
	}

	if updated == 0 {
		if failed > 0 {
			res, err := reply(false, "📭 未同步任何脚本（%d 个抓取失败）", failed)
			if err != nil {
				return res, err
			}
			res.Warnings = append(res.Warnings, fmt.Sprintf("%d 个脚本抓取失败", failed))
			return res, nil
		}
		return reply(false, "📭 所有脚本已是最新版本")
	}

	changed, err := saveReg(env, r)
	if err != nil {
		return Result{}, err
	}

	writeErrs := 0
	for i, s := range updatedEntries {
		if err := env.FS().WriteSource(s.ID, s.Type, syncedSrc[i].Code); err != nil {
			writeErrs++
			continue
		}
		if err := writeDist(env, s, syncedSrc[i].Code); err != nil {
			writeErrs++
		}
	}

	res, err := reply(changed, "✅ 已同步 %d 个脚本", updated)
	if err != nil {
		return res, err
	}
	if failed > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d 个脚本抓取失败，已跳过", failed))
	}
	if writeErrs > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d 个脚本源码/分发产物写盘失败", writeErrs))
	}
	return res, nil
}
