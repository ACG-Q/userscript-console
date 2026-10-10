package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/acg-q/userscript-console/internal/cleanup"
)

// saveArchive 是 cleanup.Save 的注入缝，测试用于构造保存失败（见 cleanup_edge 思路）。
var saveArchive = cleanup.Save

func init() {
	Register(Command{
		Name:  "cleanup",
		Help:  "归档并清理命令面板历史评论（默认 dry-run，--apply 才执行）",
		Usage: "/cleanup [--apply] [--keep N]",
		Run:   runCleanup,
	})
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
	// --keep N
	for i := 0; i < len(lower)-1; i++ {
		if strings.HasPrefix(lower[i:], "--keep") {
			rest := strings.TrimSpace(lower[i+6:])
			if len(rest) > 0 && rest[0] == '=' {
				rest = rest[1:]
			}
			rest = strings.TrimSpace(rest)
			if n := parseIntStrict(rest); n > 0 {
				f.Keep = n
			}
			break
		}
	}
	return f
}

func parseIntStrict(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// runCleanup 清理命令面板历史评论。
func runCleanup(env *Env, args string, codeBlocks []string) (Result, error) {
	flags := parseCleanupFlags(args)

	r, err := loadReg(env)
	if err != nil {
		return Result{}, err
	}

	archivePath := env.layout().ArchivePath(env.Root)
	existing, loadErr := cleanup.Load(archivePath)
	if loadErr != nil && !os.IsNotExist(loadErr) {
		return Result{}, fmt.Errorf("读取归档失败: %w", loadErr)
	}

	var totalDeleted int
	var totalFetched int

	if env.GHClient != nil && env.IssueNumber > 0 {
		ctx := context.Background()
		comments, err := env.GHClient.ListIssueComments(ctx, env.IssueNumber, 100)
		if err != nil {
			return Result{}, fmt.Errorf("拉取评论失败: %w", err)
		}
		totalFetched = len(comments)

		// 转换为 cleanup.Result
		newResults := make(map[string][]cleanup.Result)
		for _, c := range comments {
			body := c.Body
			cmd := cleanup.ParseCommand(body)
			t, _ := time.Parse(time.RFC3339, c.CreatedAt)
			if t.IsZero() {
				t, _ = time.Parse("2006-01-02T15:04:05Z07:00", c.CreatedAt)
			}
			newResults[cmd] = append(newResults[cmd], cleanup.Result{
				ID:        c.NodeID,
				Author:    c.Author,
				Body:      body,
				CreatedAt: t,
			})
		}

		// 合并归档（保留最近 Keep 条）
		merged := cleanup.MergeArchive(existing, newResults, flags.Keep)

		// 找出需要删除的评论（不在保留集合中的）
		keptIDs := make(map[string]bool)
		for _, cmd := range merged.Commands {
			for _, r := range cmd.Results {
				keptIDs[r.ID] = true
			}
		}

		if flags.Apply {
			// SPEC-DATA.md:103 归档先落盘再删评论；删除失败下轮只补删除。
			if err := saveArchive(archivePath, merged); err != nil {
				return Result{}, fmt.Errorf("保存归档失败: %w", err)
			}
			for _, c := range comments {
				if !keptIDs[c.NodeID] {
					if err := env.GHClient.DeleteComment(ctx, c.NodeID); err != nil {
						return Result{}, fmt.Errorf("删除评论失败（归档已保存，下轮只补删除）: %w", err)
					}
					totalDeleted++
				}
			}
		}
	}

	// 统计归档信息
	totalCommands := 0
	totalEntries := 0
	if existing != nil {
		for _, cmd := range existing.Commands {
			totalCommands++
			totalEntries += len(cmd.Results)
		}
	}
	if totalDeleted > 0 {
		totalEntries -= totalDeleted
	}

	msg := fmt.Sprintf("🧹 cleanup 完成\n\n"+
		"脚本总数: %d\n"+
		"拉取评论: %d\n"+
		"已删除评论: %d\n"+
		"归档路径: %s\n"+
		"归档命令组: %d\n"+
		"归档条目: %d",
		len(r.Scripts),
		totalFetched,
		totalDeleted,
		archivePath,
		totalCommands,
		totalEntries,
	)

	if !flags.Apply {
		msg += "\n\n⚠️ 当前为 dry-run，添加 --apply 执行实际删除"
	}

	return reply(totalDeleted > 0, "%s", msg)
}
