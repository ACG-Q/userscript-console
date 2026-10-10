// Package pages 提供站点生成器（HTML + JSON + 文档页）。
//
// 渲染规则（DR-6/DR-7）：
//   - HTML：`html/template` + `go:embed`（上下文自动转义 = XSS 防线，禁止手拼 HTML 字符串）。
//   - 两段式渲染：body define 先渲染，再嵌入 page-shell（assets.tmpl 的 7 段静态资产
//     以 `{{define}}` 形式内联，`{{template}}` 在 `<style>` 内是静态文本，不转义）。
//   - Markdown：`goldmark` + `bluemonday`（DR-6：无需与 Python 字节比对，XSS 用例硬验收）；
//     仅用于文档正文与说明，评论正文一律纯文本（贴 projec-02 `_comment_payload`）。
//   - 投影：Build 确定性输出；once-online 接受一次性重写（DR-6）。
package pages

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/times"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	htmlmd "github.com/yuin/goldmark/renderer/html"
)

//go:embed templates/*.tmpl
var embedFS embed.FS

// controlIssue 管理入口固定 Issue #1（对齐 projec-02 CONFIG）。
const controlIssue = 1

// Options 控制站点生成行为。
type Options struct {
	Out             string // 输出根目录，空→"dist"
	Batch           int    // 首页每批数量，≤0→10
	CommandsPerPage int    // 命令分页条数，≤0→5
	PagesBase       string // 站点基址，如 "https://owner.github.io/repo"
	Version         string // usm 版本（页脚/meta 展示），空→"dev"
	ArchivePath     string // 命令归档文件，空→<Out 父目录>/archive/commands.json（设计 §2 D6）
	Now             time.Time
}

func (o *Options) normalize() {
	if o.Out == "" {
		o.Out = "dist"
	}
	if o.ArchivePath == "" {
		o.ArchivePath = filepath.Join(filepath.Dir(filepath.Clean(o.Out)), "archive", "commands.json")
	}
	if o.Batch <= 0 {
		o.Batch = 10
	}
	if o.CommandsPerPage <= 0 {
		o.CommandsPerPage = 5
	}
	if o.Version == "" {
		o.Version = "dev"
	}
	o.PagesBase = strings.TrimRight(o.PagesBase, "/")
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
}

// Data 是从 registry + Discussion 计算的辅助数据。
type Data struct {
	// Discussions nodeID -> 版本帖讨论数据；拉取失败或降级时为 nil。
	Discussions map[string]Thread
}

// Thread 单个版本帖的讨论数据。
type Thread struct {
	Comments  []Comment
	HasAnswer bool
}

