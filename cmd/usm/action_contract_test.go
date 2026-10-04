package main

// 本文件锁定 action.yml 的对外契约。
//
// 设计背景（v1.0.0 → v1.0.1 的教训）：
//   action.yml 曾试图内置「当前二进制版本 + sha256」，由 release CI 回填。
//   结果 release 卡死在「写回 action.yml」——版本号只有打完 tag 才知道，
//   tag 又由 release workflow 处理，形成循环依赖；且写回的正则还需跨过
//   description 行，漏了就静默 exit 1。
//
// 现在改为：**action.yml 不存任何版本常量**，binary-version / binary-sha256
// 由调用方在 workflow 里显式声明，默认为空。本文件锁死这个新契约。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func loadActionRawForTest(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type actionYAML struct {
	Name    string                            `yaml:"name"`
	Inputs  map[string]map[string]interface{} `yaml:"inputs"`
	Outputs map[string]map[string]interface{} `yaml:"outputs"`
	Runs    struct {
		Using string `yaml:"using"`
		Steps []struct {
			Name string            `yaml:"name"`
			Run  string            `yaml:"run"`
			Env  map[string]string `yaml:"env"`
			Uses string            `yaml:"uses"`
			If   string            `yaml:"if"`
		} `yaml:"steps"`
	} `yaml:"runs"`
}

func loadActionYAML(t *testing.T) (*actionYAML, string) {
	t.Helper()
	path := filepath.Join("..", "..", "action.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 action.yml 失败: %v", err)
	}
	var doc actionYAML
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("action.yml 解析失败: %v", err)
	}
	return &doc, string(raw)
}

// TestActionContractInterfaceFrozen 对调用方的既有接口不可变（SPEC-ACTION §5）。
func TestActionContractInterfaceFrozen(t *testing.T) {
	doc, _ := loadActionYAML(t)

	requiredInputs := []string{
		"command", "github-token", "comment-body", "comment-user",
		"repo-owner", "issue-number", "registry-schema-version",
		"keep", "apply", "version", "use-binary",
		"binary-version", "binary-sha256",
	}
	for _, name := range requiredInputs {
		if _, ok := doc.Inputs[name]; !ok {
			t.Errorf("action.yml 缺少 input %q", name)
		}
	}

	requiredOutputs := []string{"authorized", "changed", "result", "warnings"}
	for _, name := range requiredOutputs {
		if _, ok := doc.Outputs[name]; !ok {
			t.Errorf("action.yml 缺少 output %q", name)
		}
	}
}

// TestActionNoTopLevelEnv 是真实 CI 故障的回归锁。
//
// GitHub Action 元数据只认 name/description/author/branding/inputs/outputs/runs。
// 顶层 `env:` 会被**静默忽略** —— 不报错不警告，只是 ${{ env.X }} 展开为空串，
// 曾导致下载 URL 变成 `.../download/v/usm-linux-amd64`（版本号消失）→ 404。
func TestActionNoTopLevelEnv(t *testing.T) {
	_, raw := loadActionYAML(t)

	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["env"]; ok {
		t.Error("action.yml 顶层不应有 env: —— GitHub Action 元数据不认它，会静默忽略。")
	}
}

// TestActionBinaryInputsDefaultEmpty 关键：binary-version / binary-sha256
// 的 default 必须为空 —— 版本由调用方声明，本文件不持有任何版本常量。
//
// 若哪天又有人想「让 release 回填」，这条会红：那会重新引入
// 「tag → 回填 → 推分支 → tag 语义被污染」的循环依赖。
func TestActionBinaryInputsDefaultEmpty(t *testing.T) {
	doc, _ := loadActionYAML(t)

	for _, name := range []string{"binary-version", "binary-sha256"} {
		in, ok := doc.Inputs[name]
		if !ok {
			t.Errorf("缺少 input %q", name)
			continue
		}
		got, _ := in["default"].(string)
		if got != "" {
			t.Errorf("input %q 的 default 应为空（版本由调用方声明），实际 %q", name, got)
		}
	}
}

// TestActionFetchBinaryValidatesParams 二进制模式必须在下载前校验参数，
// 缺失时报**明确错误**，而不是发出畸形 URL 让 curl 报 404。
func TestActionFetchBinaryValidatesParams(t *testing.T) {
	doc, _ := loadActionYAML(t)

	var fetchStep string
	for _, s := range doc.Runs.Steps {
		if strings.Contains(s.Name, "Fetch") {
			fetchStep = s.Run
			break
		}
	}
	if fetchStep == "" {
		t.Fatal("找不到 Fetch binary 步骤（v1 二进制模式未实现？）")
	}

	if !strings.Contains(fetchStep, `if [ -z "$WANT_VERSION" ]`) {
		t.Error("应显式检查 WANT_VERSION 为空并报错")
	}
	if !strings.Contains(fetchStep, "binary-sha256") || !strings.Contains(fetchStep, "64") {
		t.Error("应校验 WANT_SHA256 为 64 位十六进制")
	}
	// 两个失败分支都要 exit 1
	if strings.Count(fetchStep, "exit 1") < 3 {
		t.Errorf("应有 3 处 exit 1（版本空 / sha 格式错 / sha 不匹配），实际 %d",
			strings.Count(fetchStep, "exit 1"))
	}
	// sha 不匹配时应提示去 checksums.txt 查新值
	if !strings.Contains(fetchStep, "checksums.txt") {
		t.Error("sha 不匹配时应提示从 release 的 checksums.txt 取新值")
	}
}

