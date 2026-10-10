package buildinfo

import (
	"regexp"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name     string
		injected string
		env      string
		file     string
		want     string
	}{
		{
			name:     "ldflags 胜出一切",
			injected: "9.9.9",
			env:      "1.2.3",
			file:     "1.1.7",
			want:     "9.9.9",
		},
		{
			name:     "ldflags 带 v 前缀被剥离",
			injected: "v9.9.9",
			env:      "main",
			file:     "1.1.7",
			want:     "9.9.9",
		},
		{
			name:     "env 合法带 v 前缀被剥离",
			injected: "",
			env:      "v1.2.3",
			file:     "1.1.7",
			want:     "1.2.3",
		},
		{
			name:     "env 分支名被忽略回落文件",
			injected: "",
			env:      "main",
			file:     "1.1.7",
			want:     "1.1.7",
		},
		{
			name:     "env 预发布版本被忽略回落文件",
			injected: "",
			env:      "v1.2.3-beta",
			file:     "1.1.7",
			want:     "1.1.7",
		},
		{
			name:     "env SHA 形态被忽略回落文件",
			injected: "",
			env:      "abc1234",
			file:     "1.1.7",
			want:     "1.1.7",
		},
		{
			name:     "文件带 v 前缀被剥离",
			injected: "",
			env:      "",
			file:     "v1.1.7",
			want:     "1.1.7",
		},
		{
			name:     "三链全空回落 dev",
			injected: "",
			env:      "",
			file:     "",
			want:     "dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolve(tt.injected, tt.env, tt.file)
			if got != tt.want {
				t.Errorf("resolve(%q, %q, %q) = %q, want %q",
					tt.injected, tt.env, tt.file, got, tt.want)
			}
		})
	}
}

func TestFileVersionEmbedded(t *testing.T) {
	v := strings.TrimSpace(fileVersion)
	if v == "" {
		t.Fatal("embed 的 VERSION 内容为空")
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(v) {
		t.Errorf("VERSION 内容 %q 不是 X.Y.Z 形态", v)
	}
}
