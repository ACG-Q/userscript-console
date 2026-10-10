package main

import (
	"os"
	"testing"
)

// TestMain 清空会触达真实 GitHub 的环境变量。
// 回帖（POST_REPLY 默认 true）与门禁删评默认开启：开发者本机或 CI 若 export 了
// GITHUB_TOKEN + GITHUB_REPOSITORY，跑测试可能对真实仓库发请求。
// 各测试需要这些值时用 t.Setenv 自行注入（stubGH 指向 httptest）。
func TestMain(m *testing.M) {
	for _, k := range []string{"GITHUB_TOKEN", "GITHUB_REPOSITORY", "POST_REPLY", "COMMENT_ID"} {
		_ = os.Unsetenv(k)
	}
	os.Exit(m.Run())
}