// Comment 一条版本帖评论（正文纯文本，渲染处统一转义）。
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
	DocHTMLs      map[string]string // docs/<name>.html -> HTML
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
		DocHTMLs:     make(map[string]string),
	}

	// 活跃脚本排序：!Deleted 按 UpdatedAt 降序、并列 ID 升序
	active := filterActive(reg.Scripts)
	sort.Slice(active, func(i, j int) bool {
		if active[i].UpdatedAt != active[j].UpdatedAt {
			return active[i].UpdatedAt > active[j].UpdatedAt
		}
		return active[i].ID < active[j].ID
	})

	// 文档页（HasDocs 供 shell 导航判定；index.md 缺失则整站不产 docs，防死链）
	docPages, err := loadDocs(opts)
	if err != nil {
		return out, err
	}
	r, err := newRenderer(deriveRepo(opts.PagesBase), len(docPages) > 0, opts.Version)
	if err != nil {
		return out, err
	}
	for _, dp := range docPages {
		root := "../index.html"
		if dp.dir != "" {
			root = "../../index.html"
		}
		d := docsData{Title: dp.title, TOC: docPages.tocFrom(dp), Content: template.HTML(RenderMarkdown(dp.content))}
		h, err := r.renderPage(dp.title, "docs", d, nil, root)
		if err != nil {
			return out, err
		}
		out.DocHTMLs[dp.outName()] = h
		out.Pages++
	}

	// 命令分页（从 archive/commands.json 加载）
	cp, cidx, err := r.renderCommands(opts)
	if err != nil {
		out.BuildWarnings = append(out.BuildWarnings, fmt.Sprintf("W3: %v", err))
	}
	out.CommandPages = cp
	out.CommandsIndex = cidx
	out.Pages += len(cp) + 1

	// 卡片聚合（讨论摘要随 card 一次算清）
	cards := make([]cardData, 0, len(active))
	for _, s := range active {
		cards = append(cards, r.buildCard(s, opts, data))
	}
	// index.html：服务端只渲染前 Batch 张（贴 projec-02 shown = cards[:LIST_BATCH]），
	// 余量由 list-js 从 scripts.json 按 shown 计数续载
	total := len(cards)
	hasMore := total > opts.Batch
	shown := cards
	if hasMore {
		shown = cards[:opts.Batch]
	}

	// hero 统计：降级（data.Discussions == nil）→「—」
	replies, answered := "—", "—"
	if data.Discussions != nil {
		sum, cnt := 0, 0
		for _, c := range cards {
			if c.Kind != 2 {
				continue
			}
			sum += c.ReplyCount
			if c.Answered {
				cnt++
			}
		}
		replies = strconv.Itoa(sum)
		answered = strconv.Itoa(cnt)
	}

	extraJS := []string{"filter-js"}
	if hasMore {
		extraJS = append(extraJS, "list-js")
	}
	idxData := indexData{
		Total:    total,
		Replies:  replies,
		Answered: answered,
		Batch:    opts.Batch,
		Cards:    shown,
		HasMore:  hasMore,
		ListEmpty: emptyState{
			Text: "暂无脚本，请在命令面板 Issue #1 中使用 /add 添加。",
		},
		FilterEmpty: emptyState{
			ID:     "filter-empty",
			Hidden: true,
			Text:   "没有符合筛选条件的脚本",
		},
	}
	idx, err := r.renderPage("脚本控制台", "index", idxData, extraJS, "index.html")
	if err != nil {
		return out, err
	}
	out.IndexHTML = idx
	out.Pages++

	// scripts.json：全量卡片 HTML（懒加载追加，与首页同源渲染）
	sj, err := r.renderScriptsJSON(cards)
	if err != nil {
		return out, err
	}
	out.ScriptsJSON = sj
	out.Pages++

	// 详情页（活跃 + 墓碑；Deleted 由 detail 模板降级展示）
	for _, s := range reg.Scripts {
		h, err := r.renderDetail(s, opts, data)
		if err != nil {
			return out, err
		}
		out.DetailHTMLs[s.ID] = h
		out.Pages++
	}

	// 告警：有脚本但讨论数据未提供时降级渲染（fetchData 单一来源，见 cmd/usm/site.go）
	if data.Discussions == nil && len(reg.Scripts) > 0 {
		out.BuildWarnings = append(out.BuildWarnings, "W1: 讨论数据未提供（降级渲染）")
	}

	return out, nil
}

// RenderMarkdown 使用 goldmark 渲染 Markdown，并用 bluemonday UGCPolicy 消毒
// （规格 D-04：WithUnsafe 放行原始 HTML 后必须过 bluemonday，黑名单手写不可靠）。
func RenderMarkdown(src string) string {
	var buf bytes.Buffer
	md := goldmark.New(
		goldmark.WithExtensions(),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(), // h2 锚点 id：右栏本页目录依赖（经 bluemonday 存活）
		),
		goldmark.WithRendererOptions(
			htmlmd.WithUnsafe(), // 允许原始 HTML，交给 bluemonday 兜底消毒
		),
	)
	if err := md.Convert([]byte(src), &buf); err != nil {
		return esc(src)
	}
	return bluemonday.UGCPolicy().Sanitize(buf.String())
}

