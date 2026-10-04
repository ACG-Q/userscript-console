package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validJSON = `{
  "schema": 1,
  "scripts": [
    {
      "id": "3f45ee3c-0000-4000-8000-000000000001",
      "type": "self",
      "name": "示例脚本",
      "version": "1.0.1",
      "description": "含 <b>HTML</b> 与 中文 的描述",
      "author": "ACG-Q",
      "namespace": "https://your-namespace.com",
      "match": ["*://*/*"],
      "grant": ["none"],
      "enabled": true,
      "created_at": "2026-10-02T00:00:00Z",
      "updated_at": "2026-10-02T00:00:00Z",
      "documentation": "# 文档\nalert('<x>') 与 & 符号",
      "changelog": [
        { "version": "1.0.1", "date": "2026-10-02", "note": "手动更新" }
      ],
      "discussions": [],
      "deleted": false,
      "issue": { "number": 5, "node_id": "I_x", "url": "https://github.com/o/r/issues/5" }
    },
    {
      "id": "abc123def456",
      "type": "synced",
      "name": "同步脚本",
      "version": "2.0.0",
      "description": "",
      "author": "upstream",
      "namespace": "",
      "match": [],
      "grant": [],
      "enabled": true,
      "created_at": "2026-10-02T00:00:00Z",
      "updated_at": "2026-10-02T00:00:00Z",
      "documentation": "",
      "changelog": [],
      "discussions": [
        { "version": "2.0.0", "number": 10, "node_id": "D_x", "url": "https://github.com/o/r/discussions/10", "created_at": "2026-10-02" }
      ],
      "deleted": false,
      "source_url": "https://greasyfork.org/scripts/1234",
      "source_type": "greasyfork",
      "last_synced_at": null,
      "sync_enabled": true,
      "custom_match": null
    }
  ]
}`

func TestParseDefaultsAndFields(t *testing.T) {
	r, err := Parse([]byte(validJSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r.Schema != 1 {
		t.Errorf("schema = %d", r.Schema)
	}
	if len(r.Scripts) != 2 {
		t.Fatalf("scripts = %d", len(r.Scripts))
	}
	self := r.Scripts[0]
	if self.Discussions == nil || self.Changelog == nil || self.Match == nil || self.Grant == nil {
		t.Errorf("空数组默认值未补（setdefault 语义）: %+v", self)
	}
	if self.Issue == nil || self.Issue.Number != 5 {
		t.Errorf("issue 解析错误: %+v", self.Issue)
	}
	synced := r.Scripts[1]
	if synced.LastSyncedAt != nil {
		t.Errorf("null → nil 归一失败: %v", *synced.LastSyncedAt)
	}
	if synced.SourceURL == nil || *synced.SourceURL != "https://greasyfork.org/scripts/1234" {
		t.Errorf("source_url: %+v", synced.SourceURL)
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantSub string
	}{
		{"空内容", "   ", "为空"},
		{"顶层非对象", `[]`, "顶层必须是 JSON 对象"},
		{"scripts 非数组", `{"schema":1,"scripts":{"a":1}}`, "解析失败"},
		{"缺 id", `{"schema":1,"scripts":[{"type":"self"}]}`, "缺少 id"},
		{"type 非法", `{"schema":1,"scripts":[{"id":"x","type":"evil"}]}`, "type 非法"},
		{"id 重复", `{"schema":1,"scripts":[{"id":"x","type":"self"},{"id":"x","type":"self"}]}`, "重复"},
		{"schema 缺失", `{"schema":0,"scripts":[]}`, "schema 缺失或非法"},
		{"未知字段（拼写漂移必须响亮失败）", `{"schema":1,"scripts":[],"schem":1}`, "解析失败"},
		{"尾部垃圾", `{"schema":1,"scripts":[]} extra`, "多余内容"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.in))
			if err == nil {
				t.Fatal("期望报错，实际通过")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("错误 %q 不含 %q", err.Error(), tt.wantSub)
			}
			if !strings.Contains(err.Error(), "Git 历史恢复") {
				t.Errorf("缺少恢复提示: %q", err.Error())
			}
		})
	}
}

