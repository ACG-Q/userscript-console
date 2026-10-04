package main

// 本文件锁定 action.yml 的 release 契约：
//   1. inputs/outputs 对调用方的既有接口不可变（SPEC-ACTION §5）
//   2. v1 二进制常量必须走 inputs.default 承载，**不能用顶层 env**
//      （GitHub Action 元数据不认顶层 env:，会静默忽略 → 展开为空串）
//   3. 写回正则必须命中且幂等（release 重跑不产生空 diff）
//   4. 占位 SHA256（全 0）必须被拒绝（防止「校验了个 0」的假安全感）
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
		"keep", "apply", "version", "use-binary",
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

// TestActionNoTopLevelEnv 是本次真实 CI 故障的回归锁。
//
// 现象：v1 二进制模式下 curl 报 exit 22（HTTP 404），step 秒级失败。
// 根因：action.yml 把 USM_BINARY_VERSION / USM_BINARY_SHA256 放在**顶层
// `env:`**，而 GitHub Action 元数据只认 name/description/branding/inputs/
// outputs/runs —— 顶层 env: 被静默忽略，`${{ env.X }}` 展开为空串，
// 下载 URL 变成 `.../download/v/usm-linux-amd64`（少了版本号）→ 404。
//
// 修复：常量改走 `inputs.<name>.default`。本测试确保它不会退回顶层 env。
func TestActionNoTopLevelEnv(t *testing.T) {
	_, raw := loadActionYAML(t)

	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["env"]; ok {
		t.Error("action.yml 顶层不应有 env: —— GitHub Action 元数据不认它，")
		t.Error("      会静默忽略导致 ${{ env.X }} 展开为空。常量请走 inputs.<name>.default。")
	}
}

// TestActionBinaryConstantsAreInputs release CI 写回的目标必须存在。
func TestActionBinaryConstantsAreInputs(t *testing.T) {
	doc, _ := loadActionYAML(t)

	for _, name := range []string{"binary-version", "binary-sha256"} {
		in, ok := doc.Inputs[name]
		if !ok {
			t.Errorf("缺少内部常量 input %q（release CI 需写回其 default）", name)
			continue
		}
		if _, ok := in["default"]; !ok {
			t.Errorf("input %q 缺 default（GitHub 展开 ${{ inputs.%s }} 依赖它）", name, name)
		}
	}
}

// 写回正则需容忍中间的 description 行（release.yml 用同一套模式）。
// 实际形如：
//
//	binary-sha256:
//	  description: '...'
//	  default: 'xxx'
var (
	reSHADefault     = regexp.MustCompile(`(?s)(binary-sha256:.*?default:\s*')[^']*(')`)
	reVersionDefault = regexp.MustCompile(`(?s)(binary-version:.*?default:\s*')[^']*(')`)
)

// TestActionWriteBackRegexMatches 复刻 release.yml publish job 的写回逻辑。
func TestActionWriteBackRegexMatches(t *testing.T) {
	_, raw := loadActionYAML(t)

	const (
		sha     = "aaaaaaaabbbbbbbbccccccccddddddddeeeeeeeeffffffff0000000011111111"
		version = "1.2.3"
	)

	out := reSHADefault.ReplaceAllString(raw, "${1}"+sha+"${2}")
	out = reVersionDefault.ReplaceAllString(out, "${1}"+version+"${2}")

	if out == raw {
		t.Fatal("写回正则未命中 —— release.yml 会报「未找到 binary-sha256 / binary-version 的 default」")
	}

	var doc actionYAML
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("写回后 YAML 应仍合法: %v", err)
	}
	if got := doc.Inputs["binary-sha256"]["default"]; got != sha {
		t.Errorf("写回后 binary-sha256.default = %v, want %s", got, sha)
	}
	if got := doc.Inputs["binary-version"]["default"]; got != version {
		t.Errorf("写回后 binary-version.default = %v, want %s", got, version)
	}

	// 幂等：同值二次写回应无变化（release 重跑不产生空 diff 提交）
	out2 := reSHADefault.ReplaceAllString(out, "${1}"+sha+"${2}")
	out2 = reVersionDefault.ReplaceAllString(out2, "${1}"+version+"${2}")
	if out2 != out {
		t.Error("二次写回应幂等（release 重跑会因空 diff 提交失败）")
	}
}

// TestActionRejectsPlaceholderSHA v1 模式必须拒绝占位 SHA256。
func TestActionRejectsPlaceholderSHA(t *testing.T) {
	doc, _ := loadActionYAML(t)

	const zeroSHA = "0000000000000000000000000000000000000000000000000000000000000000"
	current, _ := doc.Inputs["binary-sha256"]["default"].(string)
	if current != zeroSHA {
		t.Skip("binary-sha256 已被 release 回填，跳过占位值检查")
	}

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

// TestActionFetchBinaryRejectsEmptyVersion 版本号为空必须显式报错，
// 而不是发出 `.../download/v/usm-linux-amd64` 这种畸形 URL 让 curl 报 404。
func TestActionFetchBinaryRejectsEmptyVersion(t *testing.T) {
	doc, _ := loadActionYAML(t)

	var fetchRun string
	for _, s := range doc.Runs.Steps {
		if strings.Contains(s.Name, "Fetch") || strings.Contains(s.Name, "binary") {
			fetchRun = s.Run
			break
		}
	}
	if fetchRun == "" {
		t.Fatal("找不到 Fetch binary 步骤")
	}
	if !strings.Contains(fetchRun, `if [ -z "$WANT_VERSION" ]`) {
		t.Error("Fetch binary 应显式检查 WANT_VERSION 为空并报错")
	}
	if !strings.Contains(fetchRun, "exit 1") {
		t.Error("WANT_VERSION 为空时应 exit 1")
	}
	// env 块里必须从 inputs.binary-version 取值（不是顶层 env）
	found := false
	for _, s := range doc.Runs.Steps {
		if s.Env["WANT_VERSION"] != "" && strings.Contains(s.Env["WANT_VERSION"], "inputs.binary-version") {
			found = true
		}
		if s.Env["WANT_SHA256"] != "" && strings.Contains(s.Env["WANT_SHA256"], "inputs.binary-sha256") {
			found = found || true
		}
	}
	if !found {
		t.Error("Fetch binary 的 env 应从 inputs.binary-version / inputs.binary-sha256 取值")
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
