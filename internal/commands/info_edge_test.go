package commands

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acg-q/userscript-console/internal/registry"
)

// buildInfoTestEnv 构造含停用/删除/全字段三种状态脚本的测试环境。
func buildInfoTestEnv(t *testing.T) *Env {
	t.Helper()
	root := t.TempDir()
	reg := &registry.Registry{
		Schema: registry.SchemaVersion,
		Scripts: []registry.Script{
			{
				ID:        "off01",
				Type:      registry.TypeSelf,
				Name:      "alpha",
				Version:   "1.0.0",
				Enabled:   false,
				Match:     []string{"*://*/*"},
				Grant:     []string{"none"},
				CreatedAt: "2026-01-01T00:00:00Z",
				UpdatedAt: "2026-01-01T00:00:00Z",
			},
			{
				ID:        "gone01",
				Type:      registry.TypeSelf,
				Name:      "beta",
				Version:   "1.0.0",
				Enabled:   true,
				Deleted:   true,
				Match:     []string{"*://*/*"},
				Grant:     []string{"none"},
				CreatedAt: "2026-01-01T00:00:00Z",
				UpdatedAt: "2026-01-02T00:00:00Z",
			},
			{
				ID:            "full01",
				Type:          registry.TypeSynced,
				Name:          "gamma",
				Version:       "2.0.0",
				Enabled:       true,
				Match:         []string{"*://*/*"},
				Grant:         []string{"none"},
				CreatedAt:     "2026-01-01T00:00:00Z",
				UpdatedAt:     "2026-02-01T00:00:00Z",
				Documentation: "https://test.github.io/repo/docs/full01",
				SourceURL:     stringPtr("https://example.com/full.user.js"),
				SourceType:    stringPtr("direct"),
				LastSyncedAt:  stringPtr("2026-02-01T03:00:00Z"),
				SyncEnabled:   boolPtr(false),
				Issue:         &registry.IssueRef{Number: 7, NodeID: "I_7", URL: "https://github.com/test/issues/7"},
				Changelog: []registry.ChangelogEntry{
					{Version: "2.0.0", Date: "2026-02-01", Note: "第六条"},
					{Version: "1.5.0", Date: "2026-01-20", Note: "第五条"},
					{Version: "1.4.0", Date: "2026-01-15", Note: "第四条"},
					{Version: "1.3.0", Date: "2026-01-10", Note: "第三条"},
					{Version: "1.2.0", Date: "2026-01-05", Note: "第二条"},
					{Version: "1.1.0", Date: "2026-01-03", Note: "第一条"},
				},
			},
		},
	}
	if err := reg.Save(filepath.Join(root, "registry.json")); err != nil {
		t.Fatal(err)
	}
	return &Env{Root: root, Now: time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)}
}

func TestFindEntryEmptyKeyReturnsNil(t *testing.T) {
	r := &registry.Registry{
		Schema:  registry.SchemaVersion,
		Scripts: []registry.Script{{ID: "x01", Type: registry.TypeSelf, Name: "脚本"}},
	}
	if findEntry(r, "   ") != nil {
		t.Error("空白 key 应返回 nil，不匹配任何条目")
	}
}

func TestRunInfoRequiresKey(t *testing.T) {
	_, err := runInfo(&Env{Root: t.TempDir()}, "   ", nil)
	if err == nil || !strings.Contains(err.Error(), "用法") {
		t.Errorf("空参数应报用法错误, got %v", err)
	}
}

func TestRunInfoLoadRegError(t *testing.T) {
	_, err := runInfo(&Env{}, "off01", nil)
	if err == nil {
		t.Fatal("root 未配置时 runInfo 应返回 error")
	}
}

func TestRunInfoStatusBranches(t *testing.T) {
	env := buildInfoTestEnv(t)
	cases := []struct {
		key, want string
	}{
		{"off01", "停用"},
		{"gone01", "已删除（软删除）"},
		{"full01", "启用"},
	}
	for _, c := range cases {
		res, err := runInfo(env, c.key, nil)
		if err != nil {
			t.Fatalf("runInfo(%s) 报错: %v", c.key, err)
		}
		if !strings.Contains(res.Text, "| 状态 | "+c.want+" |") {
			t.Errorf("runInfo(%s) 状态应含 %q:\n%s", c.key, c.want, res.Text)
		}
	}
}

func TestRunInfoOptionalRows(t *testing.T) {
	env := buildInfoTestEnv(t)
	res, err := runInfo(env, "full01", nil)
	if err != nil {
		t.Fatal(err)
	}
	wants := []string{
		"| 最后同步 | 2026-02-01T03:00:00Z |",
		"| Issue 链接 | https://github.com/test/issues/7 |",
		"| 文档链接 | https://test.github.io/repo/docs/full01 |",
		"| 自动同步 | 已关闭 |",
		"changelog（最近 5 条）",
	}
	for _, w := range wants {
		if !strings.Contains(res.Text, w) {
			t.Errorf("runInfo 应含 %q:\n%s", w, res.Text)
		}
	}
}
