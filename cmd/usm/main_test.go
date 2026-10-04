package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureOutput 捕获 run 对 stdout/stderr 的写入。
func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	or, ow, _ := os.Pipe()
	er, ew, _ := os.Pipe()
	os.Stdout, os.Stderr = ow, ew
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	done1, done2 := make(chan string), make(chan string)
	go func() { b, _ := io.ReadAll(or); done1 <- string(b) }()
	go func() { b, _ := io.ReadAll(er); done2 <- string(b) }()
	fn()
	ow.Close()
	ew.Close()
	return <-done1, <-done2
}

func TestRunUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"无参数", nil, 2},
		{"未知子命令", []string{"bogus"}, 2},
		{"version", []string{"version"}, 0},
		{"help", []string{"help"}, 0},
		{"未接线子命令", []string{"build"}, 1},
		{"snapshot 缺参数", []string{"snapshot"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.args); got != tt.want {
				t.Errorf("run(%v) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}

func TestVersionOutput(t *testing.T) {
	out, _ := captureOutput(t, func() { run([]string{"version"}) })
	if !strings.Contains(out, "usm "+version) {
		t.Errorf("版本输出: %q", out)
	}
	if !strings.Contains(out, "registry-schema-version 1") {
		t.Errorf("schema 输出: %q", out)
	}
}

func TestUnknownCommandMessage(t *testing.T) {
	_, errBuf := captureOutput(t, func() { run([]string{"bogus"}) })
	if !strings.Contains(errBuf, "未知子命令") {
		t.Errorf("错误信息: %q", errBuf)
	}
}
