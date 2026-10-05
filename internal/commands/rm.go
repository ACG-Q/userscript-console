package commands

import (
	"strings"

	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/script"
)

func init() {
	Register(Command{
		Name:  "rm",
		Help:  "软删除脚本（保留条目支持复活）",
		Usage: "/rm <id|名称|来源URL>",
		Run:   runRm,
	})
}

// runRm 软删除脚本。I-4: 软删除后保留条目，支持同源复活。
func runRm(env *Env, args string, codeBlocks []string) (Result, error) {
	r, err := loadReg(env)
	if err != nil {
		return Result{}, err
	}

	key := strings.TrimSpace(args)
	if key == "" {
		return fail("用法: /rm <id|名称|来源URL>")
	}

	s := findEntry(r, key)
	if s == nil {
		return fail("未找到脚本 %q，请用 /list 查看全部", key)
	}

	if s.Deleted {
		return reply(false, "脚本 %q 已是已删除状态（ID: %s）", s.Name, s.ID)
	}

	// 软删除：设置 Deleted=true，保持其他字段不变
	s.Deleted = true
	s.UpdatedAt = rfc3339(nowOf(env))

	changed, err := saveReg(env, r)
	if err != nil {
		return Result{}, err
	}

	// 移除脚本文件（如果是 self 类型）
	if s.Type == registry.TypeSelf {
		if err := script.RemoveSource(env.Root, s.ID, s.Type); err != nil {
			return reply(changed, "⚠️ 已软删除脚本 %q，但清理文件失败: %v", s.Name, err)
		}
	}

	return reply(changed, "✅ 已软删除脚本 %q（ID: %s，支持 /add 复活）", s.Name, s.ID)
}
