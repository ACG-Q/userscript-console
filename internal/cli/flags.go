// Package cli 承载 usm 子命令的参数编排（SPEC-CLI §0.4：flag > env > 默认）。
package cli

import (
	"os"
	"strconv"
	"strings"
)

// EnvOr 读环境变量，空/缺失回默认。
func EnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envBool(key string) bool {
	return strings.EqualFold(os.Getenv(key), "true")
}

// runCommandFlags run-command 的输入（SPEC-CLI §1：flag > 同名 env > 默认）。
type RunCommandFlags struct {
	CommentBody     string
	CommentUser     string
	RepoOwner       string
	IssueNumber     string
	PagesBase       string
	AuthorName      string
	AuthorNamespace string
	ResultFile      string
	JSON            bool
}

// doctorFlags doctor 的输入。
type doctorFlags struct {
	Root  string
	JSON  bool
	Check bool
}

// cleanupFlags cleanup 的输入。
type cleanupFlags struct {
	Root  string
	Keep  int
	Apply bool
	JSON  bool
}

// schemaFlags 各子命令共用的 schema 校验入参。
type schemaFlags struct {
	Version int
}