// ── 两段式渲染 ───────────────────────────────────────────────

type renderer struct {
	t       *template.Template
	repo    string
	issue   int
	hasDocs bool
	version string
}

func newRenderer(repo string, hasDocs bool, version string) (*renderer, error) {
	funcMap := template.FuncMap{
		"sub": func(a, b int) int { return a - b },
		"add": func(a, b int) int { return a + b },
	}
	t, err := template.New("root").Funcs(funcMap).ParseFS(embedFS, "templates/*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("解析模板: %w", err)
	}
	return &renderer{t: t, repo: repo, issue: controlIssue, hasDocs: hasDocs, version: version}, nil
}

// renderPage 两段式：先渲染 body define，再嵌入 page-shell。
// rootHref：首页 "index.html"，子页 "../index.html"（Rel 由前缀推导）。
func (r *renderer) renderPage(title, bodyName string, data any, extraJS []string, rootHref string) (string, error) {
	var body bytes.Buffer
	if err := r.t.ExecuteTemplate(&body, bodyName, data); err != nil {
		return "", fmt.Errorf("渲染 %s: %w", bodyName, err)
	}
	var js strings.Builder
	for _, name := range extraJS {
		if err := r.t.ExecuteTemplate(&js, name, nil); err != nil {
			return "", fmt.Errorf("渲染 %s: %w", name, err)
		}
	}
	rel := ""
	if strings.HasPrefix(rootHref, "../") {
		rel = "../"
	}
	shell := shellData{
		Title:    title,
		RootHref: rootHref,
		Rel:      rel,
		Repo:     r.repo,
		Issue:    r.issue,
		HasDocs:  r.hasDocs,
		Version:  r.version,
		Body:     template.HTML(body.String()),
		ExtraJS:  template.HTML(js.String()),
	}
	var buf bytes.Buffer
	if err := r.t.ExecuteTemplate(&buf, "page-shell", shell); err != nil {
		return "", fmt.Errorf("渲染 page-shell: %w", err)
	}
	return buf.String(), nil
}

// ── 模板数据结构 ─────────────────────────────────────────────

type shellData struct {
	Title    string
	RootHref string
	Rel      string
	Repo     string
	Issue    int
	HasDocs  bool
	Version  string
	Body     template.HTML
	ExtraJS  template.HTML
}

type emptyState struct {
	Small    bool
	ID       string
	Hidden   bool
	Text     string
	LinkHref string
	LinkText string
	External bool
}

type indexData struct {
	Total       int
	Replies     string
	Answered    string
	Batch       int
	Cards       []cardData
	HasMore     bool
	ListEmpty   emptyState
	FilterEmpty emptyState
}

// cardData 扁平承载 script-card 及其全部子 define（type-pill/version-pill/
// status-badge/install-btn/card-disc/issue-badges），SourceType 已解引用为 string。
type cardData struct {
	ID          string
	Name        string
	Type        string
	SourceType  string
	Version     string
	Enabled     bool
	Description string
	MatchFirst  string
	TimeLabel   string
	DiscHref    string
	Label       string // install-btn 文案："安装"
	PagesBase   string

	// card-disc
	Kind          int // 0 无讨论 / 1 降级 / 2 有数据
	NonePanel     emptyState
	DegradedPanel emptyState
	Answered      bool
	ReplyCount    int
	HasLatest     bool
	LatestBody    string
	LatestWho     string
	LatestTime    string
}

type installBtnData struct {
	Enabled   bool
	PagesBase string
	ID        string
	Label     string
}

type cmtView struct {
	Author string
	Time   string
	Body   string
}

type discPostView struct {
	Label      string
	Version    string
	Number     int
	URL        string
	Answered   bool
	ReplyCount int
	Comments   []cmtView
}

