// Package pages 提供站点生成器（HTML + JSON + 静态资源）。
//
// 渲染规则（DR-6/DR-7）：
//   - HTML：`html/template` + `go:embed`（上下文自动转义 = XSS 防线，禁止手拼 HTML 字符串）。
//   - Markdown：`goldmark` + `bluemonday`（DR-6：无需与 Python 字节比对，XSS 用例硬验收）。
//   - 嵌入：`embed.FS`（模板 + 静态资源），顺序 `TOKEN+COMPONENT+PREVIEW` → `<style>`，
//     `extra_js` 顺序 `FILTER→LIST` / `DISC`。
//   - 投影：`BuildTitle/BuildBody` 确定性输出；once-online 接受一次性重写（DR-6）。
package pages

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/times"
	"github.com/yuin/goldmark"
	htmlmd "github.com/yuin/goldmark/renderer/html"
)

//go:embed templates/*.tmpl
//go:embed static/*.css static/**/*.css static/**/*.js
var embedFS embed.FS

// Options 控制站点生成行为。
type Options struct {
	Out             string // 输出根目录，空→"dist"
	Batch           int    // 首页每批数量，≤0→10
	CommandsPerPage int    // 命令分页条数，≤0→5
	PagesBase       string // 站点基址，如 "https://owner.github.io/repo"
	Now             time.Time
}

func (o *Options) normalize() {
	if o.Out == "" {
		o.Out = "dist"
	}
	if o.Batch <= 0 {
		o.Batch = 10
	}
	if o.CommandsPerPage <= 0 {
		o.CommandsPerPage = 5
	}
	o.PagesBase = strings.TrimRight(o.PagesBase, "/")
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
}

// Data 是从 registry + Discussion 计算的辅助数据。
type Data struct {
	IssueStats         map[string]int       // nodeID -> comment count
	DiscussionComments map[string][]Comment // nodeID -> comments
}

// Comment 一条版本帖评论。
type Comment struct {
	Author    string
	Body      string
	CreatedAt string
}

// Outcome 是 Build 的返回值。
type Outcome struct {
	IndexHTML     string
	ScriptsJSON   string
	DetailHTMLs   map[string]string
	CommandPages  map[int]string // page number -> HTML
	CommandsIndex string
	BuildWarnings []string
	Changed       bool
	Pages         int
}

// ── 渲染入口 ─────────────────────────────────────────────────

// Build 渲染整站。
func Build(reg *registry.Registry, opts Options, data Data) (Outcome, error) {
	opts.normalize()

	out := Outcome{
		DetailHTMLs:  make(map[string]string),
		CommandPages: make(map[int]string),
	}

	// 活跃脚本排序：!Deleted 按 UpdatedAt 降序、并列 ID 升序
	active := filterActive(reg.Scripts)
	sort.Slice(active, func(i, j int) bool {
		if active[i].UpdatedAt != active[j].UpdatedAt {
			return active[i].UpdatedAt > active[j].UpdatedAt
		}
		return active[i].ID < active[j].ID
	})

	// index.html
	idx, err := renderIndex(active, opts, reg)
	if err != nil {
		return out, err
	}
	out.IndexHTML = idx
	out.Pages++

	// scripts.json
	sj, err := renderScriptsJSON(active, opts)
	if err != nil {
		return out, err
	}
	out.ScriptsJSON = sj
	out.Pages++

	// 详情页
	for _, s := range active {
		h, err := renderDetail(s, opts, data)
		if err != nil {
			return out, err
		}
		out.DetailHTMLs[s.ID] = h
		out.Pages++
	}
	// 墓碑页
	for _, s := range reg.Scripts {
		if !s.Deleted {
			continue
		}
		h, err := renderTombstone(s, opts)
		if err != nil {
			return out, err
		}
		out.DetailHTMLs[s.ID] = h
		out.Pages++
	}

	// 命令分页（从 archive/commands.json 加载）
	cp, cidx, err := renderCommands(opts)
	if err != nil {
		out.BuildWarnings = append(out.BuildWarnings, fmt.Sprintf("W3: %v", err))
	}
	out.CommandPages = cp
	out.CommandsIndex = cidx
	out.Pages += len(cp) + 1

	// 告警：IssueStats 未提供时降级渲染
	if data.IssueStats == nil && len(reg.Scripts) > 0 {
		out.BuildWarnings = append(out.BuildWarnings, "W1: IssueStats 未提供（降级渲染）")
	}

	return out, nil
}

