package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/script"
)

func init() {
	Register(Command{
		Name:  "build",
		Help:  "构建站点产物（dist/）",
		Usage: "/build",
		Run:   runBuild,
	})
}

// runBuild 构建站点产物。
func runBuild(env *Env, args string, codeBlocks []string) (Result, error) {
	r, err := loadReg(env)
	if err != nil {
		return Result{}, err
	}

	// 检查是否有 PagesBase 配置
	if env.PagesBase == "" {
		return fail("build 命令需要配置 PAGES_BASE 环境变量")
	}

	// 构建分发产物
	distDir := filepath.Join(env.Root, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("创建 dist 目录失败: %w", err)
	}

	built := 0
	for i := range r.Scripts {
		s := &r.Scripts[i]
		if s.Deleted || s.Type != registry.TypeSelf {
			continue
		}

		// 读取源码
		srcCode, err := script.ReadSource(env.Root, s.ID, s.Type)
		if err != nil {
			return Result{}, fmt.Errorf("读取脚本 %s 失败: %w", s.ID, err)
		}

		// 写入分发产物
		if err := script.WriteDist(env.Root, s.ID, srcCode); err != nil {
			return Result{}, fmt.Errorf("写入分发产物失败: %w", err)
		}
		built++
	}

	return reply(true, "✅ 已构建 %d 个脚本的分发产物", built)
}