// discPayload* 是 #discData JSON 载荷（disc-js 消费；省略 is_owner/is_answer/
// replies，JS falsy 安全）。json.Marshal 默认转义 <>&，防 </script> 注入。
type discPayloadComment struct {
	Author string `json:"author"`
	Time   string `json:"time"`
	Body   string `json:"body"`
}

type discPayloadPost struct {
	Version    string               `json:"version"`
	Number     int                  `json:"number"`
	URL        string               `json:"url"`
	IsAnswered bool                 `json:"is_answered"`
	ReplyCount int                  `json:"reply_count"`
	Comments   []discPayloadComment `json:"comments"`
}

type detailData struct {
	Script         registry.Script
	Author         string
	When           string
	Matches        []string
	Description    string
	DocHTML        template.HTML
	Install        installBtnData
	HasPosts       bool
	Posts          []discPostView
	EmptyComments  emptyState
	FallbackPanel  emptyState
	DiscJSON       template.JS
	Changelog      []registry.ChangelogEntry
	EmptyChangelog emptyState
}

type cmdResultView struct {
	Author string
	When   string
	Body   string
}

type cmdEntryView struct {
	Author  string
	When    string
	No      int
	Command string
	Results []cmdResultView
}

type commandsData struct {
	Note       string
	Entries    []cmdEntryView
	EmptyPanel emptyState
	Page       int
	TotalPages int
}

type docTOCEntry struct {
	Href string
	Name string
}

type docsData struct {
	Title   string
	TOC     []docTOCEntry
	Content template.HTML
}

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

// ── 首页卡片聚合 ─────────────────────────────────────────────

// buildCard 聚合单个脚本的卡片视图与讨论摘要（三态贴 projec-02
// script_issue_panel；数据源为 Discussions 账本 + 抓取结果）。
func (r *renderer) buildCard(s registry.Script, opts Options, data Data) cardData {
	c := cardData{
		ID:          s.ID,
		Name:        s.Name,
		Type:        s.Type,
		Version:     s.Version,
		Enabled:     s.Enabled,
		Description: s.Description, // 不截断（贴 Python script_card）
		Label:       "安装",
		PagesBase:   opts.PagesBase,
	}
	if c.Name == "" {
		c.Name = s.ID
	}
	if s.SourceType != nil {
		c.SourceType = *s.SourceType
	}
	if len(s.Match) > 0 {
		c.MatchFirst = s.Match[0]
	}
	c.TimeLabel = timeLabel(s)
	c.DiscHref = r.discussionHref(s)

	ledger := len(s.Discussions) > 0
	issueURL := ""
	if s.Issue != nil {
		issueURL = safeURL(s.Issue.URL)
	}
	nonePanel := emptyState{
		Small:    true,
		Text:     "还没有讨论",
		LinkHref: r.issuesListURL(),
		LinkText: "发起讨论 →",
		External: true,
	}
	degradedPanel := func() emptyState {
		return emptyState{
			Small:    true,
			Text:     "摘要暂不可用",
			LinkHref: r.openURL(s),
			LinkText: "在 GitHub 打开 →",
			External: true,
		}
	}

	if data.Discussions == nil {
		if (ledger || issueURL != "") && r.openURL(s) != "" {
			c.Kind = 1
			c.DegradedPanel = degradedPanel()
		} else {
			c.Kind = 0
			c.NonePanel = nonePanel
		}
		return c
	}

	// 收集该脚本全部线程（账本倒序 = 新→旧）
	var ths []Thread
	for i := len(s.Discussions) - 1; i >= 0; i-- {
		if th, ok := data.Discussions[s.Discussions[i].NodeID]; ok {
			ths = append(ths, th)
		}
	}
	if len(ths) == 0 {
		if (ledger || issueURL != "") && r.openURL(s) != "" {
			c.Kind = 1
			c.DegradedPanel = degradedPanel()
		} else {
			c.Kind = 0
			c.NonePanel = nonePanel
		}
		return c
	}

	c.Kind = 2
	for _, th := range ths {
		c.ReplyCount += len(th.Comments)
		if th.HasAnswer {
			c.Answered = true
		}
	}
	for _, th := range ths {
		if len(th.Comments) == 0 {
			continue
		}
		last := th.Comments[len(th.Comments)-1] // GitHub 原序 = 旧→新，末条即最新
		who := last.Author
		if who == "" {
			who = "未知用户"
		}
		c.HasLatest = true
		c.LatestBody = times.Clip(last.Body, 80)
		c.LatestWho = who
		c.LatestTime = times.RelativeTime(last.CreatedAt, opts.Now)
		break
	}
	return c
}

