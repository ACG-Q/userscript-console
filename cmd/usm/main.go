// Command usm 是油猴脚本控制台 CLI（userscript-console 的层 2 入口）。
//
// 退出码契约（SPEC-CLI §0.2）：
//
//	0 = 成功（含业务失败的结果文本）
//	1 = 致命（数据损坏/网络不可恢复/参数必填缺失）
//	2 = 用法错误（未知子命令/缺参数）
package main

import (
	"fmt"
	"os"

	"github.com/acg-q/userscript-console/internal/cli"
	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/snapshot"
)

// version 由 -ldflags "-X main.version=x.y.z" 注入。
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

// doctorRun 解析 `doctor [--check] [--json] [--root path]`（SPEC-CLI §5）。
func doctorRun(args []string) int {
	root, check, asJSON := ".", false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--check":
			check = true
		case "--json":
			asJSON = true
		case "--root":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "ERROR: --root 缺少参数")
				return 2
			}
			i++
			root = args[i]
		default:
			fmt.Fprintf(os.Stderr, "ERROR: doctor 未知参数 %q\n", args[i])
			return 2
		}
	}
	return cli.RunDoctor(root, check, asJSON)
}

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("usm %s\nregistry-schema-version %d\n", version, registry.SchemaVersion)
		return 0
	case "help", "--help", "-h":
		usage(os.Stdout)
		return 0
	case "snapshot":
		return snapshot.Run("tests/snapshot", args[1:])
	case "doctor":
		return doctorRun(args[1:])
	case "run-command", "project", "build", "cleanup":
		fmt.Fprintf(os.Stderr, "ERROR: 子命令 %q 尚未接线（开发中，见 PLAN 阶段 3）\n", args[0])
		return 1
	default:
		fmt.Fprintf(os.Stderr, "ERROR: 未知子命令 %q\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `usm —— 油猴脚本控制台

用法: usm <command> [flags]

命令:
  run-command    执行命令面板评论中的 /command（回帖语义，见 SPEC-CLI §1）
  project        registry → Issues/版本帖 对账投影
  build          构建站点产物（dist/）
  cleanup        归档并清理命令面板历史评论（默认 dry-run，--apply 才执行）
  doctor         数据一致性自检（--check 供 CI）
  snapshot       快照基线 <check|update>
  version        打印版本与 registry schema 版本
  help           本帮助

全局约定: 默认工作目录即数据根（含 registry.json）；--root 可显式指定。
`)
}
