// Package cleanup 处理命令面板历史评论的归档与清理。
package cleanup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Archive 归档文件格式。
type Archive struct {
	Schema   int          `json:"schema"`
	Commands []CommandKey `json:"commands"`
}

// CommandKey 按命令名分组的归档条目。
type CommandKey struct {
	Command   string    `json:"command"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
	Results   []Result  `json:"results"`
}

// Result 单次命令执行结果。
type Result struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// Load 读取归档文件。
func Load(path string) (*Archive, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var a Archive
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("解析归档失败: %w", err)
	}
	if a.Schema == 0 {
		a.Schema = 1
	}
	return &a, nil
}

// archiveFile 是 Save 对临时文件的最小操作面。
// 窄接口仅为测试注入写/刷/关/权限失败分支而存在——
// 这些错误在真实文件系统上无法可靠构造（见 cleanup_edge_test.go）。
type archiveFile interface {
	Write(p []byte) (n int, err error)
	Sync() error
	Close() error
	Name() string
}

// createArchiveTemp 是 os.CreateTemp 的窄接缝，默认实现不变。
var createArchiveTemp = func(dir, pattern string) (archiveFile, error) {
	return os.CreateTemp(dir, pattern)
}

// Save 原子写入归档文件。
func Save(path string, a *Archive) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建归档目录失败: %w", err)
	}
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化归档失败: %w", err)
	}
	data = bytes.TrimRight(data, "\n")
	tmp, err := createArchiveTemp(dir, ".archive-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入归档失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("刷新归档失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭归档文件失败: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("设置归档文件权限失败: %w", err)
	}
	return os.Rename(tmpName, path)
}

// Group 按命令名分组结果条目。
func Group(results []Result) map[string][]Result {
	groups := make(map[string][]Result)
	for _, r := range results {
		cmd := parseCommand(r.Body)
		groups[cmd] = append(groups[cmd], r)
	}
	// 排序每个组的条目（按时间倒序）
	for cmd := range groups {
		sort.Slice(groups[cmd], func(i, j int) bool {
			return groups[cmd][i].CreatedAt.After(groups[cmd][j].CreatedAt)
		})
	}
	return groups
}

// MergeArchive 将新结果合并到归档中，保留每个命令最近 Keep 条。
func MergeArchive(existing *Archive, newResults map[string][]Result, keep int) *Archive {
	if existing == nil {
		existing = &Archive{Schema: 1}
	}
	merged := make(map[string][]Result)
	// 保留现有条目
	for _, cmd := range existing.Commands {
		merged[cmd.Command] = append(merged[cmd.Command], cmd.Results...)
	}
	// 合并新条目
	for cmd, results := range newResults {
		merged[cmd] = append(merged[cmd], results...)
	}
	// 截断每个命令的条目数
	for cmd := range merged {
		if len(merged[cmd]) > keep {
			merged[cmd] = merged[cmd][:keep]
		}
	}
	// 构建归档
	var commands []CommandKey
	for cmd, results := range merged {
		commands = append(commands, CommandKey{
			Command:   cmd,
			Results:   results,
			Author:    results[0].Author,
			CreatedAt: results[0].CreatedAt,
		})
	}
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Command < commands[j].Command
	})
	return &Archive{Schema: 1, Commands: commands}
}

// parseCommand 从评论正文提取命令名。
func parseCommand(body string) string {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) == 0 {
		return "unknown"
	}
	first := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(first, "/") {
		return "unknown"
	}
	// 仅有斜杠时返回 unknown
	if len(first) <= 1 {
		return "unknown"
	}
	parts := strings.SplitN(first[1:], " ", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "unknown"
	}
	return strings.ToLower(parts[0])
}

// ParseCommand 导出 parseCommand 供外部包使用。
func ParseCommand(body string) string { return parseCommand(body) }
