package main

// 本文件是本次真实 CI 故障的回归锁。
//
// 现象（内容仓 deploy-pages.yml，build 步骤）：
//
//	jq: parse error: Invalid numeric literal at line 1, column 4
//	Error: Process completed with exit code 5.
//	Error: Unable to process file command 'output' successfully.
//
// 根因：action.yml 对**所有**子命令都追加 --json 并用 jq 解析 stdout，
// 但 buildRun / projectRun / cleanupRun 只 fmt.Println(res.Text) —— 输出的是
// 人类可读文本（"✅ 构建完成：…"），不是 JSON。
//
// jq 解析失败 → .changed/.result 取到空 → heredoc 写入不完整 →
// GitHub 报 "Matching delimiter not found" + exit 5。
//
// 前两次修复（共享 EOF_ID、printf 替代 echo）都只治了症状，
// 真正的根因是**子命令没实现 --json**。本文件锁死这个契约。

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout 捕获 os.Stdout（writeJSON 直接写 *os.File）。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	os.Stdout = orig
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

// assertActionJSON 断言输出可被 jq（等价：encoding/json）解析，
// 且含 action.yml 需要的三个字段。
func assertActionJSON(t *testing.T, out, label string) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("%s: 输出不是合法 JSON（jq 会 parse error）: %v\n输出: %q", label, err, out)
	}
	for _, key := range []string{"authorized", "changed", "result", "version"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("%s: JSON 缺字段 %q（action.yml 的 jq 会取到 null）", label, key)
		}
	}
	if v, ok := payload["version"].(string); !ok || v == "" {
		t.Errorf("%s: version 必须是非空字符串，实际 %T (%v)", label, payload["version"], payload["version"])
	}
	if _, ok := payload["changed"].(bool); !ok {
		t.Errorf("%s: changed 必须是 bool，实际 %T", label, payload["changed"])
	}
}

// TestBuildJSONOutput build 必须在 --json 下输出 JSON（本次故障的根因）。
func TestBuildJSONOutput(t *testing.T) {
	root := buildTestRegistryDir(t)
	t.Setenv("PAGES_BASE", "https://test.github.io/repo")

	out := captureStdout(t, func() {
		if code := buildRun([]string{"--root", root, "--json"}); code != 0 {
			t.Errorf("buildRun --json 应返回 0, got %d", code)
		}
	})
	assertActionJSON(t, out, "build --json")
}

// TestProjectJSONOutput project 同上。
func TestProjectJSONOutput(t *testing.T) {
	root := buildTestRegistryDir(t)

	out := captureStdout(t, func() {
		// 无 GH_REPO_OWNER → 软失败（rc 0），但仍须输出合法 JSON
		t.Setenv("GH_REPO_OWNER", "")
		t.Setenv("GH_CLIENT", "")
		if code := projectRun([]string{"--root", root, "--json"}); code != 0 {
			t.Errorf("projectRun --json 应返回 0, got %d", code)
		}
	})
	assertActionJSON(t, out, "project --json")
}

// TestCleanupJSONOutput cleanup 同上。
func TestCleanupJSONOutput(t *testing.T) {
	root := buildTestRegistryDir(t)

	out := captureStdout(t, func() {
		if code := cleanupRun([]string{"--root", root, "--json"}); code != 0 {
			t.Errorf("cleanupRun --json 应返回 0, got %d", code)
		}
	})
	assertActionJSON(t, out, "cleanup --json")
}

// TestDoctorJSONOutput doctor 的 JSON 结构由 usm 组装（不是 cli.RunDoctor
// 的 {problems, ok}），否则 action.yml 的 jq 取 .changed 会拿到 null。
func TestDoctorJSONOutput(t *testing.T) {
	root := buildTestRegistryDir(t)

	out := captureStdout(t, func() {
		if code := doctorRun([]string{"--check", "--root", root, "--json"}); code != 0 {
			t.Errorf("doctorRun --json 健康仓应返回 0, got %d", code)
		}
	})
	assertActionJSON(t, out, "doctor --json")
	if strings.Contains(out, "数据一致性检查通过") {
		t.Error("doctor --json 不应混入人类可读文本（会破坏 JSON 解析）")
	}
}

// TestDoctorJSONOutputOnFailure 失败时也必须输出合法 JSON，
// 且 result 里带问题列表（便于失败回帖）。
func TestDoctorJSONOutputOnFailure(t *testing.T) {
	empty := t.TempDir() // 无 registry.json → doctor 报问题

	out := captureStdout(t, func() {
		code := doctorRun([]string{"--check", "--root", empty, "--json"})
		if code != 1 {
			t.Errorf("doctorRun --check 空仓应返回 1, got %d", code)
		}
	})
	assertActionJSON(t, out, "doctor --json (fail)")

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	result, _ := payload["result"].(string)
	if !strings.Contains(result, "registry.json") {
		t.Errorf("失败 result 应说明问题，实际: %q", result)
	}
}

// TestNonJSONModeUnchanged 不带 --json 时行为不变（人类可读文本）。
func TestNonJSONModeUnchanged(t *testing.T) {
	root := buildTestRegistryDir(t)
	t.Setenv("PAGES_BASE", "https://test.github.io/repo")

	out := captureStdout(t, func() {
		buildRun([]string{"--root", root})
	})
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("无 --json 时应输出人类可读文本，实际是 JSON: %q", out)
	}
	if !strings.Contains(out, "构建完成") {
		t.Errorf("无 --json 时应输出构建摘要，实际: %q", out)
	}
}

// TestHasJSONFlag 参数识别。
func TestHasJSONFlag(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"--root", "x"}, false},
		{[]string{"--json"}, true},
		{[]string{"--root", "x", "--json"}, true},
		{[]string{"--json=true"}, false}, // 只认独立的 --json
	}
	for _, c := range cases {
		if got := hasJSONFlag(c.args); got != c.want {
			t.Errorf("hasJSONFlag(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}