func TestBytesCanonical(t *testing.T) {
	r, err := Parse([]byte(validJSON))
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 || out[len(out)-1] != '}' {
		t.Errorf("必须无结尾换行，末字节 = %q", out[len(out)-1])
	}
	s := string(out)
	if !strings.Contains(s, "\n  \"schema\": 1") {
		t.Error("必须 2 空格缩进")
	}
	if strings.Contains(s, `\u003c`) || strings.Contains(s, `\u003e`) || strings.Contains(s, `\u0026`) {
		t.Error("SetEscapeHTML(false) 未生效：< > & 被转成了 \\uXXXX")
	}
	if !strings.Contains(s, `<b>HTML</b>`) {
		t.Error("HTML 应原样输出（不转义）")
	}
	if strings.Contains(s, `\u`) {
		t.Error("非 ASCII 被转义（ensure_ascii 等价要求）")
	}
	// 键序 = 字段声明顺序（spec 顺序）
	idxID := strings.Index(s, `"id"`)
	idxType := strings.Index(s, `"type"`)
	idxName := strings.Index(s, `"name"`)
	if !(idxID < idxType && idxType < idxName) {
		t.Errorf("键序不符: id=%d type=%d name=%d", idxID, idxType, idxName)
	}
}

func TestRoundTripIdempotent(t *testing.T) {
	r, err := Parse([]byte(validJSON))
	if err != nil {
		t.Fatal(err)
	}
	first, _ := r.Bytes()
	r2, err := Parse(first)
	if err != nil {
		t.Fatalf("规范输出不可再解析: %v", err)
	}
	second, _ := r2.Bytes()
	if string(first) != string(second) {
		t.Errorf("Bytes 不幂等:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func TestNullNormalizeOnRoundTrip(t *testing.T) {
	// 显式 null（旧数据）→ 读入 nil → 写出省略；第二次起稳定（一次性规范化的定义）。
	in := `{"schema":1,"scripts":[{"id":"a","type":"synced","name":"","version":"","description":"","author":"","namespace":"","match":[],"grant":[],"enabled":true,"created_at":"","updated_at":"","documentation":"","changelog":[],"discussions":[],"deleted":false,"last_synced_at":null}]}`
	r, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	first, _ := r.Bytes()
	if strings.Contains(string(first), "last_synced_at") {
		t.Error("null 应归一为缺省键")
	}
	r2, _ := Parse(first)
	second, _ := r2.Bytes()
	if string(first) != string(second) {
		t.Error("规范化后必须稳定")
	}
}

func TestSaveLoadByteEqual(t *testing.T) {
	r, err := Parse([]byte(validJSON))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	if err := r.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := r.Bytes()
	if string(data) != string(want) {
		t.Error("落盘内容与 Bytes 不一致")
	}
	r2, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	again, _ := r2.Bytes()
	if string(again) != string(want) {
		t.Error("Load→Save 非字节等价（I-2 破坏）")
	}
	// 再写一次：原子写不产生 diff
	if err := r2.Save(path); err != nil {
		t.Fatal(err)
	}
	data2, _ := os.ReadFile(path)
	if string(data2) != string(want) {
		t.Error("重复保存产生 diff")
	}
	// 无残留临时文件
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "registry.json" {
			t.Errorf("临时文件残留: %s", e.Name())
		}
	}
}

func TestQueries(t *testing.T) {
	r, _ := Parse([]byte(validJSON))
	if r.FindByID("abc123def456") == nil {
		t.Error("FindByID 失败")
	}
	if r.FindByID("nope") != nil {
		t.Error("FindByID 误命中")
	}
	if r.FindBySourceURL("https://greasyfork.org/scripts/1234") == nil {
		t.Error("FindBySourceURL 失败")
	}
	r.RemoveID("3f45ee3c-0000-4000-8000-000000000001")
	if len(r.Scripts) != 1 {
		t.Error("RemoveID 失败")
	}
}

func TestSourceID(t *testing.T) {
	// md5("https://greasyfork.org/scripts/1234") 前 12 位，写死防漂移。
	got := SourceID("https://greasyfork.org/scripts/1234")
	if len(got) != 12 {
		t.Fatalf("长度 = %d", len(got))
	}
	if SourceID("https://a") == SourceID("https://b") {
		t.Error("不同 URL 碰撞")
	}
}

func TestManualRegistryNormalized(t *testing.T) {
	// 手工构造（nil 切片）也能输出合法规范 JSON。
	r := &Registry{Schema: 1}
	out, err := r.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "{\n  \"schema\": 1,\n  \"scripts\": []\n}" {
		t.Errorf("手工对象归一失败: %s", out)
	}
}

func TestAdd(t *testing.T) {
	r := &Registry{Schema: 1}
	r.Add(Script{ID: "a1", Name: "脚本A"})
	if len(r.Scripts) != 1 || r.Scripts[0].Name != "脚本A" {
		t.Errorf("Add 失败: %+v", r.Scripts)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("Load 不存在文件应返回 error")
	}
}
