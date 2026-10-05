package pages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acg-q/userscript-console/internal/registry"
)

// edgeOut 返回隔离的输出根（out/dist），archive 落在 out/archive，
// 避免与既有用例共用 OS 临时目录的 archive 互相污染。
func edgeOut(t *testing.T) (root, out string) {
	t.Helper()
	root = t.TempDir()
	out = filepath.Join(root, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, out
}

// writeEdgeArchive 在 out 同级的 archive/commands.json 写入内容或建同名目录。
func writeEdgeArchive(t *testing.T, out string, content string, asDir bool) {
	t.Helper()
	dir := filepath.Join(filepath.Dir(filepath.Clean(out)), "archive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "commands.json")
	if asDir {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func edgeOptions(out string, commandsPerPage int) Options {
	return Options{
		Out:             out,
		Now:             time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
		PagesBase:       "https://test.github.io/repo",
		CommandsPerPage: commandsPerPage,
	}
}

// edgeData 非空 IssueStats 抑制 W1，便于精确断言告警内容。
func edgeData() Data {
	return Data{IssueStats: map[string]int{}}
}

func TestBuildSortsByIDWhenUpdatedAtEqual(t *testing.T) {
	reg := &registry.Registry{
		Schema: registry.SchemaVersion,
		Scripts: []registry.Script{
			{
				ID: "zzz1", Type: registry.TypeSelf, Name: "Z", Version: "1.0.0",
				Enabled: true, Match: []string{"*://*/*"}, Grant: []string{"none"},
				CreatedAt: "2026-10-01T00:00:00Z", UpdatedAt: "2026-10-05T00:00:00Z",
			},
			{
				ID: "aaa1", Type: registry.TypeSelf, Name: "A", Version: "1.0.0",
				Enabled: true, Match: []string{"*://*/*"}, Grant: []string{"none"},
				CreatedAt: "2026-10-01T00:00:00Z", UpdatedAt: "2026-10-05T00:00:00Z",
			},
		},
	}
	_, out := edgeOut(t)
	got, err := Build(reg, edgeOptions(out, 0), edgeData())
	if err != nil {
		t.Fatal(err)
	}
	ia := strings.Index(got.ScriptsJSON, `"ID":"aaa1"`)
	iz := strings.Index(got.ScriptsJSON, `"ID":"zzz1"`)
	if ia < 0 || iz < 0 {
		t.Fatalf("scripts.json 缺少脚本条目: %s", got.ScriptsJSON)
	}
	if ia > iz {
		t.Errorf("UpdatedAt 相同时应按 ID 升序，aaa1 应在 zzz1 之前: %s", got.ScriptsJSON)
	}
}

func TestBuildWarnsWhenArchiveIsBadJSON(t *testing.T) {
	_, out := edgeOut(t)
	writeEdgeArchive(t, out, "not json", false)
	got, err := Build(buildTestRegistry(t), edgeOptions(out, 0), edgeData())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got.BuildWarnings, "\n")
	if !strings.Contains(joined, "W3") || !strings.Contains(joined, "解析归档失败") {
		t.Errorf("归档非 JSON 应产生 W3 解析告警: %v", got.BuildWarnings)
	}
}

func TestBuildWarnsWhenArchiveUnreadable(t *testing.T) {
	_, out := edgeOut(t)
	// commands.json 是目录 → ReadFile 报非 IsNotExist 错误
	writeEdgeArchive(t, out, "", true)
	got, err := Build(buildTestRegistry(t), edgeOptions(out, 0), edgeData())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got.BuildWarnings, "\n")
	if !strings.Contains(joined, "W3") || !strings.Contains(joined, "读取归档失败") {
		t.Errorf("归档不可读应产生 W3 读取告警: %v", got.BuildWarnings)
	}
}

func TestBuildWithEmptyCommandsArchive(t *testing.T) {
	_, out := edgeOut(t)
	writeEdgeArchive(t, out, `{"schema":1,"commands":[]}`, false)
	got, err := Build(buildTestRegistry(t), edgeOptions(out, 0), edgeData())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.BuildWarnings) != 0 {
		t.Errorf("空归档不应产生告警: %v", got.BuildWarnings)
	}
	if len(got.CommandPages) != 0 || got.CommandsIndex != "" {
		t.Errorf("空归档不应产生命令页: pages=%v index=%q", got.CommandPages, got.CommandsIndex)
	}
}

func TestBuildCommandPagesPagination(t *testing.T) {
	_, out := edgeOut(t)
	archive := `{"schema":1,"commands":[{"command":"add","author":"u","created_at":"2026-10-01T00:00:00Z","results":[` +
		`{"id":"c1","author":"u","body":"one","created_at":"2026-10-01T00:00:00Z"},` +
		`{"id":"c2","author":"u","body":"two","created_at":"2026-10-01T00:00:00Z"},` +
		`{"id":"c3","author":"u","body":"three","created_at":"2026-10-01T00:00:00Z"},` +
		`{"id":"c4","author":"u","body":"four","created_at":"2026-10-01T00:00:00Z"},` +
		`{"id":"c5","author":"u","body":"five","created_at":"2026-10-01T00:00:00Z"}]}]}`
	writeEdgeArchive(t, out, archive, false)
	got, err := Build(buildTestRegistry(t), edgeOptions(out, 2), edgeData())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.CommandPages) != 3 {
		t.Fatalf("5 条记录、每页 2 条应产出 3 页，实际 %d 页；warnings=%v", len(got.CommandPages), got.BuildWarnings)
	}
	// 第 2 页存在 → 模板中的 sub/add/seq 分页辅助函数被执行
	if _, ok := got.CommandPages[2]; !ok {
		t.Errorf("缺少第 2 页: %v", got.CommandPages)
	}
	if !strings.Contains(got.CommandsIndex, "add") {
		t.Errorf("命令索引应含 add: %q", got.CommandsIndex)
	}
}

func TestBuildDetailRendersIssueNumber(t *testing.T) {
	reg := &registry.Registry{
		Schema: registry.SchemaVersion,
		Scripts: []registry.Script{
			{
				ID: "issue01", Type: registry.TypeSelf, Name: "关联 Issue", Version: "1.0.0",
				Enabled: true, Match: []string{"*://*/*"}, Grant: []string{"none"},
				CreatedAt: "2026-10-01T00:00:00Z", UpdatedAt: "2026-10-05T00:00:00Z",
				Issue: &registry.IssueRef{Number: 42, NodeID: "I_42", URL: "https://github.com/test/issues/42"},
			},
		},
	}
	_, out := edgeOut(t)
	got, err := Build(reg, edgeOptions(out, 0), edgeData())
	if err != nil {
		t.Fatal(err)
	}
	html := got.DetailHTMLs["issue01"]
	if !strings.Contains(html, "Issue #42") {
		t.Errorf("详情页应回退渲染 IssueNumber=42:\n%s", html)
	}
}