// discussionHref 卡片「讨论」按钮：最新版本帖优先，其次脚本 Issue，最终 Issue 列表
// （贴 projec-02 discussion_href）。
func (r *renderer) discussionHref(s registry.Script) string {
	for i := len(s.Discussions) - 1; i >= 0; i-- {
		if u := safeURL(s.Discussions[i].URL); u != "" {
			return u
		}
	}
	if s.Issue != nil {
		if u := safeURL(s.Issue.URL); u != "" {
			return u
		}
	}
	return r.issuesListURL()
}

// openURL 降级面板跳转目标：脚本 Issue 优先，其次最新版本帖。
func (r *renderer) openURL(s registry.Script) string {
	if s.Issue != nil {
		if u := safeURL(s.Issue.URL); u != "" {
			return u
		}
	}
	for i := len(s.Discussions) - 1; i >= 0; i-- {
		if u := safeURL(s.Discussions[i].URL); u != "" {
			return u
		}
	}
	return ""
}

// timeLabel 卡片时间标签（贴 projec-02 script_card）。
func timeLabel(s registry.Script) string {
	if s.Type == registry.TypeSynced {
		when := ""
		if s.LastSyncedAt != nil {
			when = first10(*s.LastSyncedAt)
		}
		if when == "" {
			when = first10(s.UpdatedAt)
		}
		if when != "" {
			return "同步于 " + when
		}
		return "同步"
	}
	if when := first10(s.UpdatedAt); when != "" {
		return "更新于 " + when
	}
	return ""
}

