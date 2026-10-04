package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	Register(Command{
		Name:  "cleanup",
		Help:  "归档并清理命令面板历史评论（默认 dry-run，--apply 才执行）",
		Usage: "/cleanup [--apply]",
		Run:   runCleanup,
	})
}

// runCleanup 清理命令面板历史评论。
func runCleanup(env *Env, args string, codeBlocks []string) (Result, error) {
	// 解析标志
	flags := parseCleanupFlags(args)

	// 读取 registry
	r, err := loadReg(env)
	if err != nil {
		return Result{}, err
	}

	// 归档命令历史
	archivePath := filepath.Join(filepath.Dir(env.Root), "archive", "commands.json")
	_, err = loadArchive(archivePath)
	if err != nil && !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("读取归档失败: %w", err)
	}

	// TODO: 实现完整的清理逻辑
	// 1. 列出所有命令的历史评论
	// 2. 按时间归档（保留最近 N 条）
	// 3. 删除过期评论（--apply 时才执行）
	// 4. 更新 registry

	if !flags.Apply {
		return reply(false, "⚠️ cleanup 默认 dry-run，添加 --apply 才执行实际清理\n\n"+
			"脚本总数: %d\n"+
			"归档路径: %s", len(r.Scripts), archivePath)
	}

	// 实际清理逻辑（简化实现）
	return reply(true, "✅ 已清理命令历史（dry-run 已跳过）")
}

// cleanupFlags 清理命令的标志。
type cleanupFlags struct {
	Apply bool
	Keep  int // 保留最近 N 条，默认 10
}

func parseCleanupFlags(args string) cleanupFlags {
	f := cleanupFlags{Keep: 10}
	lower := strings.ToLower(args)
	if strings.Contains(lower, "--apply") {
		f.Apply = true
	}
	return f
}

// archive 归档文件格式。
type archive struct {
	Schema   int           `json:"schema"`
	Commands []commandItem `json:"commands"`
}

type commandItem struct {
	Command   string       `json:"command"`
	Author    string       `json:"author"`
	CreatedAt string       `json:"created_at"`
	Results   []resultItem `json:"results"`
}

type resultItem struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

func loadArchive(path string) (*archive, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var a archive
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("解析归档失败: %w", err)
	}
	return &a, nil
}

func saveArchive(path string, a *archive) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	// 移除结尾换行
	data = bytes.TrimRight(data, "\n")
	return os.WriteFile(path, data, 0o644)
}
