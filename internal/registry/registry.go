// Package registry 实现 registry.json（内容仓唯一真源）的读写、校验与原子落盘。
//
// 契约（SPEC-DATA §1.1、SPEC-ARCH-TEST I-2）：
//   - schema / 字段集合 / 数据所有权是硬契约；
//   - 序列化格式一经定型必须稳定：Load→Save 空改动字节不变（幂等）；
//   - 原子写：同目录临时文件 + rename；
//   - JSON 形态：2 空格缩进、不转义 HTML/非 ASCII、**无结尾换行**。
//
// DR-6：格式允许一次性规范化（本实现即为定型后的规范形态）：
//   - 键序 = struct 字段声明顺序（不再要求与 Python 插入序一致）；
//   - null 语义的可选字段缺省即省略（如 synced 未同步过 → 无 last_synced_at 键，
//     读入显式 null 同样归一为缺省），读入即补 discussions/changelog/match/grant 空数组。
package registry

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SchemaVersion 当前 schema 版本。
const SchemaVersion = 1

// RegError registry 数据错误。Msg 中文可直接呈现给维护者。
type RegError struct{ Msg string }

func (e *RegError) Error() string { return e.Msg }

func errf(format string, args ...any) error {
	return &RegError{Msg: fmt.Sprintf(format, args...)}
}

const restoreHint = "（请用 Git 历史恢复该文件，不要直接删除或手改结构）"

// ── 数据结构（字段声明顺序即 JSON 键顺序） ──────────────────────

type Registry struct {
	Schema  int      `json:"schema"`
	Scripts []Script `json:"scripts"`
}

type ChangelogEntry struct {
	Version string `json:"version"`
	Date    string `json:"date"`
	Note    string `json:"note"`
}

type DiscussionEntry struct {
	Version   string `json:"version"`
	Number    int    `json:"number"`
	NodeID    string `json:"node_id"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

type IssueRef struct {
	Number int    `json:"number"`
	NodeID string `json:"node_id"`
	URL    string `json:"url"`
}

type Script struct {
	// ── 通用字段 ──
	ID            string            `json:"id"`
	Type          string            `json:"type"` // self | synced
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Description   string            `json:"description"`
	Author        string            `json:"author"`
	Namespace     string            `json:"namespace"`
	Match         []string          `json:"match"`
	Grant         []string          `json:"grant"`
	Enabled       bool              `json:"enabled"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at"`
	Documentation string            `json:"documentation"`
	Changelog     []ChangelogEntry  `json:"changelog"`
	Discussions   []DiscussionEntry `json:"discussions"`
	Deleted       bool              `json:"deleted"`
	Issue         *IssueRef         `json:"issue,omitempty"`

	// ── synced 专有（self 脚本整组省略） ──
	SourceURL    *string `json:"source_url,omitempty"`
	SourceType   *string `json:"source_type,omitempty"`
	LastSyncedAt *string `json:"last_synced_at,omitempty"` // 缺省 = 从未同步（等价 null）
	SyncEnabled  *bool   `json:"sync_enabled,omitempty"`
	CustomMatch  *string `json:"custom_match,omitempty"` // 历史字段，保留
}

const (
	TypeSelf   = "self"
	TypeSynced = "synced"
)

// ── 加载与校验 ─────────────────────────────────────────────

// Load 读取并校验 registry 文件。
func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse 解析 registry 字节流：解析 → 补默认值 → 结构校验。
func Parse(data []byte) (*Registry, error) {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return nil, errf("registry 内容为空%s", restoreHint)
	}
	if trimmed[0] != '{' {
		return nil, errf("registry 顶层必须是 JSON 对象，实际以 %q 开头%s", trimmed[0], restoreHint)
	}
	var r Registry
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields() // 关键：字段名拼写漂移必须响亮失败（Query.discussion 事故同类）；schema 演进时工具与数据同 PR 更新
	if err := dec.Decode(&r); err != nil {
		return nil, errf("registry 解析失败: %v%s", err, restoreHint)
	}
	if dec.More() {
		return nil, errf("registry 顶层对象之后存在多余内容%s", restoreHint)
	}
	r.applyDefaults()
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

