package main

// 本文件锁定 action.yml 的 release 契约：
//   1. inputs/outputs 接口不可变（SPEC-ACTION §5：v0/v1 切换不得改签名）
//   2. env 必须保留 USM_BINARY_SHA256 / USM_BINARY_VERSION —— release CI 靠正则写回
//   3. 写回正则必须命中且幂等（release 重跑不产生空 diff）
//   4. v1 二进制模式的 SHA256 占位值必须被拒绝（防止「校验了个 0」）
//   5. $GITHUB_OUTPUT heredoc 分隔符开闭一致（见 gha_output_test.go）
//
// 这些断言直接读仓库里的 action.yml —— 改坏 CI 会红，改 action.yml 破坏契约也会红。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type actionYAML struct {
	Name    string                            `yaml:"name"`
	Env     map[string]string                 `yaml:"env"`
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

// TestActionContractInterfaceFrozen 接口不可变：这是 C4-2 的核心约束。
func TestActionContractInterfaceFrozen(t *testing.T) {
	doc, _ := loadActionYAML(t)

	requiredInputs := []string{
		"command", "github-token", "comment-body", "comment-user",
		"repo-owner", "issue-number", "registry-schema-version",
		"keep", "apply", "version",
	}
	for _, name := range requiredInputs {
		if _, ok := doc.Inputs[name]; !ok {
			t.Errorf("action.yml 缺少既有 input %q（接口不可变，SPEC-ACTION §5）", name)
		}
	}

	requiredOutputs := []string{"authorized", "changed", "result", "warnings"}
	for _, name := range requiredOutputs {
		if _, ok := doc.Outputs[name]; !ok {
			t.Errorf("action.yml 缺少 output %q", name)
		}
	}
}

// TestActionEnvHasReleaseConstants release CI 用正则写回这两个常量，
// 缺一个或改名都会让发布流程报「未找到常量」。
func TestActionEnvHasReleaseConstants(t *testing.T) {
	doc, _ := loadActionYAML(t)

	if _, ok := doc.Env["USM_BINARY_SHA256"]; !ok {
		t.Error("env 缺 USM_BINARY_SHA256（release CI 需写回）")
	}
	if _, ok := doc.Env["USM_BINARY_VERSION"]; !ok {
		t.Error("env 缺 USM_BINARY_VERSION（release CI 需写回）")
	}
}

var (
	reSHAConst     = regexp.MustCompile(`(USM_BINARY_SHA256:\s*')[^']*(')`)
	reVersionConst = regexp.MustCompile(`(USM_BINARY_VERSION:\s*')[^']*(')`)
)

// TestActionWriteBackRegexMatches 复刻 release.yml publish job 的写回逻辑。
func TestActionWriteBackRegexMatches(t *testing.T) {
	_, raw := loadActionYAML(t)

	const (
		sha     = "aaaaaaaabbbbbbbbccccccccddddddddeeeeeeeeffffffff0000000011111111"
		version = "1.2.3"
	)

	out := reSHAConst.ReplaceAllString(raw, "${1}"+sha+"${2}")
	out = reVersionConst.ReplaceAllString(out, "${1}"+version+"${2}")

	if out == raw {
		t.Fatal("写回正则未命中 —— release.yml 会报「action.yml 未找到常量」")
	}

	var doc actionYAML
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("写回后 YAML 应仍合法: %v", err)
	}
	if doc.Env["USM_BINARY_SHA256"] != sha {
		t.Errorf("写回后 SHA256 = %q, want %q", doc.Env["USM_BINARY_SHA256"], sha)
	}
	if doc.Env["USM_BINARY_VERSION"] != version {
		t.Errorf("写回后 VERSION = %q, want %q", doc.Env["USM_BINARY_VERSION"], version)
	}

	// 幂等：同值二次写回应无变化（release 重跑不产生空 diff 提交）
	out2 := reSHAConst.ReplaceAllString(out, "${1}"+sha+"${2}")
	out2 = reVersionConst.ReplaceAllString(out2, "${1}"+version+"${2}")
	if out2 != out {
		t.Error("二次写回应幂等（release 重跑会因空 diff 提交失败）")
	}
}

// TestActionRejectsPlaceholderSHA v1 模式必须拒绝占位 SHA256。
func TestActionRejectsPlaceholderSHA(t *testing.T) {
	doc, raw := loadActionYAML(t)

	const zeroSHA = "0000000000000000000000000000000000000000000000000000000000000000"
	if !strings.Contains(raw, zeroSHA) {
		t.Skip("当前 action.yml 未使用全 0 占位值（可能已由 release 回填）")
	}

	// action.yml 的 Fetch binary 步骤应显式拒绝占位值
	var fetchStep string
	for _, s := range doc.Runs.Steps {
		if strings.Contains(s.Name, "Fetch") || strings.Contains(s.Name, "binary") {
			fetchStep = s.Run
			break
		}
	}
	if fetchStep == "" {
		t.Fatal("找不到 Fetch binary 步骤（v1 模式未实现？）")
	}
	if !strings.Contains(fetchStep, zeroSHA) {
		t.Error("Fetch binary 未识别全 0 占位 SHA —— 会「校验了个 0」形成假安全感")
	}
	if !strings.Contains(fetchStep, "exit 1") {
		t.Error("占位 SHA256 应直接 exit 1，不应继续下载校验")
	}
}

// TestActionNoSelfEvaluatingHeredoc 分隔符自求值（<<$(date...)）跨秒会失配。
func TestActionNoSelfEvaluatingHeredoc(t *testing.T) {
	_, raw := loadActionYAML(t)

	if m := regexp.MustCompile(`<<\S*\$\(`).FindAllString(raw, -1); len(m) > 0 {
		t.Errorf("仍有自求值 heredoc 分隔符（跨秒产生不同值 → Matching delimiter not found）: %v", m)
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
