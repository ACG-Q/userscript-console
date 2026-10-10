package main

import (
	"strings"

	"github.com/acg-q/userscript-console/internal/layout"
)

// layoutFlagNames 四个路径 flag（设计 §2 D6），与 layout.FromEnv 的四个环境变量一一对应。
var layoutFlagNames = []string{"--registry", "--scripts-dir", "--dist-dir", "--archive-path"}

// parseLayout 解析路径配置：flag > env > 默认（设计 §2 D6）。
// flag 缺失 → env → layout 默认值；非法值（空、".."、反斜杠）→ error，
// 调用方映射为 stderr 中文报错 + exit 2（SPEC-CLI §0.2 用法错误）。
func parseLayout(args []string) (layout.Layout, error) {
	return layout.FromEnv().WithFlags(
		flagVal(args, "--registry"),
		flagVal(args, "--scripts-dir"),
		flagVal(args, "--dist-dir"),
		flagVal(args, "--archive-path"),
	)
}

// stripLayoutFlags 返回剔除四个路径 flag（含其值参数）后的参数，
// 供参数严格校验的子命令（doctor）先归一，避免把路径 flag 误判为未知参数。
// 支持 --flag=value 与 --flag value 两种形态；裸 flag 缺值时直接吞到末尾
// （是否缺值由 parseLayout/WithFlags 校验并报错）。
func stripLayoutFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		matched := false
		for _, f := range layoutFlagNames {
			if a == f {
				matched = true
				i++ // 消费单独的值参数
				break
			}
			if strings.HasPrefix(a, f+"=") {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, a)
		}
	}
	return out
}

// flagVal 取 --name=value 或 --name value 形态的 flag 值；缺失 → 空串。
// 取法与既有 --root 解析（parseRootFlag）保持一致。
func flagVal(args []string, name string) string {
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], name+"=") {
			return strings.TrimPrefix(args[i], name+"=")
		}
		if args[i] == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