func (r *Registry) applyDefaults() {
	if r.Scripts == nil {
		r.Scripts = []Script{}
	}
	for i := range r.Scripts {
		s := &r.Scripts[i]
		if s.Discussions == nil {
			s.Discussions = []DiscussionEntry{}
		}
		if s.Changelog == nil {
			s.Changelog = []ChangelogEntry{}
		}
		if s.Match == nil {
			s.Match = []string{}
		}
		if s.Grant == nil {
			s.Grant = []string{}
		}
	}
}

// Validate 结构校验（加载即校验语义；也可对内存对象复用）。
func (r *Registry) Validate() error {
	if r.Schema < 1 {
		return errf("schema 缺失或非法（实际 %d），当前支持 schema=%d%s", r.Schema, SchemaVersion, restoreHint)
	}
	if r.Scripts == nil {
		return errf("scripts 必须是数组（当前为 null）%s", restoreHint)
	}
	seen := make(map[string]int, len(r.Scripts))
	for i, s := range r.Scripts {
		if s.ID == "" {
			return errf("scripts[%d] 缺少 id%s", i, restoreHint)
		}
		if s.Type != TypeSelf && s.Type != TypeSynced {
			return errf("scripts[%d](id=%s) type 非法：%q（只允许 self|synced）%s", i, s.ID, s.Type, restoreHint)
		}
		if prev, dup := seen[s.ID]; dup {
			return errf("scripts[%d] id=%s 与 scripts[%d] 重复%s", i, s.ID, prev, restoreHint)
		}
		seen[s.ID] = i
	}
	return nil
}

// ── 序列化与原子写 ──────────────────────────────────────────

// Bytes 输出规范形态 JSON：2 空格缩进、不转义 HTML、无结尾换行。空改动输出稳定（I-2）。
func (r *Registry) Bytes() ([]byte, error) {
	r.applyDefaults() // 手工构造的对象同样归一（nil → []）
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false) // 关键：否则 <>& 变 <>< 造成全量 diff（I-2）
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	out := bytes.TrimSuffix(buf.Bytes(), []byte("\n")) // 现有契约：末字节为 '}'
	return out, nil
}

// Save 原子落盘：同目录唯一临时文件 + rename；文件末尾无换行。
func (r *Registry) Save(path string) error {
	data, err := r.Bytes()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后该调用无效果
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// ── 查询与变更 ─────────────────────────────────────────────

func (r *Registry) FindByID(id string) *Script {
	for i := range r.Scripts {
		if r.Scripts[i].ID == id {
			return &r.Scripts[i]
		}
	}
	return nil
}

func (r *Registry) FindBySourceURL(url string) *Script {
	for i := range r.Scripts {
		s := &r.Scripts[i]
		if s.SourceURL != nil && *s.SourceURL == url {
			return s
		}
	}
	return nil
}

// Add 追加条目（调用方负责去重/复活语义，见 commands 层 I-4）。
func (r *Registry) Add(s Script) {
	r.Scripts = append(r.Scripts, s)
}

// RemoveID 软删除语义由调用方置 Deleted；本方法仅物理移除（doctor 修复用）。
func (r *Registry) RemoveID(id string) bool {
	for i := range r.Scripts {
		if r.Scripts[i].ID == id {
			r.Scripts = append(r.Scripts[:i], r.Scripts[i+1:]...)
			return true
		}
	}
	return false
}

// ── ID 规则（SPEC-DATA §1.1） ──────────────────────────────

// SourceID synced 脚本 ID = 来源 URL MD5 前 12 位。
func SourceID(sourceURL string) string {
	sum := md5.Sum([]byte(sourceURL))
	return hex.EncodeToString(sum[:])[:12]
}

// ErrNotFound 查询未命中。
var ErrNotFound = errors.New("registry: entry not found")