// renderScriptsJSON 全量卡片 HTML 进 scripts.json（懒加载追加；与首页同源）。
func (r *renderer) renderScriptsJSON(cards []cardData) (string, error) {
	type item struct {
		Type string `json:"type"`
		HTML string `json:"html"`
	}
	items := make([]item, 0, len(cards))
	for _, c := range cards {
		var buf bytes.Buffer
		if err := r.t.ExecuteTemplate(&buf, "script-card", c); err != nil {
			return "", fmt.Errorf("渲染 script-card: %w", err)
		}
		items = append(items, item{Type: c.Type, HTML: buf.String()})
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false) // 贴 Python json.dumps：不转义 <>&
	if err := enc.Encode(items); err != nil {
		return "", err
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}

// ── 详情页 ───────────────────────────────────────────────────

func (r *renderer) renderDetail(s registry.Script, opts Options, data Data) (string, error) {
	author := s.Author
	if author == "" {
		author = "-"
	}
	when := first10(s.UpdatedAt)
	if when == "" && s.LastSyncedAt != nil {
		when = first10(*s.LastSyncedAt)
	}
	doc := s.Documentation
	if doc == "" {
		doc = "_暂无文档_"
	}
	d := detailData{
		Script:      s,
		Author:      author,
		When:        when,
		Matches:     s.Match,
		Description: s.Description,
		DocHTML:     template.HTML(RenderMarkdown(doc)),
		Install: installBtnData{
			Enabled:   s.Enabled,
			PagesBase: opts.PagesBase,
			ID:        s.ID,
			Label:     "安装脚本",
		},
		EmptyComments:  emptyState{Small: true, Text: "该版本帖还没有评论"},
		EmptyChangelog: emptyState{Text: "暂无更新记录。"},
		Changelog:      s.Changelog,
	}

	// 版本面板：账本倒序（新→旧），抓取失败的条目跳过
	first := true
	for i := len(s.Discussions) - 1; i >= 0; i-- {
		dsc := s.Discussions[i]
		th, ok := data.Discussions[dsc.NodeID]
		if !ok {
			continue
		}
		label := "未知版本"
		if dsc.Version != "" {
			label = "v" + dsc.Version
		}
		if first {
			label += "（最新）"
			first = false
		}
		p := discPostView{
			Label:      label,
			Version:    dsc.Version,
			Number:     dsc.Number,
			URL:        safeURL(dsc.URL),
			Answered:   th.HasAnswer,
			ReplyCount: len(th.Comments),
		}
		for _, c := range th.Comments {
			who := c.Author
			if who == "" {
				who = "未知用户"
			}
			p.Comments = append(p.Comments, cmtView{
				Author: who,
				Time:   times.RelativeTime(c.CreatedAt, opts.Now),
				Body:   times.Clip(c.Body, 400),
			})
		}
		d.Posts = append(d.Posts, p)
	}
	d.HasPosts = len(d.Posts) > 0

	if d.HasPosts {
		payload := make([]discPayloadPost, 0, len(d.Posts))
		for _, p := range d.Posts {
			dp := discPayloadPost{
				Version:    p.Version,
				Number:     p.Number,
				URL:        p.URL,
				IsAnswered: p.Answered,
				ReplyCount: p.ReplyCount,
				Comments:   make([]discPayloadComment, 0, len(p.Comments)),
			}
			for _, c := range p.Comments {
				dp.Comments = append(dp.Comments, discPayloadComment(c))
			}
			payload = append(payload, dp)
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return "", fmt.Errorf("序列化讨论数据: %w", err)
		}
		d.DiscJSON = template.JS(b)
	} else if open := r.openURL(s); open != "" {
		d.FallbackPanel = emptyState{
			Small:    true,
			Text:     "摘要暂不可用",
			LinkHref: open,
			LinkText: "在 GitHub 打开 →",
			External: true,
		}
	} else {
		d.FallbackPanel = emptyState{
			Small:    true,
			Text:     "还没有讨论",
			LinkHref: r.issuesListURL(),
			LinkText: "发起讨论 →",
			External: true,
		}
	}

	title := s.Name
	if title == "" {
		title = s.ID
	}
	return r.renderPage(title, "detail", d, []string{"disc-js"}, "../index.html")
}

// ── 命令分页 ─────────────────────────────────────────────────

// renderCommands 渲染命令归档：全局倒序整组分页（对齐 build_pages.py
// build_command_pages），每页 opts.CommandsPerPage 组；空归档也产空态 page-1；
// index 为 meta-refresh。
func (r *renderer) renderCommands(opts Options) (map[int]string, string, error) {
	opts.normalize() // 直接调用方可能未归一（测试/内部调用），与 Build 主路径同待遇
	archivePath := opts.ArchivePath

	var groups []commandGroup
	data, err := os.ReadFile(archivePath)
	if err == nil {
		var archive commandArchive
		if err := json.Unmarshal(data, &archive); err != nil {
			return nil, "", fmt.Errorf("解析归档失败: %w", err)
		}
		groups = archive.Commands
	} else if !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("读取归档失败: %w", err)
	}

	// ISO8601 字典序即时间序：新→旧
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].CreatedAt > groups[j].CreatedAt
	})

	perPage := opts.CommandsPerPage
	totalPages := (len(groups) + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1 // 空归档也产空态 page-1（CUTOVER 空态可达）
	}
	note := "命令面板中更早的「命令 + 执行结果」清理后会自动归档到这里。"
	if len(groups) > 0 {
		note = fmt.Sprintf("共 %d 条历史命令，清理自命令面板 Issue #1，按时间新→旧排列，每页 %d 条。",
			len(groups), perPage)
	}
	emptyPanel := emptyState{
		Text:     "还没有归档的命令记录。",
		LinkHref: r.issueURL(),
		LinkText: "去命令面板发一条命令 →",
		External: true,
	}

	result := make(map[int]string, totalPages)
	for p := 1; p <= totalPages; p++ {
		start := (p - 1) * perPage
		end := start + perPage
		if end > len(groups) {
			end = len(groups)
		}
		chunk := groups[start:end]
		entries := make([]cmdEntryView, 0, len(chunk))
		for i, g := range chunk {
			author := g.Author
			if author == "" {
				author = "未知用户"
			}
			e := cmdEntryView{
				Author:  author,
				When:    cmdTime(g.CreatedAt),
				No:      i + 1, // 序号每页从 1 起（贴 Python enumerate(chunk, start=1)）
				Command: g.Command,
			}
			for _, res := range g.Results {
				ra := res.Author
				if ra == "" {
					ra = "未知用户"
				}
				e.Results = append(e.Results, cmdResultView{
					Author: ra,
					When:   cmdTime(res.CreatedAt),
					Body:   res.Body,
				})
			}
			entries = append(entries, e)
		}
		d := commandsData{
			Note:       note,
			Entries:    entries,
			EmptyPanel: emptyPanel,
			Page:       p,
			TotalPages: totalPages,
		}
		h, err := r.renderPage(fmt.Sprintf("命令归档 · 第 %d 页", p), "commands", d, nil, "../index.html")
		if err != nil {
			return result, "", err
		}
		result[p] = h
	}

	var idx bytes.Buffer
	// data 传 shellData：commands-index 是重定向壳页，只需要页头 meta 的 Version。
	if err := r.t.ExecuteTemplate(&idx, "commands-index", shellData{Version: r.version}); err != nil {
		return result, "", fmt.Errorf("渲染 commands-index: %w", err)
	}
	return result, idx.String(), nil
}