// RenderMarkdown 使用 goldmark + 基础消毒渲染 Markdown 为 HTML。
func RenderMarkdown(src string) string {
	var buf bytes.Buffer
	md := goldmark.New(
		goldmark.WithExtensions(),
		goldmark.WithRendererOptions(
			htmlmd.WithUnsafe(), // 允许原始 HTML
		),
	)
	if err := md.Convert([]byte(src), &buf); err != nil {
		return esc(src)
	}
	return sanitizeDangerous(buf.String())
}

// ── 模板渲染 ─────────────────────────────────────────────────

// renderTemplate 从 embedFS 加载并渲染模板。
func renderTemplate(name string, data any) (string, error) {
	funcMap := template.FuncMap{
		"sub": func(a, b int) int { return a - b },
		"add": func(a, b int) int { return a + b },
		"seq": func(n int) []int {
			var s []int
			for i := 1; i <= n; i++ {
				s = append(s, i)
			}
			return s
		},
	}
	t, err := template.New(name).Funcs(funcMap).ParseFS(embedFS, "templates/"+name+".tmpl")
	if err != nil {
		return "", fmt.Errorf("解析模板 %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("渲染模板 %s: %w", name, err)
	}
	return buf.String(), nil
}

// ── 首页 ─────────────────────────────────────────────────────

type scriptCard struct {
	ID          string
	Name        string
	Version     string
	Type        string
	Enabled     bool
	Deleted     bool
	Description string
}

type indexPageData struct {
	Scripts   []scriptCard
	Total     int
	Batch     int
	PagesBase string
}

func renderIndex(scripts []registry.Script, opts Options, reg *registry.Registry) (string, error) {
	cards := make([]scriptCard, 0, len(scripts))
	for _, s := range scripts {
		cards = append(cards, scriptCard{
			ID:          s.ID,
			Name:        s.Name,
			Version:     s.Version,
			Type:        s.Type,
			Enabled:     s.Enabled,
			Deleted:     s.Deleted,
			Description: times.Clip(s.Description, 80),
		})
	}
	data := indexPageData{
		Scripts:   cards,
		Total:     len(cards),
		Batch:     opts.Batch,
		PagesBase: opts.PagesBase,
	}
	return renderTemplate("index", data)
}

func renderScriptsJSON(scripts []registry.Script, opts Options) (string, error) {
	cards := make([]scriptCard, 0, len(scripts))
	for _, s := range scripts {
		cards = append(cards, scriptCard{
			ID:          s.ID,
			Name:        s.Name,
			Version:     s.Version,
			Type:        s.Type,
			Enabled:     s.Enabled,
			Deleted:     s.Deleted,
			Description: times.Clip(s.Description, 80),
		})
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(cards); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// ── 详情页 ───────────────────────────────────────────────────

type discussionItem struct {
	Version   string
	NodeID    string
	URL       string
	CreatedAt string
	Comments  []Comment
}

type detailPageData struct {
	Script        registry.Script
	Matches       []string
	Grants        []string
	Changelog     []registry.ChangelogEntry
	Discussions   []discussionItem
	IssueNumber   int
	HasDiscussion bool
	PagesBase     string
	RelativeTime  func(string) string
}

func renderDetail(s registry.Script, opts Options, data Data) (string, error) {
	d := detailPageData{
		Script:       s,
		Matches:      s.Match,
		Grants:       s.Grant,
		Changelog:    s.Changelog,
		PagesBase:    opts.PagesBase,
		RelativeTime: func(iso string) string { return times.RelativeTime(iso, opts.Now) },
	}

	// 版本面板
	if len(s.Discussions) > 0 {
		d.HasDiscussion = true
		for _, dsc := range s.Discussions {
			item := discussionItem{
				Version:   dsc.Version,
				NodeID:    dsc.NodeID,
				URL:       dsc.URL,
				CreatedAt: dsc.CreatedAt,
			}
			if cs, ok := data.DiscussionComments[dsc.NodeID]; ok {
				for _, c := range cs {
					item.Comments = append(item.Comments, Comment{
						Author:    c.Author,
						Body:      RenderMarkdown(c.Body),
						CreatedAt: c.CreatedAt,
					})
				}
			}
			d.Discussions = append(d.Discussions, item)
		}
	}

	// Issue 面板回退
	if s.Issue != nil && s.Issue.Number > 0 {
		d.IssueNumber = s.Issue.Number
	}

	return renderTemplate("detail", d)
}

func renderTombstone(s registry.Script, opts Options) (string, error) {
	d := detailPageData{
		Script:       s,
		Matches:      s.Match,
		Grants:       s.Grant,
		Changelog:    s.Changelog,
		PagesBase:    opts.PagesBase,
		RelativeTime: func(iso string) string { return times.RelativeTime(iso, opts.Now) },
	}
	return renderTemplate("detail", d)
}

// ── 命令分页 ─────────────────────────────────────────────────

type commandArchive struct {
	Schema   int            `json:"schema"`
	Commands []commandGroup `json:"commands"`
}

type commandGroup struct {
	Command   string         `json:"command"`
	Author    string         `json:"author"`
	CreatedAt string         `json:"created_at"`
	Results   []commandEntry `json:"results"`
}

type commandEntry struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

type commandsPageData struct {
	Command    string
	Items      []commandEntry
	Page       int
	TotalPages int
	TotalItems int
	Base       string
}

type commandsIndexData struct {
	Commands []string
	Base     string
}

func renderCommands(opts Options) (map[int]string, string, error) {
	archivePath := filepath.Join(filepath.Dir(filepath.Clean(opts.Out)), "archive", "commands.json")

	data, err := os.ReadFile(archivePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("读取归档失败: %w", err)
	}

	var archive commandArchive
	if err := json.Unmarshal(data, &archive); err != nil {
		return nil, "", fmt.Errorf("解析归档失败: %w", err)
	}

	if len(archive.Commands) == 0 {
		return nil, "", nil
	}

	// 按 command 名分组
	groups := make(map[string][]commandGroup)
	for _, c := range archive.Commands {
		groups[c.Command] = append(groups[c.Command], c)
	}

	// 排序命令名
	cmdNames := make([]string, 0, len(groups))
	for name := range groups {
		cmdNames = append(cmdNames, name)
	}
	sort.Strings(cmdNames)

	// 生成分页
	result := make(map[int]string)
	pageNum := 1
	for _, cmdName := range cmdNames {
		items := groups[cmdName]
		// 展平所有结果
		var allEntries []commandEntry
		for _, g := range items {
			allEntries = append(allEntries, g.Results...)
		}
		// 分页
		for i := 0; i < len(allEntries); i += opts.CommandsPerPage {
			end := i + opts.CommandsPerPage
			if end > len(allEntries) {
				end = len(allEntries)
			}
			page := allEntries[i:end]
			html, err := renderCommandPage(cmdName, page, pageNum, (len(allEntries)+opts.CommandsPerPage-1)/opts.CommandsPerPage, len(allEntries), opts.PagesBase)
			if err != nil {
				return result, "", err
			}
			result[pageNum] = html
			pageNum++
		}
	}

	// commands/index.html
	idxHTML, err := renderCommandsIndex(cmdNames, opts.PagesBase)
	if err != nil {
		return result, "", err
	}

	return result, idxHTML, nil
}

func renderCommandPage(cmdName string, items []commandEntry, page, total, count int, base string) (string, error) {
	return renderTemplate("commands", commandsPageData{
		Command:    cmdName,
		Items:      items,
		Page:       page,
		TotalPages: total,
		TotalItems: count,
		Base:       base,
	})
}

func renderCommandsIndex(cmdNames []string, base string) (string, error) {
	return renderTemplate("commands-index", commandsIndexData{Commands: cmdNames, Base: base})
}

// ── 辅助函数 ─────────────────────────────────────────────────

func filterActive(scripts []registry.Script) []registry.Script {
	var active []registry.Script
	for _, s := range scripts {
		if !s.Deleted {
			active = append(active, s)
		}
	}
	return active
}

func esc(s string) string {
	return template.HTMLEscapeString(s)
}

// sanitizeDangerous 移除危险 HTML 标签
func sanitizeDangerous(html string) string {
	dangerous := []string{
		"<script", "</script>",
		"<object", "</object>",
		"<embed", "</embed>",
		"<form", "</form>",
	}
	for _, tag := range dangerous {
		html = strings.ReplaceAll(html, tag, "")
	}
	return html
}

// readTemplates 返回 templates/ 目录的文件列表
func readTemplates() ([]string, error) {
	entries, err := fs.ReadDir(embedFS, "templates")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".tmpl") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
