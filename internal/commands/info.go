package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/acg-q/userscript-console/internal/escape"
)

func init() {
	Register(Command{
		Name:  "info",
		Help:  "查看单个脚本的完整元信息、Issue/文档链接与最近 changelog",
		Usage: "/info <id|来源URL|名称>",
		Run:   runInfo,
	})
}

// infoChangelogShow changelog 展示条数上限。
const infoChangelogShow = 5

// runInfo 按 id / 来源 URL / 精确名称查单条。
func runInfo(env *Env, args string, code []string) (Result, error) {
	key := strings.TrimSpace(args)
	if key == "" {
		return Result{}, errors.New("用法: /info <id|来源URL|名称>")
	}
	r, err := loadReg(env)
	if err != nil {
		return Result{}, err
	}
	s := findEntry(r, key)
	if s == nil {
		return fail("未找到脚本 %q，请用 /list 查看全部条目，或检查 ID/来源 URL/名称是否准确。", key)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📖 脚本详情：**%s**\n\n", escape.EscapeMdText(s.Name))
	b.WriteString("| 字段 | 值 |\n|---|---|\n")
	row := func(k, v string) { fmt.Fprintf(&b, "| %s | %s |\n", k, escape.EscapeMdCell(v)) }

	row("ID", "`"+s.ID+"`")
	row("名称", s.Name)
	row("版本", s.Version)
	if s.Type == "self" {
		row("类型", "self（自写脚本）")
	} else {
		row("类型", "synced（同步脚本）")
	}
	status := "启用"
	if !s.Enabled {
		status = "停用"
	}
	if s.Deleted {
		status = "已删除（软删除）"
	}
	row("状态", status)
	row("描述", orDash(s.Description))
	row("作者", orDash(s.Author))
	row("命名空间", orDash(s.Namespace))
	row("@match", joinOrDash(s.Match))
	row("@grant", joinOrDash(s.Grant))
	if s.SourceURL != nil {
		v := *s.SourceURL
		st := ""
		if s.SourceType != nil {
			st = "（" + *s.SourceType + "）"
		}
		row("来源", v+st)
	} else {
		row("来源", "自写（无外部来源）")
	}
	if s.LastSyncedAt != nil {
		row("最后同步", *s.LastSyncedAt)
	} else {
		row("最后同步", "从未同步")
	}
	row("创建时间", orDash(s.CreatedAt))
	row("更新时间", orDash(s.UpdatedAt))
	if s.Issue != nil && s.Issue.URL != "" {
		row("Issue 链接", s.Issue.URL)
	} else {
		row("Issue 链接", "未关联")
	}
	if s.Documentation != "" {
		row("文档链接", s.Documentation)
	} else {
		row("文档链接", "未生成")
	}
	if s.SyncEnabled != nil && !*s.SyncEnabled {
		row("自动同步", "已关闭")
	}

	b.WriteString("\n**changelog（最近 ")
	n := len(s.Changelog)
	show := n
	if show > infoChangelogShow {
		show = infoChangelogShow
	}
	fmt.Fprintf(&b, "%d 条）**\n\n", show)
	if show == 0 {
		b.WriteString("_暂无 changelog 记录。_\n")
	} else {
		b.WriteString("| 版本 | 日期 | 说明 |\n|---|---|---|\n")
		for _, c := range s.Changelog[:show] {
			fmt.Fprintf(&b, "| %s | %s | %s |\n",
				escape.EscapeMdCell(c.Version), escape.EscapeMdCell(c.Date), escape.EscapeMdCell(c.Note))
		}
	}
	return reply(false, "%s", b.String())
}

// orDash 空值占位。
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// joinOrDash 多值字段拼接。
func joinOrDash(list []string) string {
	if len(list) == 0 {
		return "—"
	}
	var parts []string
	for _, v := range list {
		parts = append(parts, "`"+v+"`")
	}
	return strings.Join(parts, " ")
}