// cmdTime 归档时间戳 → `YYYY-MM-DD HH:MM`；解析失败原样返回；空 → 占位符。
func cmdTime(iso string) string {
	if iso == "" {
		return "—"
	}
	moment, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		moment, err = time.Parse("2006-01-02T15:04:05", iso)
	}
	if err != nil {
		return iso
	}
	return moment.Format("2006-01-02 15:04")
}

// ── 文档页 ───────────────────────────────────────────────────

type docPage struct {
	dir     string // ""（顶层）或 "commands"（唯一白名单子目录）
	slug    string
	title   string
	content string
}

// outName 输出文件相对 dist/docs 的键。
func (p docPage) outName() string {
	if p.dir == "" {
		return p.slug + ".html"
	}
	return p.dir + "/" + p.slug + ".html"
}

type docPages []docPage

// tocFrom 从当前页视角生成目录链接：同目录裸 slug、跨目录带前缀
// （仅两级："" 与 commands/）。
func (ps docPages) tocFrom(cur docPage) []docTOCEntry {
	toc := make([]docTOCEntry, 0, len(ps))
	for _, p := range ps {
		var href string
		switch {
		case p.dir == cur.dir:
			href = p.slug + ".html"
		case cur.dir == "":
			href = p.dir + "/" + p.slug + ".html"
		default:
			href = "../" + p.slug + ".html"
		}
		toc = append(toc, docTOCEntry{Href: href, Name: p.title})
	}
	return toc
}

// docFile 一份待转换的 md：dir 为 ""（顶层）或 "commands"。
type docFile struct {
	dir  string
	name string
}

