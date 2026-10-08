package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/script"
)

// DoctorReport usm doctor --json 输出（SPEC-CLI §5）。
type DoctorReport struct {
	Problems []string `json:"problems"`
	OK       bool     `json:"ok"`
}

// Check 汇总数据一致性检查结果，不做任何输出。
// 调用方自行决定输出格式 —— action.yml 需要 {authorized, changed, result} 结构，
// 而 CLI 直接跑 doctor 时需要人类可读文本或 DoctorReport，两者不能共用一次调用。
func Check(root string) (problems []string, ok bool) {
	problems = doctorProblems(root)
	return problems, len(problems) == 0
}

// RunDoctor 执行数据一致性自检（SPEC-DATA §6 六条）。
// check=true 时打印逐条问题；json=true 时输出 JSON。返回进程退出码（0/1）。
func RunDoctor(root string, check, asJSON bool) int {
	problems := doctorProblems(root)
	rep := DoctorReport{Problems: problems, OK: len(problems) == 0}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(rep)
		if check && !rep.OK {
			return 1
		}
		return boolToInt(!rep.OK && check)
	}
	if rep.OK {
		fmt.Println("doctor: 数据一致性检查通过")
		return 0
	}
	for _, p := range problems {
		fmt.Println("问题: " + p)
	}
	if check {
		return 1
	}
	return 0
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// doctorProblems 收集全部问题（空 = 健康）。
func doctorProblems(root string) []string {
	var probs []string
	add := func(format string, args ...any) { probs = append(probs, fmt.Sprintf(format, args...)) }

	// 1. registry 可解析 + schema + id/type（Parse 内部已校验）
	reg, err := registry.Load(filepath.Join(root, "registry.json"))
	if err != nil {
		add("registry.json 无法解析: %v", err)
		return probs // 后续检查依赖 registry，直接返回
	}
	if reg.Schema != registry.SchemaVersion {
		add("registry schema=%d，期望 %d", reg.Schema, registry.SchemaVersion)
	}

	seenDirs := map[string]bool{}

	for _, s := range reg.Scripts {
		// 2/3. 源码文件存在性（按软删状态双向校验）
		srcPath := scriptSourcePath(root, s)
		distPath := filepath.Join(root, script.DistPath(s.ID))
		srcExists := fileExists(srcPath)
		distExists := fileExists(distPath)

		if !s.Deleted {
			if !srcExists {
				add("条目 %s(%s) 缺源码文件 %s", s.ID, s.Name, srcPath)
			}
		} else {
			if srcExists {
				add("已删除条目 %s(%s) 仍存在源码 %s", s.ID, s.Name, srcPath)
			}
			if distExists {
				add("已删除条目 %s(%s) 仍存在安装包 %s（条目已删而 dist 还在）", s.ID, s.Name, distPath)
			}
		}

		// 5. 孤儿检测：登记源码目录归属
		if s.Type == registry.TypeSelf {
			seenDirs[filepath.Join(root, "scripts", "self", s.ID)] = true
		} else {
			seenDirs[filepath.Join(root, "scripts", "synced", s.ID)] = true
		}
	}

	// 5b. 盘上存在但 registry 无条目 → 孤儿目录
	for _, kind := range []string{"self", "synced"} {
		base := filepath.Join(root, "scripts", kind)
		entries, err := os.ReadDir(base)
		if err != nil {
			continue // 目录不存在 = 无孤儿
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if !seenDirs[filepath.Join(base, e.Name())] {
				add("孤儿目录: scripts/%s/%s（registry 无对应条目）", kind, e.Name())
			}
		}
	}

	// 6. archive/commands.json 可解析 + 无重复 command_id
	archivePath := filepath.Join(root, "archive", "commands.json")
	if data, err := os.ReadFile(archivePath); err == nil {
		var arch struct {
			Schema   int `json:"schema"`
			Commands []struct {
				CommandID string `json:"command_id"`
			} `json:"commands"`
		}
		if err := json.Unmarshal(data, &arch); err != nil {
			add("archive/commands.json 无法解析: %v", err)
		} else {
			seen := map[string]bool{}
			for _, c := range arch.Commands {
				if c.CommandID == "" {
					continue // 旧归档无幂等键：跳过（下轮 cleanup 合并时回填）
				}
				if seen[c.CommandID] {
					add("archive/commands.json 存在重复 command_id: %s", c.CommandID)
				}
				seen[c.CommandID] = true
			}
		}
	}
	return probs
}

func scriptSourcePath(root string, s registry.Script) string {
	if s.Type == registry.TypeSelf {
		return filepath.Join(root, "scripts", "self", s.ID, "index.js")
	}
	return filepath.Join(root, "scripts", "synced", s.ID, "script.user.js")
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// ContainsAll 诊断辅助（供测试与错误信息使用）。
func ContainsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