// TestActionFetchBinaryEnvSources Fetch 步骤的 env 必须来自 inputs，
// 不能来自顶层 env（后者不被识别）。
func TestActionFetchBinaryEnvSources(t *testing.T) {
	doc, _ := loadActionYAML(t)

	found := false
	for _, s := range doc.Runs.Steps {
		if !strings.Contains(s.Name, "Fetch") {
			continue
		}
		for _, key := range []string{"WANT_VERSION", "WANT_SHA256"} {
			if strings.Contains(s.Env[key], "inputs.") {
				found = true
			} else {
				t.Errorf("Fetch 步骤的 %s 应来自 inputs.*，实际 %q", key, s.Env[key])
			}
		}
	}
	if !found {
		t.Error("Fetch 步骤未从 inputs 读取版本与校验和")
	}
}

// TestActionValidatesJSONBeforeOutput 输出前必须先校验 usm 输出的 JSON 合法性。
//
// 真实事故：build/project/cleanup 未实现 --json，输出人类可读文本，
// jq 解析失败 → $GITHUB_OUTPUT 的 heredoc 写入不完整 →
// GitHub 报 "Matching delimiter not found" + exit 5。
//
// 契约本身由 json_output_test.go 锁定；这里锁「action 侧要有防线」。
func TestActionValidatesJSONBeforeOutput(t *testing.T) {
	doc, _ := loadActionYAML(t)

	var runStep string
	for _, s := range doc.Runs.Steps {
		if s.Name == "Run" {
			runStep = s.Run
		}
	}
	if runStep == "" {
		t.Fatal("找不到 Run 步骤")
	}

	if !strings.Contains(runStep, "jq -e . usm-out.json") {
		t.Error("Run 步骤应在写 $GITHUB_OUTPUT 前用 jq 校验 JSON 合法性")
	}
	if !strings.Contains(runStep, "exit 6") {
		t.Error("JSON 非法应以专用退出码（6）退出，便于与业务错误区分")
	}

	// heredoc 分隔符必须复用同一变量
	if !strings.Contains(runStep, `EOF_ID="USM_EOF_$(date +%s%N)"`) {
		t.Error("heredoc 分隔符应只求值一次（共享 EOF_ID）")
	}
	if strings.Contains(runStep, "<<USM_EOF_") {
		t.Error("heredoc 分隔符不应内联自求值（跨秒会失配）")
	}
}

// TestActionNoSelfEvaluatingHeredoc 分隔符自求值（<<$(date...)）跨秒会失配。
func TestActionNoSelfEvaluatingHeredoc(t *testing.T) {
	raw := loadActionRawForTest(t)

	if m := regexp.MustCompile(`<<\S*\$\(`).FindAllString(raw, -1); len(m) > 0 {
		t.Errorf("仍有自求值 heredoc 分隔符: %v", m)
	}
}

// TestActionPAGESBaseDerivation PAGES_BASE 推导必须用 bash 原生参数替换，
// 不能套一层 $(echo ...) —— 之前那层会吃掉转义。
func TestActionPAGESBaseDerivation(t *testing.T) {
	doc, _ := loadActionYAML(t)

	var runBody string
	for _, s := range doc.Runs.Steps {
		if s.Name == "Run" {
			runBody = s.Run
		}
	}
	if runBody == "" {
		t.Fatal("找不到 Run 步骤")
	}

	if !strings.Contains(runBody, `PAGES_BASE:=https://${GITHUB_REPOSITORY/\//-}.github.io`) {
		t.Error("PAGES_BASE 推导写法不符（应为 bash 原生参数替换，无 $(echo) 包裹）")
	}
}

// TestActionCommandWhitelist 子命令必须走白名单 case，未知值 exit 2。
func TestActionCommandWhitelist(t *testing.T) {
	doc, _ := loadActionYAML(t)

	var runBody string
	for _, s := range doc.Runs.Steps {
		if s.Name == "Run" {
			runBody = s.Run
		}
	}
	if runBody == "" {
		t.Fatal("找不到 Run 步骤")
	}

	for _, cmd := range []string{"run-command", "project", "build", "cleanup", "doctor"} {
		if !strings.Contains(runBody, cmd+")") {
			t.Errorf("子命令白名单缺 %s", cmd)
		}
	}
	if !strings.Contains(runBody, "exit 2") {
		t.Error("未知子命令应 exit 2")
	}
}
