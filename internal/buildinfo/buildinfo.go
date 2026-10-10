// Package buildinfo 解析 usm 的版本元数据（设计 §1）。
package buildinfo

import (
	_ "embed"
	"os"
	"regexp"
	"strings"
)

//go:embed VERSION
var fileVersion string

// injected 由 release 编译注入：
// -ldflags "-X github.com/acg-q/userscript-console/internal/buildinfo.injected=X.Y.Z"
var injected string

// semverRe 只认 vX.Y.Z / X.Y.Z；源码模式下 USM_VERSION 是 action ref
// （可能是分支名/SHA），非语义化版本一律忽略，回落 VERSION 文件。
var semverRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// Version 解析链（设计 §1.2 D3）：ldflags > USM_VERSION > VERSION 文件 > "dev"。
func Version() string {
	return resolve(injected, os.Getenv("USM_VERSION"), fileVersion)
}

func resolve(injected, env, file string) string {
	if v := strings.TrimSpace(injected); v != "" {
		return strings.TrimPrefix(v, "v")
	}
	if v := strings.TrimSpace(env); v != "" && semverRe.MatchString(v) {
		return strings.TrimPrefix(v, "v")
	}
	if v := strings.TrimSpace(file); v != "" {
		return strings.TrimPrefix(v, "v")
	}
	return "dev"
}
