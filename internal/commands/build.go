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
	if env.PagesBase == "" {
		return fail("build 命令需要配置 PAGES_BASE 环境变量")
	}

	r, err := loadReg(env)
	if err != nil {
		return Result{}, err
	}

	built := 0
	skipped := 0
	errs := 0

	distDir := filepath.Join(env.Root, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("创建 dist 目录失败: %w", err)
	}

	for i := range r.Scripts {
		s := &r.Scripts[i]
		if s.Deleted || s.Type != registry.TypeSelf {
			skipped++
			continue
		}

		srcCode, err := script.ReadSource(env.Root, s.ID, s.Type)
		if err != nil {
			errs++
			continue
		}

		if err := script.WriteDist(env.Root, s.ID, srcCode); err != nil {
			errs++
			continue
		}
		built++
	}

	msg := fmt.Sprintf("✅ 构建完成：\n\n"+
		"- 已构建: %d 个脚本\n"+
		"- 已跳过: %d 个（已删除或非 self 类型）\n",
		built, skipped)

	if errs > 0 {
		msg += fmt.Sprintf("- 错误: %d 个\n", errs)
	}

	if env.PagesBase != "" {
		msg += fmt.Sprintf("\n📦 站点基址: %s/dist/\n", env.PagesBase)
	}

	return reply(built > 0, "%s", msg)
}

// listDist 列出 dist/ 目录内容。
func listDist(root string) ([]string, error) {
	distDir := filepath.Join(root, "dist")
	entries, err := os.ReadDir(distDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	return files, nil
}
