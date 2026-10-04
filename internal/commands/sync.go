package commands

import (
	"fmt"
	"strings"

	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/script"
)

func init() {
	Register(Command{
		Name:  "sync",
		Help:  "同步脚本（拉取最新代码并更新 registry）",
		Usage: "/sync <id|名称|来源URL> | /sync-all",
		Run:   runSync,
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

	// 抓取最新版本
	src, err := fetchSource(env, *s.SourceURL)
	if err != nil {
		return Result{}, fmt.Errorf("同步失败: %w", err)
	}

	// 检查版本变更
	if src.Version == s.Version {
		return reply(false, "脚本 %q 已是最新版本 v%s", s.Name, s.Version)
	}

	// 更新 registry
	oldVersion := s.Version
	s.Version = src.Version
	s.Description = src.Description
	s.Author = src.Author
	s.Match = src.Match
	s.Grant = src.Grant
	s.UpdatedAt = rfc3339(nowOf(env))
	s.LastSyncedAt = stringPtr(rfc3339(nowOf(env)))
	s.Changelog = append([]registry.ChangelogEntry{
		{Version: src.Version, Date: dateStr(nowOf(env)), Note: fmt.Sprintf("同步更新 v%s → v%s", oldVersion, src.Version)},
	}, s.Changelog...)
	if len(s.Changelog) > 10 {
		s.Changelog = s.Changelog[:10]
	}

	changed, err := saveReg(env, r)
	if err != nil {
		return Result{}, err
	}

	// 更新脚本文件
	if err := script.WriteSource(env.Root, s.ID, s.Type, src.Code); err != nil {
		return Result{}, fmt.Errorf("写入脚本文件失败: %w", err)
	}

	return reply(changed, "✅ 已同步脚本 %q v%s → v%s", s.Name, oldVersion, src.Version)
}

// runSyncAll 批量同步所有启用的 synced 脚本。
func runSyncAll(env *Env, r *registry.Registry) (Result, error) {
	synced := make([]registry.Script, 0)
	for i := range r.Scripts {
		s := &r.Scripts[i]
		if s.Type == registry.TypeSynced && s.Enabled && !s.Deleted && s.SourceURL != nil {
			synced = append(synced, *s)
		}
	}

	if len(synced) == 0 {
		return reply(false, "📭 没有需要同步的脚本")
	}

	updated := 0
	for i := range synced {
		s := &synced[i]
		src, err := fetchSource(env, *s.SourceURL)
		if err != nil {
			continue // 跳过失败的，继续其他
		}
		if src.Version == s.Version {
			continue
		}
		oldVersion := s.Version
		s.Version = src.Version
		s.Description = src.Description
		s.Match = src.Match
		s.Grant = src.Grant
		s.UpdatedAt = rfc3339(nowOf(env))
		s.LastSyncedAt = stringPtr(rfc3339(nowOf(env)))
		s.Changelog = append([]registry.ChangelogEntry{
			{Version: src.Version, Date: dateStr(nowOf(env)), Note: fmt.Sprintf("同步更新 v%s → v%s", oldVersion, src.Version)},
		}, s.Changelog...)
		updated++
	}

	if updated == 0 {
		return reply(false, "📭 所有脚本已是最新版本")
	}

	changed, err := saveReg(env, r)
	if err != nil {
		return Result{}, err
	}
	return reply(changed, "✅ 已同步 %d 个脚本", updated)
}
