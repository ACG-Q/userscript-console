package main

import (
	"reflect"
	"testing"

	"github.com/acg-q/userscript-console/internal/layout"
)

// clearLayoutEnv 清空四个路径 env，保证测试只看 flag/默认值来源。
func clearLayoutEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"USM_REGISTRY", "USM_SCRIPTS_DIR", "USM_DIST_DIR", "USM_ARCHIVE_PATH"} {
		t.Setenv(k, "")
	}
}

// TestParseLayout默认值 无 flag 无 env → 与历史硬编码默认布局完全一致。
func TestParseLayout默认值(t *testing.T) {
	clearLayoutEnv(t)
	lay, err := parseLayout(nil)
	if err != nil {
		t.Fatalf("默认解析不应报错: %v", err)
	}
	if want := layout.Defaults(); lay != want {
		t.Errorf("默认布局不符:\n got  %+v\n want %+v", lay, want)
	}
}

// TestParseLayout环境变量生效 env 是 flag 与默认值之间的层（D6 优先级链）。
func TestParseLayout环境变量生效(t *testing.T) {
	clearLayoutEnv(t)
	t.Setenv("USM_REGISTRY", "reg.json")
	t.Setenv("USM_DIST_DIR", "cdn")

	lay, err := parseLayout(nil)
	if err != nil {
		t.Fatalf("env 解析不应报错: %v", err)
	}
	if lay.Registry != "reg.json" {
		t.Errorf("USM_REGISTRY 未生效: %q", lay.Registry)
	}
	if lay.Dist != "cdn" {
		t.Errorf("USM_DIST_DIR 未生效: %q", lay.Dist)
	}
	if lay.Scripts != "scripts" || lay.Archive != "archive/commands.json" {
		t.Errorf("未设置的 env 应保持默认: %+v", lay)
	}
}

// TestParseLayoutFlag覆盖环境变量 flag > env（= 与空格两种形态都识别）。
func TestParseLayoutFlag覆盖环境变量(t *testing.T) {
	clearLayoutEnv(t)
	t.Setenv("USM_SCRIPTS_DIR", "js")
	t.Setenv("USM_REGISTRY", "env-reg.json")

	lay, err := parseLayout([]string{"--scripts-dir", "res", "--registry=flag-reg.json"})
	if err != nil {
		t.Fatalf("flag 解析不应报错: %v", err)
	}
	if lay.Scripts != "res" {
		t.Errorf("空格形态 flag 未覆盖 env: %q", lay.Scripts)
	}
	if lay.Registry != "flag-reg.json" {
		t.Errorf("= 形态 flag 未覆盖 env: %q", lay.Registry)
	}
}

// TestParseLayout非法值报错 相对值拒绝 ".." 与反斜杠（防目录穿越/跨平台歧义）。
func TestParseLayout非法值报错(t *testing.T) {
	clearLayoutEnv(t)
	tests := []struct {
		name string
		args []string
	}{
		{"相对值含..", []string{"--scripts-dir=../x"}},
		{"空格形态含..", []string{"--dist-dir", "a/../b"}},
		{"反斜杠", []string{`--archive-path=win\path`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseLayout(tt.args); err == nil {
				t.Errorf("parseLayout(%v) 应报错", tt.args)
			}
		})
	}
}

// Test子命令非法路径flag退出码2 五个触碰数据的子命令必须在入口统一拦截（SPEC-CLI §0.2）。
func Test子命令非法路径flag退出码2(t *testing.T) {
	for _, sub := range []string{"run-command", "project", "build", "cleanup", "doctor"} {
		t.Run(sub, func(t *testing.T) {
			clearLayoutEnv(t)
			if rc := run([]string{sub, "--scripts-dir=../x"}); rc != 2 {
				t.Errorf("%s 非法路径 flag 应退出 2, got %d", sub, rc)
			}
		})
	}
}

// TestStripLayoutFlags 严格参数校验的子命令用它剔除路径 flag，避免误判未知参数。
func TestStripLayoutFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"空参数", nil, []string{}},
		{"等号形态", []string{"doctor", "--registry=r.json", "--check"}, []string{"doctor", "--check"}},
		{"空格形态", []string{"--dist-dir", "cdn", "build", "--root=."}, []string{"build", "--root=."}},
		{"裸flag在末尾", []string{"doctor", "--registry"}, []string{"doctor"}},
		{"非路径flag不受影响", []string{"build", "--root", "."}, []string{"build", "--root", "."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripLayoutFlags(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("stripLayoutFlags(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

// TestDoctor接受路径Flag doctor 参数严格校验，剔除后应正常跑完而非报未知参数。
func TestDoctor接受路径Flag(t *testing.T) {
	clearLayoutEnv(t)
	root := t.TempDir()
	rc := run([]string{"doctor", "--root", root, "--registry", "reg.json", "--dist-dir=cdn"})
	if rc != 0 {
		t.Errorf("doctor 接受路径 flag 应正常退出 0（未开 --check）, got %d", rc)
	}
}
