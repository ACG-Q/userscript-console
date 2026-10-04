// Package snapshot 实现 `usm snapshot check|update` 的通用快照引擎。
//
// 设计（DR-6 口径）：快照是**新项目自产基线**（tests/snapshot/），
// 由各模块注册的 Generator（输入快照 → 输出文件映射）产生；
// check 逐字符比对（换行归一化 \r\n→\n），不一致打印逐行 unified diff；update 写回。
// Generator 由 cmd 层在集成阶段注册（issuepage/pages 投影与站点产物）。
package snapshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Generator 生成快照内容：相对路径（用 / 分隔）→ 文件内容（\n 行尾）。
type Generator func() (map[string]string, error)

var defaultGen Generator

// RegisterDefault 注册默认生成器（cmd 层集成时调用；重复注册 panic 属编程错误）。
func RegisterDefault(g Generator) {
	if defaultGen != nil {
		panic("snapshot: default generator already registered")
	}
	defaultGen = g
}

// Run 实现 CLI 子命令：args = ["check"] | ["update"]。
// 返回值为进程退出码语义：0 成功，1 不一致/错误，2 用法错误。
func Run(dir string, args []string) int {
	if len(args) != 1 || (args[0] != "check" && args[0] != "update") {
		fmt.Fprintln(os.Stderr, "ERROR: 用法: usm snapshot <check|update>")
		return 2
	}
	if defaultGen == nil {
		fmt.Fprintln(os.Stderr, "ERROR: 未注册快照生成器")
		return 1
	}
	files, err := defaultGen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: 生成快照失败: %v\n", err)
		return 1
	}
	if args[0] == "update" {
		changed, err := Update(dir, files)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: 写回快照失败: %v\n", err)
			return 1
		}
		for _, c := range changed {
			fmt.Printf("更新快照: %s\n", c)
		}
		if len(changed) == 0 {
			fmt.Println("快照无变化")
		}
		return 0
	}
	diffs := Check(dir, files)
	if len(diffs) == 0 {
		fmt.Println("快照一致")
		return 0
	}
	for _, d := range diffs {
		fmt.Println(d)
	}
	fmt.Fprintf(os.Stderr, "ERROR: %d 个快照不一致（usm snapshot update 可写回新基线，变更需人工审阅）\n", len(diffs))
	return 1
}

// Check 比对目录与生成结果；返回差异描述列表（空 = 一致）。
// 比对规则：换行归一化 \r\n→\n 后逐字符；目录中不在生成结果里的文件也算差异（孤儿基线）。
func Check(dir string, files map[string]string) []string {
	onDisk := readTree(dir)
	var diffs []string
	for _, rel := range sortedKeys(files) {
		want := normalize(files[rel])
		got, exists := onDisk[rel]
		switch {
		case !exists:
			diffs = append(diffs, fmt.Sprintf("缺失: %s（基线不存在）", rel))
		case got != want:
			diffs = append(diffs, fmt.Sprintf("不一致: %s\n%s", rel, unifiedDiff(rel, got, want)))
		}
	}
	for _, rel := range sortedKeys(onDisk) {
		if ignoredRel(rel) {
			continue
		}
		if _, ok := files[rel]; !ok {
			diffs = append(diffs, fmt.Sprintf("孤儿: %s（基线存在但生成结果没有）", rel))
		}
	}
	return diffs
}

// ignoredRel 基线目录内允许存在的说明性文件（不参与孤儿判定与清理）。
func ignoredRel(rel string) bool {
	return rel == "README.md"
}

// Update 把生成结果写回目录（原子写），返回内容有变化的文件列表。
func Update(dir string, files map[string]string) ([]string, error) {
	onDisk := readTree(dir)
	var changed []string
	for _, rel := range sortedKeys(files) {
		want := normalize(files[rel])
		if got, exists := onDisk[rel]; exists && got == want {
			continue
		}
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return changed, err
		}
		if err := atomicWrite(path, []byte(want)); err != nil {
			return changed, err
		}
		changed = append(changed, rel)
	}
	// 清理孤儿基线（README.md 等说明文件不清理）
	for _, rel := range sortedKeys(onDisk) {
		if ignoredRel(rel) {
			continue
		}
		if _, ok := files[rel]; !ok {
			path := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return changed, err
			}
			changed = append(changed, rel+"（已删除孤儿）")
		}
	}
	return changed, nil
}

// ── 内部工具 ──────────────────────────────────────────────

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimRight(s, "\n") // 末尾换行归一：基线以单 \n 结尾与否不影响一致性
}

func readTree(dir string) map[string]string {
	out := map[string]string{}
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil // 目录不存在 → 空基线
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		out[filepath.ToSlash(rel)] = normalize(string(data))
		return nil
	})
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".snap-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// unifiedDiff 朴素逐行 diff：找到首个差异行，输出 ±3 行上下文（够定位，不引入依赖）。
func unifiedDiff(name, got, want string) string {
	gl := strings.Split(got, "\n")
	wl := strings.Split(want, "\n")
	max := len(gl)
	if len(wl) < max {
		max = len(wl)
	}
	first := -1
	for i := 0; i < max; i++ {
		if gl[i] != wl[i] {
			first = i
			break
		}
	}
	if first < 0 {
		if len(gl) != len(wl) {
			first = max // 前缀一致，长度不同
		} else {
			return ""
		}
	}
	lo := first - 3
	if lo < 0 {
		lo = 0
	}
	hi := first + 3
	var b strings.Builder
	fmt.Fprintf(&b, "  @@ %s 首个差异在第 %d 行 @@", name, first+1)
	for i := lo; i <= hi; i++ {
		if i < len(gl) {
			fmt.Fprintf(&b, "\n  - %d: %s", i+1, gl[i])
		}
		if i < len(wl) {
			fmt.Fprintf(&b, "\n  + %d: %s", i+1, wl[i])
		}
	}
	return b.String()
}
