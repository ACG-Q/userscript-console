package commands

import (
	"fmt"
	"strings"

	"github.com/acg-q/userscript-console/internal/escape"
	"github.com/acg-q/userscript-console/internal/registry"
)

func init() {
	Register(Command{
		Name:  "list",
		Help:  "列出全部活动脚本（已软删除的不列）",
		Usage: "/list",
		Run:   runList,
	})
}

// runList 活动条目表（ID/名称/版本/类型/状态）+ 统计行。
func runList(env *Env, args string, code []string) (Result, error) {
	r, err := loadReg(env)
	if err != nil {
		return Result{}, err
	}
	active := make([]*registry.Script, 0, len(r.Scripts))
	deleted := 0
	for i := range r.Scripts {
		s := &r.Scripts[i]
		if s.Deleted {
			deleted++
			continue
		}
		active = append(active, s)
	}
	if len(active) == 0 {
		return reply(false, "📭 暂无活动脚本，用 `/add <来源URL>` 或 `/add` + 代码块添加第一个脚本。%s", deletedHint(deleted))
	}
	var b strings.Builder
	b.WriteString("📋 活动脚本列表：\n\n")
	b.WriteString("| ID | 名称 | 版本 | 类型 | 状态 |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, s := range active {
		state := "启用"
		if !s.Enabled {
			state = "停用"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n",
			s.ID, escape.EscapeMdCell(s.Name), escape.EscapeMdCell(s.Version), s.Type, state)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "共 %d 个%s", len(active), deletedHint(deleted))
	return reply(false, "%s", b.String())
}

// deletedHint 「另有 M 个已删除」尾巴（M=0 时为空串）。
func deletedHint(deleted int) string {
	if deleted <= 0 {
		return ""
	}
	return fmt.Sprintf("（另有 %d 个已删除）", deleted)
}