// listDocFiles 收集要转换的 md：顶层 *.md + commands/*.md（白名单子目录，
// dev/、superpowers/ 等其他子目录一律跳过）。顶层在前、组内文件名升序。
func listDocFiles(docsDir string) ([]docFile, error) {
	var files []docFile
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 docs 目录失败: %w", err)
	}
	var top, cmds []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		top = append(top, e.Name())
	}
	sort.Strings(top)
	for _, n := range top {
		files = append(files, docFile{dir: "", name: n})
	}
	cmdEntries, err := os.ReadDir(filepath.Join(docsDir, "commands"))
	if err != nil {
		if os.IsNotExist(err) {
			return files, nil
		}
		return nil, fmt.Errorf("读取 docs/commands 目录失败: %w", err)
	}
	for _, e := range cmdEntries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		cmds = append(cmds, e.Name())
	}
	sort.Strings(cmds)
	for _, n := range cmds {
		files = append(files, docFile{dir: "commands", name: n})
	}
	return files, nil
}

// loadDocs 读取 <Out 的父目录>/docs：顶层 *.md + commands/*.md（见 listDocFiles）。
// index.md 门禁只看顶层：缺失 → 整站不产 docs（nav HasDocs=false，防死链）。
func loadDocs(opts Options) (docPages, error) {
	docsDir := filepath.Join(filepath.Dir(filepath.Clean(opts.Out)), "docs")
	files, err := listDocFiles(docsDir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	seen := make(map[string]string, len(files)) // outName → 源文件名
	var pages docPages
	hasTopIndex := false
	for _, f := range files {
		stem := strings.TrimSuffix(f.name, filepath.Ext(f.name))
		slug := kebab(stem)
		if slug == "" {
			slug = "doc"
		}
		dp := docPage{dir: f.dir, slug: slug}
		if prev, ok := seen[dp.outName()]; ok {
			return nil, fmt.Errorf("文档 slug 冲突: %s 与 %s 都映射到 %s", prev, f.name, dp.outName())
		}
		seen[dp.outName()] = f.name
		if f.dir == "" && slug == "index" {
			hasTopIndex = true
		}
		b, err := os.ReadFile(filepath.Join(docsDir, f.dir, f.name))
		if err != nil {
			return nil, fmt.Errorf("读取文档 %s 失败: %w", filepath.Join(f.dir, f.name), err)
		}
		dp.content = string(b)
		dp.title = docTitle(dp.content, stem)
		pages = append(pages, dp)
	}
	if !hasTopIndex {
		return nil, nil
	}
	return pages, nil
}

// kebab 文件名 → URL slug：小写、非字母数字 → "-"、collapse/trim；保留中文。
func kebab(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// docTitle 首个 "# " 标题行，缺省回退文件名 stem。
func docTitle(content, fallback string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return fallback
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

// safeURL 仅放行 http(s) 绝对地址，其余（含 javascript:）置空。
func safeURL(u string) string {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return u
}

func (r *renderer) issuesListURL() string {
	if r.repo == "" {
		return ""
	}
	return "https://github.com/" + r.repo + "/issues?q=is%3Aissue+label%3Ascript"
}

func (r *renderer) issueURL() string {
	if r.repo == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/issues/%d", r.repo, r.issue)
}

// deriveRepo 从 PagesBase（owner.github.io/repo）推导 "owner/repo"；
// 非 GitHub Pages 或空 → ""（隐藏 GitHub/管理入口链接）。
func deriveRepo(pagesBase string) string {
	if pagesBase == "" {
		return ""
	}
	u, err := url.Parse(pagesBase)
	if err != nil || u.Host == "" {
		return ""
	}
	const suffix = ".github.io"
	if !strings.HasSuffix(u.Host, suffix) {
		return ""
	}
	owner := strings.TrimSuffix(u.Host, suffix)
	if owner == "" {
		return ""
	}
	seg := strings.Trim(u.Path, "/")
	if seg == "" {
		return ""
	}
	if i := strings.Index(seg, "/"); i >= 0 {
		seg = seg[:i]
	}
	return owner + "/" + seg
}

// first10 取日期前缀（YYYY-MM-DD）；短于 10 字节原样返回。
func first10(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}
