package commands

import (
	"fmt"
	"time"

	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/script"
)

func init() {
	Register(Command{
		Name:  "add",
		Help:  "添加脚本（来源 URL 或自写代码块）",
		Usage: "/add <来源URL> | /add（后跟代码块）",
		Run:   runAdd,
	})
}

// runAdd 添加新脚本。两种模式：
// 1. /add <URL> — 抓取来源并同步到 registry
// 2. /add + 代码块 — 将代码块作为自写脚本添加
func runAdd(env *Env, args string, codeBlocks []string) (Result, error) {
	r, err := loadReg(env)
	if err != nil {
		return Result{}, err
	}

	now := nowOf(env)

	// 模式 1: 来源 URL
	if args != "" && isURL(args) {
		return addFromURL(env, r, args, now)
	}

	// 模式 2: 自写脚本（需要代码块）
	if len(codeBlocks) == 0 {
		return fail("添加自写脚本需要提供代码块，请在评论中包含 ```...``` 代码块")
	}

	return addSelfScript(env, r, codeBlocks[0], now)
}

// addFromURL 从来源 URL 抓取并添加到 registry。
func addFromURL(env *Env, r *registry.Registry, rawURL string, now time.Time) (Result, error) {
	// 检查是否已存在
	if existing := r.FindBySourceURL(rawURL); existing != nil {
		if existing.Deleted {
			// I-4: 复活已删除条目
			existing.Deleted = false
			existing.Enabled = true
			existing.UpdatedAt = rfc3339(now)
			changed, err := saveReg(env, r)
			if err != nil {
				return Result{}, err
			}
			return reply(changed, "✅ 已复活脚本 %q（来源: %s）", existing.Name, rawURL)
		}
		return fail("脚本已存在（ID: %s），无需重复添加", existing.ID)
	}

	// 抓取来源
	src, err := fetchSource(env, rawURL)
	if err != nil {
		return Result{}, err
	}

	id := registry.SourceID(rawURL)
	s := registry.Script{
		ID:           id,
		Type:         registry.TypeSynced,
		Name:         src.Name,
		Version:      src.Version,
		Description:  src.Description,
		Author:       src.Author,
		Match:        src.Match,
		Grant:        src.Grant,
		Enabled:      true,
		Deleted:      false,
		CreatedAt:    rfc3339(now),
		UpdatedAt:    rfc3339(now),
		SourceURL:    &rawURL,
		SourceType:   stringPtr(src.SourceType),
		LastSyncedAt: stringPtr(rfc3339(now)),
		SyncEnabled:  boolPtr(true),
		Changelog: []registry.ChangelogEntry{
			{Version: src.Version, Date: dateStr(now), Note: "初始同步"},
		},
	}

	r.Add(s)
	changed, err := saveReg(env, r)
	if err != nil {
		return Result{}, err
	}

	// 写入脚本文件
	if err := script.WriteSource(env.Root, s.ID, s.Type, src.Code); err != nil {
		return Result{}, fmt.Errorf("写入脚本文件失败: %w", err)
	}

	return reply(changed, "✅ 已添加脚本 %q v%s（来源: %s，ID: %s）", s.Name, s.Version, rawURL, s.ID)
}

// addSelfScript 添加自写脚本。
func addSelfScript(env *Env, r *registry.Registry, code string, now time.Time) (Result, error) {
	// 解析用户脚本头
	header, ok := script.ExtractMeta(code)
	if !ok {
		return fail("无法解析脚本头，请确保代码包含完整的 ==UserScript== 头块")
	}

	id := newSelfID()
	s := registry.Script{
		ID:          id,
		Type:        registry.TypeSelf,
		Name:        header.Name,
		Version:     header.Version,
		Description: header.Description,
		Author:      header.Author,
		Namespace:   header.Namespace,
		Match:       header.Match,
		Grant:       header.Grant,
		Enabled:     true,
		Deleted:     false,
		CreatedAt:   rfc3339(now),
		UpdatedAt:   rfc3339(now),
		Changelog:   []registry.ChangelogEntry{{Version: header.Version, Date: dateStr(now), Note: "初始版本"}},
	}

	// 检查 ID 冲突
	if r.FindByID(id) != nil {
		return fail("脚本 ID 冲突，请检查脚本头中的 @grant/@match 是否有重复")
	}

	r.Add(s)
	changed, err := saveReg(env, r)
	if err != nil {
		return Result{}, err
	}

	// 写入脚本文件
	if err := script.WriteSource(env.Root, s.ID, s.Type, code); err != nil {
		return Result{}, fmt.Errorf("写入脚本文件失败: %w", err)
	}

	return reply(changed, "✅ 已添加自写脚本 %q v%s（ID: %s）", s.Name, s.Version, s.ID)
}

// String helper functions
func stringPtr(s string) *string { return &s }
func boolPtr(b bool) *bool       { return &b }
