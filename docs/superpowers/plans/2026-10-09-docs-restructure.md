# 双仓文档重构实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 双仓文档重构——工具仓 `usm build` 支持 `docs/commands/` 子目录转换，写入 11 个文档 md，新增自举 Pages workflow；内容仓整删 `docs/`；全部验收后最终删除工具仓 `docs/superpowers/`。

**架构：** `internal/pages.loadDocs` 在顶层 `*.md` 之外仅增扫 `commands/` 白名单子目录（`docPage` 增 `dir` 字段、输出键 `commands/<slug>.html`、TOC/root 链接按两级层级计算）；`cmd/usm/site.go` 的 `writeSite` 路径校验放宽为"恰好一级 `[a-z0-9-]+/` 前缀"，`staleSiteFiles` 递归化。工具仓 Pages workflow 在 runner 临时生成空 `registry.json`（不落仓）后 `go run ./cmd/usm build` 自举发布。已批准规格：`docs/superpowers/specs/2026-10-09-docs-restructure-design.md`（f5d03c2）。

**技术栈：** Go 1.x（stdlib：os/filepath/regexp/io/fs）、GitHub Pages Actions（configure-pages/upload-pages-artifact/deploy-pages）、Markdown。

**环境注意（Windows + PowerShell 5.1）：** 含中文/特殊字符的 commit message 一律先用写文件工具写入临时文件再 `git commit -F <file>`；golangci-lint 前先 `$env:GOROOT=(go env GOROOT)`；git 的 stderr 输出（NativeCommandError）不代表失败，看退出码与实际输出。

---

## 文件结构

| 文件 | 动作 | 职责 |
|---|---|---|
| `internal/pages/pages.go` | 修改（:881-947、:118-135） | `docPage.dir`、`outName()`、`tocFrom()`、`listDocFiles()`、`loadDocs` 子目录扫描、Build 渲染层级 |
| `internal/pages/pages_test.go` | 修改（:1105-1118 helper + 新测试） | writeDocs 支持子目录 key；4 个新测试 |
| `cmd/usm/site.go` | 修改（:171-179、:202-222） | `validDocName()` 一级子目录放行；`staleSiteFiles` 递归 |
| `cmd/usm/site_test.go` | 修改（追加） | 4 个新测试 |
| `docs/index.md` | 创建 | 文档落地页（转换） |
| `docs/commands/{add,list,info,rm,sync,project,build,cleanup,cli}.md` | 创建 ×9 | 命令文档（全部转换） |
| `docs/dev/extending-sources.md` | 创建 | 扩展适配器指南（不转换） |
| `.github/workflows/pages.yml` | 创建 | 工具仓自举 Pages 发布 |
| `userscripts/docs/` | 整删（git rm -r） | 内容仓清理 |
| `userscripts/SPEC-DATA.md` | 修改（:108-118） | 文档归属表标注已移除 |
| `docs/superpowers/` | 整删（最后一步） | 内部档案最终清理（含本计划文件自身） |

---

## 任务 1：loadDocs 支持 commands/ 子目录（TDD，internal/pages）

**文件：**
- 修改：`internal/pages/pages.go:881-947`（docPage/toc/loadDocs）、`:118-135`（Build 渲染循环）
- 修改：`internal/pages/pages_test.go:1105-1118`（writeDocs helper）+ 追加新测试

- [ ] **步骤 1：扩展 writeDocs helper 支持子目录 key**

`internal/pages/pages_test.go` 中替换 writeDocs（现 :1105-1118）：

```go
// writeDocs 在 out 同级的 docs/ 写入文档文件；key 可含 "/"（如 "commands/add.md"）。
func writeDocs(t *testing.T, out string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(filepath.Dir(filepath.Clean(out)), "docs")
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
```

- [ ] **步骤 2：编写失败的测试**

在 `internal/pages/pages_test.go` 的 TestBuildDocsSlugConflict 之后追加：

```go
// TestBuildWithDocsCommandsSubdir commands/ 子目录转换：输出键带前缀、
// root 链接两级、TOC 按层级；dev/ 子目录不转换。
func TestBuildWithDocsCommandsSubdir(t *testing.T) {
	reg := buildTestRegistry(t)
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[]}`)
	writeDocs(t, outDir, map[string]string{
		"index.md":                 "# 文档中心\n\n索引。",
		"commands/add.md":          "# add 添加\n\n内容。",
		"dev/extending-sources.md": "# 扩展\n\n不转换。",
	})

	out, err := Build(reg, Options{Out: outDir, PagesBase: "https://test.github.io/repo"}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.DocHTMLs) != 2 {
		t.Fatalf("应产 index + commands/add 共 2 个文档页（dev/ 不转换）, got %d: %v", len(out.DocHTMLs), out.DocHTMLs)
	}
	idx, ok := out.DocHTMLs["index.html"]
	if !ok {
		t.Fatal("缺 docs/index.html")
	}
	if !strings.Contains(idx, `href="commands/add.html"`) {
		t.Errorf("顶层 TOC 应链到 commands/add.html")
	}
	add, ok := out.DocHTMLs["commands/add.html"]
	if !ok {
		t.Fatal("缺 docs/commands/add.html")
	}
	if !strings.Contains(add, `href="../../index.html"`) {
		t.Errorf("commands 页 root 链接应为 ../../index.html")
	}
	if !strings.Contains(add, `href="../index.html"`) {
		t.Errorf("commands 页 TOC 链回顶层 index 应为 ../index.html")
	}
	if !strings.Contains(add, `href="add.html"`) {
		t.Errorf("commands 页 TOC 链同级页应为裸 add.html")
	}
}

// TestBuildDocsWithCommandsNoTopIndex 有 commands/ 但无顶层 index.md
// → 整站仍不产 docs（门禁只认顶层 index.md）。
func TestBuildDocsWithCommandsNoTopIndex(t *testing.T) {
	reg := buildTestRegistry(t)
	outDir := isolatedOut(t)
	writeCommandsArchive(t, outDir, `{"schema":1,"commands":[]}`)
	writeDocs(t, outDir, map[string]string{"commands/add.md": "# add"})

	out, err := Build(reg, Options{Out: outDir, PagesBase: "https://test.github.io/repo"}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.DocHTMLs) != 0 {
		t.Errorf("无顶层 index.md 时 commands/ 不应产出文档页, got %v", out.DocHTMLs)
	}
	if strings.Contains(out.IndexHTML, "docs/index.html") {
		t.Errorf("无文档页时 nav 不应出现文档链接")
	}
}
```

- [ ] **步骤 3：运行测试验证失败**

运行（PowerShell，workdir 为工具仓根）：

```powershell
go test ./internal/pages/ -run 'TestBuildWithDocsCommandsSubdir|TestBuildDocsWithCommandsNoTopIndex' -v
```

预期：FAIL——`TestBuildWithDocsCommandsSubdir` 报 `应产 2 个文档页, got 1`（dev/、commands/ 都没扫）。

- [ ] **步骤 4：实现 docPage.dir / outName / tocFrom / listDocFiles / loadDocs / Build**

`internal/pages/pages.go` 改动四处。

4a. 替换 docPage 与 toc（现 :881-895）：

```go
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
//（仅两级："" 与 commands/）。
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
```

4b. 替换 loadDocs（现 :897-947）：

```go
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
```

4c. Build 渲染循环改层级（现 :127-135）：

```go
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
```

4d. 确认 `toc()` 无其他调用方后再删：`Select-String -Path internal\pages\*.go -Pattern '\.toc\('`——仅 pages.go:128（已在 4c 中替换为 tocFrom）。若还有其他文件命中，逐个改为 `tocFrom(<对应页>)`。

- [ ] **步骤 5：运行测试验证通过**

```powershell
go test ./internal/pages/ -v
```

预期：全部 PASS——新增 2 个测试通过，既有 TestBuildWithDocs、TestBuildWithoutIndexMD、TestBuildDocsSlugConflict 不回归。

- [ ] **步骤 6：运行全量测试**

```powershell
go test ./...
```

预期：全绿。

- [ ] **步骤 7：Commit**

写入临时文件（用写文件工具）`$env:TEMP\opencode\msg1.txt`，内容：

```
feat(pages): loadDocs 支持 commands/ 白名单子目录

docPage 增 dir 字段，输出键 commands/<slug>.html；
tocFrom 按当前页层级生成 TOC href；root 链接
commands/ 下为 ../../index.html。dev/ 等其他子目录
不转换（测试锁死）；index.md 门禁仍只认顶层。
```

```powershell
git add internal/pages/pages.go internal/pages/pages_test.go
git commit -F $env:TEMP\opencode\msg1.txt
```

---

## 任务 2：writeSite 一级子目录放行 + staleSiteFiles 递归（TDD，cmd/usm）

**文件：**
- 修改：`cmd/usm/site.go:171-179`（doc 循环）、`:202-222`（staleSiteFiles）
- 修改：`cmd/usm/site_test.go`（追加 4 个测试）

- [ ] **步骤 1：编写失败的测试**

在 `cmd/usm/site_test.go` 末尾追加：

```go
// TestWriteSiteAllowsDocsSubdir dist/docs/commands/<slug>.html 应放行并落盘。
func TestWriteSiteAllowsDocsSubdir(t *testing.T) {
	root := t.TempDir()
	out := pages.Outcome{
		IndexHTML:   "<html>home</html>",
		ScriptsJSON: "{}",
		DocHTMLs: map[string]string{
			"index.html":        "<html>idx</html>",
			"commands/add.html": "<html>add</html>",
		},
	}
	changed, err := writeSite(root, out)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("首次写入应 changed=true")
	}
	p := filepath.Join(root, "dist", "docs", "commands", "add.html")
	if b, err := os.ReadFile(p); err != nil || !strings.Contains(string(b), "add") {
		t.Errorf("应写入 %s, err=%v", p, err)
	}
}

// TestWriteSiteRejectsDeepDocsSubdir 二级以上子目录必须拒绝（路径契约）。
func TestWriteSiteRejectsDeepDocsSubdir(t *testing.T) {
	root := t.TempDir()
	out := pages.Outcome{
		IndexHTML:   "<html>home</html>",
		ScriptsJSON: "{}",
		DocHTMLs:    map[string]string{"a/b/c.html": "<html>x</html>"},
	}
	if _, err := writeSite(root, out); err == nil {
		t.Fatal("二级文档子目录应报错")
	}
}

// TestWriteSiteRejectsDocSubdirTraversal 含 .. 的键必须拒绝。
func TestWriteSiteRejectsDocSubdirTraversal(t *testing.T) {
	root := t.TempDir()
	out := pages.Outcome{
		IndexHTML:   "<html>home</html>",
		ScriptsJSON: "{}",
		DocHTMLs:    map[string]string{"commands/../evil.html": "<html>x</html>"},
	}
	if _, err := writeSite(root, out); err == nil {
		t.Fatal("含 .. 的文档键应报错")
	}
	if _, err := os.Stat(filepath.Join(root, "evil.html")); err == nil {
		t.Fatal("不应写出 dist 外文件")
	}
}

// TestWriteSiteRejectsDocPathForms 反斜杠、绝对路径、空段键必须拒绝。
func TestWriteSiteRejectsDocPathForms(t *testing.T) {
	for _, name := range []string{`commands\add.html`, "/tmp/evil.html", "commands//evil.html"} {
		root := t.TempDir()
		out := pages.Outcome{
			IndexHTML:   "<html>home</html>",
			ScriptsJSON: "{}",
			DocHTMLs:    map[string]string{name: "<html>x</html>"},
		}
		if _, err := writeSite(root, out); err == nil {
			t.Errorf("%q 应报错", name)
		}
	}
}

// TestWriteSiteRemovesStaleDocsSubdir 陈旧清理要递归进子目录。
func TestWriteSiteRemovesStaleDocsSubdir(t *testing.T) {
	root := t.TempDir()
	full := pages.Outcome{
		IndexHTML:   "<html>home</html>",
		ScriptsJSON: "{}",
		DocHTMLs: map[string]string{
			"index.html":        "<html>idx</html>",
			"commands/add.html": "<html>add</html>",
			"commands/old.html": "<html>old</html>",
		},
	}
	if _, err := writeSite(root, full); err != nil {
		t.Fatal(err)
	}
	pruned := pages.Outcome{
		IndexHTML:   "<html>home</html>",
		ScriptsJSON: "{}",
		DocHTMLs: map[string]string{
			"index.html":        "<html>idx</html>",
			"commands/add.html": "<html>add</html>",
		},
	}
	if _, err := writeSite(root, pruned); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(root, "dist", "docs", "commands", "old.html")
	if _, err := os.Stat(stale); err == nil {
		t.Error("陈旧的 commands/old.html 应被清理")
	}
	kept := filepath.Join(root, "dist", "docs", "commands", "add.html")
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("commands/add.html 不应被误删: %v", err)
	}
}
```

- [ ] **步骤 2：运行测试验证失败**

```powershell
go test ./cmd/usm/ -run 'TestWriteSiteAllowsDocsSubdir|TestWriteSiteRejectsDeepDocsSubdir|TestWriteSiteRejectsDocSubdirTraversal|TestWriteSiteRejectsDocPathForms|TestWriteSiteRemovesStaleDocsSubdir' -v
```

预期：FAIL——Allows 被 `非法文档文件名` 拒绝；RejectsDeep 目前返回 nil error（未收紧）；RejectsTraversal 的 `commands/../evil.html` 含 `..` 目前恰好被旧 ContainsAny 拦下、但 `commands\add.html` 等旧逻辑也拦（ContainsAny 含 `\`）——以 Allows 与 RemovesStale 的失败为驱动红；RejectsPathForms 中 `commands//evil.html` 目前不报错（旧逻辑只查 `\` 与 `..`，无 `/`）。

- [ ] **步骤 3：实现 validDocName + writeSite + staleSiteFiles**

3a. `cmd/usm/site.go` 确认导入含 `"path"`、`"regexp"`、`"io/fs"`（缺则补；现有 imports 视文件头部为准）。

3b. 新增 validDocName（放在 writeSite 之前）：

```go
// validDocName 文档输出键契约：<slug>.html 或恰好一级 <subdir>/<slug>.html
//（子目录白名单 [a-z0-9-]+）；拒绝 ..、\、绝对路径、更深层级。
func validDocName(name string) bool {
	if name == "" || strings.Contains(name, `\`) || strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) == 1 {
		return parts[0] == path.Base(parts[0])
	}
	if len(parts) != 2 {
		return false
	}
	if ok, _ := regexp.MatchString(`^[a-z0-9-]+$`, parts[0]); !ok {
		return false
	}
	return parts[1] != "" && parts[1] == path.Base(parts[1])
}
```

3c. 替换 writeSite 中 doc 循环（现 :171-179）：

```go
	for name, html := range out.DocHTMLs {
		// 文档输出键进入文件路径：只放行顶层或恰好一级白名单子目录。
		if !validDocName(name) {
			return false, fmt.Errorf("非法文档文件名 %q", name)
		}
		if err := write(filepath.ToSlash(filepath.Join(siteDocsDir, name)), html); err != nil {
			return false, err
		}
	}
```

3d. 替换 staleSiteFiles（现 :202-222）为递归版：

```go
// staleSiteFiles 列出 dir 下本轮未生成、且属于站点产物形态的陈旧文件。
// 只认 .html：dist/ 根另有 .user.js 分发产物，不能误伤。
// 递归子目录：dist/docs/commands/ 的陈旧页同样要清。
func staleSiteFiles(root, dir string, keep map[string]struct{}) ([]string, error) {
	base := filepath.Join(root, dir)
	if _, err := os.Stat(base); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", dir, err)
	}
	var stale []string
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".html") {
			return nil
		}
		if _, ok := keep[p]; !ok {
			stale = append(stale, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("遍历 %s 失败: %w", dir, err)
	}
	return stale, nil
}
```

- [ ] **步骤 4：运行测试验证通过**

```powershell
go test ./cmd/usm/ -v
```

预期：全部 PASS——新增 4 个通过，既有 TestWriteSiteRejectsTraversalDoc、TestWriteSiteRemovesStale 等不回归。

- [ ] **步骤 5：Commit**

写入临时文件 `$env:TEMP\opencode\msg2.txt`：

```
feat(usm): writeSite 放行一级文档子目录并递归清理陈旧页

validDocName 允许恰好一级 [a-z0-9-]+/ 前缀，
..、\、绝对路径、二级以上照旧拒绝；staleSiteFiles
改 WalkDir 递归，dist/docs/commands/ 陈旧页可清理。
```

```powershell
git add cmd/usm/site.go cmd/usm/site_test.go
git commit -F $env:TEMP\opencode\msg2.txt
```

---

## 任务 3：写入 11 个文档 md

**文件：**
- 创建：`docs/index.md`、`docs/commands/{add,list,info,rm,sync,project,build,cleanup,cli}.md`、`docs/dev/extending-sources.md`

- [ ] **步骤 1：docs/index.md**

````markdown
# userscript-console

GitHub 无服务器油猴脚本控制台：用 issue 评论驱动脚本注册表（registry.json），`usm` CLI 负责编译与发布 GitHub Pages 站点。本仓是**工具与文档**所在地；脚本数据（registry.json、源码、dist）由各使用方仓库持有。

> 站点命令文档页间的相对链接在 GitHub 网页浏览源码时失效——已知取舍，以发布站点为准。

## 命令文档

面板命令（issue 评论 `/cmd`）与 CLI 子命令的行为契约：

- [添加脚本](commands/add.html) —— `/add`
- [脚本清单](commands/list.html) —— `/list`
- [脚本详情](commands/info.html) —— `/info`
- [删除与复活](commands/rm.html) —— `/rm`
- [上游同步](commands/sync.html) —— `/sync`、`/sync-all`
- [项目投影](commands/project.html) —— `/project`
- [构建站点](commands/build.html) —— `/build`
- [清理面板](commands/cleanup.html) —— `/cleanup`
- [CLI 入口](commands/cli.html) —— `usm` 子命令汇总

## 开发者文档

- [扩展脚本源适配器](https://github.com/ACG-Q/userscript-console/blob/master/docs/dev/extending-sources.md)（仓库内 `docs/dev/`，不转换为站点页）
````

- [ ] **步骤 2：docs/commands/add.md**

````markdown
# add —— 添加脚本

## 定位

面板命令 `/add`：往账本添加自写脚本（评论附代码块）或远程脚本（给来源 URL，抓取元数据并入库）。

## 用法

```
/add <来源URL>
/add            ← 评论后附一个 ``` 代码块
```

- 来源 URL 由 `sources.Detect` 识别站点类型（greasyfork / userscript_zone / github_gist / direct 兜底）。
- 无 URL 且无代码块 → 失败「添加自写脚本需要提供代码块，请在评论中包含 ```...``` 代码块」。

## 参数

| 模式 | 输入 | 产物类型 |
|---|---|---|
| 远程 | `/add <URL>` | `synced`，自动 `Fetch` 元数据，`SyncEnabled=true` |
| 自写 | `/add` + 代码块 | `self`，从 `==UserScript==` 头解析 Name/Version/Author/Match/Grant |

## env（CLI 直跑时）

| 变量 | 说明 |
|---|---|
| `COMMENT_BODY` / `COMMENT_USER` / `ISSUE_NUMBER` | 面板上下文（`usm run-command`） |
| `REPO_OWNER` / `GH_REPO_OWNER` | 仓库 owner（缺省时部分回帖链接退化） |
| `AUTHOR_NAME`（默认 `usm`）/ `AUTHOR_NAMESPACE` | 自写脚本作者头缺失时的回填 |
| `USM_REGISTRY_SCHEMA` | 声明账本 schema，防新旧二进制混用 |

## 回帖示例

```
✅ 已添加脚本 "Foo" v1.2（来源: https://greasyfork.org/scripts/1234，ID: ab12cd）
✅ 已添加自写脚本 "Bar" v0.3（ID: self01）
```

## 幂等/边界语义

- **重复添加**：同 `SourceURL` 已存在 → 失败「脚本已存在（ID: %s），无需重复添加」，不覆盖。
- **复活（I-4）**：同 `SourceURL` 条目已被 `/rm` 软删除 → 不报错，恢复 `Deleted=false`、`Enabled=true` 并重新抓取，回帖「✅ 已复活脚本 %q（来源: %s）」。
- **自写头块缺失**：「无法解析脚本头，请确保代码包含完整的 ==UserScript== 头块」。
- 添加成功即写 `registry.json` + 源码 + `dist/` 分发产物。

## 代码指针

`internal/commands/add.go`、`internal/sources/`（Detect 与各适配器）。
````

- [ ] **步骤 3：docs/commands/list.md**

````markdown
# list —— 脚本清单

## 定位

面板命令 `/list`：列出全部活动脚本的速览表。

## 用法

```
/list
```

## 参数

无。

## env

无专属 env（作为面板命令时由 `usm run-command` 提供账本上下文）。

## 回帖示例

```
📋 活动脚本列表：

| ID | 名称 | 版本 | 类型 | 状态 |
|---|---|---|---|---|
| `ab12cd` | Foo | 1.2 | synced | 启用 |
| `self01` | Bar | 0.3 | self | 停用 |

共 2 个（另有 1 个已删除）
```

空账本时：「📭 暂无活动脚本，用 `/add <来源URL>` 或 `/add` + 代码块添加第一个脚本。」

## 幂等/边界语义

- **只列活动条目**：`Deleted=true` 的不进表，仅在尾部统计「（另有 M 个已删除）」（M=0 时省略）。
- 只读命令，不改账本。

## 代码指针

`internal/commands/list.go`。
````

- [ ] **步骤 4：docs/commands/info.md**

````markdown
# info —— 脚本详情

## 定位

面板命令 `/info`：按 id / 来源 URL / 精确名称查单个脚本的完整元数据。

## 用法

```
/info <id|来源URL|名称>
```

## 参数

| 位置参数 | 说明 |
|---|---|
| key | 精确匹配 id、`SourceURL` 或 `Name`；缺省报错「用法: /info <id|来源URL|名称>」 |

## env

无专属 env。

## 回帖示例

回帖为 Markdown 表：ID/名称/版本/类型/状态/描述/作者/命名空间/@match/@grant/来源/最后同步/创建时间/更新时间/Issue 链接/文档链接/（自动同步关闭时增行），随后「changelog（最近 5 条）」表。

## 幂等/边界语义

- **未找到**：「未找到脚本 %q，请用 /list 查看全部条目，或检查 ID/来源 URL/名称是否准确。」
- 状态三态：启用 / 停用 / 已删除（软删除）；changelog 超过 5 条只展示最近 5 条。
- 只读命令。

## 代码指针

`internal/commands/info.go`。
````

- [ ] **步骤 5：docs/commands/rm.md**

````markdown
# rm —— 软删除与复活契约

## 定位

面板命令 `/rm`：软删除脚本——条目保留（支持复活），源码与分发产物移除。

## 用法

```
/rm <id|名称|来源URL>
```

## 参数

| 位置参数 | 说明 |
|---|---|
| key | 同 `/info` 的匹配规则；缺省报错「用法: /rm <id|名称|来源URL>」 |

## env

无专属 env。

## 回帖示例

```
✅ 已软删除脚本 "Foo"（ID: ab12cd，支持 /add 复活）
```

## 幂等/边界语义

- **软删除（I-4）**：仅置 `Deleted=true` 并更新 `UpdatedAt`，条目保留在账本；`/list` 不再列出。
- **幂等**：已是删除状态 → 「脚本 %q 已是已删除状态（ID: %s）」，不重复处理。
- **复活**：同 `SourceURL` 再次 `/add` 即恢复（见 [add](add.html)）。
- **部分失败**：registry 已改但源码/分发产物移除失败 → 回帖「❌ 已软删除脚本 %q，但源文件失败: %v」（成功标记保留，下轮 doctor 可发现）。

## 代码指针

`internal/commands/rm.go`、`internal/script.RemoveSource/RemoveDist`。
````

- [ ] **步骤 6：docs/commands/sync.md**

````markdown
# sync —— 上游同步

## 定位

面板命令 `/sync`（单个）与 `/sync-all`（批量）：从 `SourceURL` 抓取最新代码更新账本与产物。`/sync all` 与 `/sync-all` 等价。

## 用法

```
/sync <id|名称|来源URL>
/sync-all          ← 等价 /sync all
```

## 参数

| 位置参数 | 说明 |
|---|---|
| key | 单个脚本的 id/名称/来源 URL；`all` 或缺省 → 批量模式 |

## env

无专属 env（批量抓取失败计入 warnings，不中断）。

## 回帖示例

```
✅ 已同步脚本 "Foo" v1.2 → v1.3
📭 所有脚本已是最新版本
📭 未同步任何脚本（1 个抓取失败）
📭 没有需要同步的脚本
```

## 幂等/边界语义

- **单个模式前置校验**（顺序即回帖顺序）：未找到 → 失败；`self` 脚本 →「脚本 %q 是自写脚本，无法同步」；缺 `SourceURL` → 失败；已停用 → 失败；上游版本 == 当前版本 → 「已是最新版本 v%s」（no-op）。
- **更新落库**：版本/描述/作者/Match/Grant/时间戳整体刷新，changelog 头部插入「同步更新 v旧 → v新」（保留最多 10 条），源码与分发产物同步写盘。
- **批量模式**：只同步 `synced` + 启用 + 未删除 + 有 `SourceURL` 的条目；单个失败跳过不中断；全部最新 → 幂等 no-op；有失败时结果带 warning「N 个脚本抓取失败，已跳过」。

## 代码指针

`internal/commands/sync.go`（`runSyncOne`/`runSyncAll`/`applySynced`）。
````

- [ ] **步骤 7：docs/commands/project.md**

````markdown
# project —— 项目投影

## 定位

面板命令 `/project` 与 CLI `usm project`：遍历账本，对账投影到 GitHub Issues / 版本帖——确保每个活跃脚本有配套 Issue（创建/更新/关闭）。

## 用法

```
/usm project            ← CLI
usm project --json      ← action.yml 模式
/project                ← 面板
```

## 参数

无位置参数；`--json` 输出 `{authorized, changed, result, warnings}`。

## env

| 变量 | 说明 |
|---|---|
| `GH_REPO_OWNER` | 仓库 owner（CLI 模式） |
| `GITHUB_REPOSITORY` / `GH_REPO` | `owner/repo` |
| `GITHUB_TOKEN` | GraphQL/REST 客户端；缺失 → 仅统计不操作 |
| `PAGES_BASE` | 投影内链的站点基址 |

## 回帖示例

```
📊 投影统计：

- 活跃脚本: 12
- 已删除: 3
- 无关联 Issue: 2

✅ 本次操作：
- 创建 Issue: 2
- 更新 Issue: 5
- 错误: 0
```

## 幂等/边界语义

- 对账是幂等的：重复投影只补差（已存在且一致的 Issue 不动）。
- **前置**：`GH_REPO_OWNER` 与 `GH_CLIENT` 都缺失 → 失败「project 命令需要配置 GH_REPO_OWNER 或 GH_CLIENT」。
- 未配置 GitHub API 时仅输出统计，回帖带「⚠️ GitHub API 未配置，仅输出统计」。

## 代码指针

`internal/commands/project.go`、`internal/projector/`。
````

- [ ] **步骤 8：docs/commands/build.md**

````markdown
# build —— 构建站点

## 定位

面板命令 `/build` 与 CLI `usm build`：把账本编译为整站产物 `dist/`（脚本副本 + scripts.json + 首页 + 详情页 + 命令归档页 + 文档页），供 GitHub Pages 发布。

## 用法

```
usm build                ← CLI（常配 PAGES_BASE）
usm build --json
/build                   ← 面板
```

## 参数

无位置参数；`--json` 时输出 action.yml 用 JSON。

## env

| 变量 | 说明 |
|---|---|
| `PAGES_BASE` | 站点基址（面板模式缺失 → 失败「build 命令需要配置 PAGES_BASE 环境变量」） |
| `USM_ROOT` / `--root` | 数据根（含 registry.json），默认当前目录 |
| `GITHUB_TOKEN` | 可选：抓取 discussions 供站点讨论区；缺失降级不失败 |

## 回帖示例

```
✅ 构建完成：

- 已构建: 12 个脚本
- 已跳过: 3 个（已删除）
- 站点页面: 24 个

📦 站点基址: https://<owner>.github.io/<repo>/dist/
```

## 幂等/边界语义

- **幂等**：相同输入第二次构建 `changed=false`（内容级 diff 后再写盘）。
- **`env.Site` 为 nil**（单测/无站点场景）：只产出 `dist/` 脚本副本，不产整站页面。
- 已删除脚本跳过构建；源码读取失败计入错误数不中断。
- 整站构建含 `dist/docs/`：顶层 `docs/*.md` + `docs/commands/*.md` 转换为 HTML（`docs/index.md` 缺失则整站不产 docs）。

## 代码指针

`internal/commands/build.go`、`cmd/usm/site.go`、`internal/pages/`。
````

- [ ] **步骤 9：docs/commands/cleanup.md**

````markdown
# cleanup —— 面板清理

## 定位

面板命令 `/cleanup` 与 CLI `usm cleanup`：归档命令面板历史评论到 `archive/commands.json`，`--apply` 时删除不再保留的评论。

## 用法

```
usm cleanup                    ← dry-run（默认）
usm cleanup --apply            ← 实际执行
usm cleanup --keep N           ← 每命令组保留最近 N 条（默认 10）
/cleanup [--apply] [--keep N]  ← 面板
```

## 参数

| 参数 | 说明 |
|---|---|
| `--apply` | 实际删除；缺失为 dry-run。面板不带参时 env `USM_APPLY=true` 同效 |
| `--keep N` | 每命令组保留最近 N 条（env `USM_KEEP`）；N<=0 视为默认 10 |

## env

| 变量 | 说明 |
|---|---|
| `GITHUB_TOKEN` | 拉取与删除评论 |
| `GITHUB_REPOSITORY` | `owner/repo` |
| `USM_APPLY` / `USM_KEEP` | 面板模式的标志等价物 |

## 回帖示例

```
🧹 cleanup 完成

脚本总数: 12
拉取评论: 48
已删除评论: 30
归档路径: archive/commands.json
归档命令组: 9
归档条目: 48

⚠️ 当前为 dry-run，添加 --apply 执行实际删除
```

## 幂等/边界语义

- **归档先落盘再删评论**：删除失败时下轮只补删除（幂等键 `command_id`，孤儿组用首条 result id）。
- dry-run 只统计不删；`--keep N` 与归档合并逻辑共用 `cleanup.MergeArchive`。
- 无 `GHClient` 或无面板 issue 上下文时只做归档统计。

## 代码指针

`internal/commands/cleanup.go`、`internal/cleanup/`。
````

- [ ] **步骤 10：docs/commands/cli.md**

````markdown
# cli —— usm 子命令汇总

## 定位

`usm` 是本仓的层 2 CLI 入口（GitHub Action `acg-q/userscript-console` 底层即它）。本文汇总顶层子命令；面板命令的详细语义见各自文档页。

## 用法

```
usm <command> [flags]

run-command    执行命令面板评论中的 /command（回帖语义）
project        registry → Issues/版本帖 对账投影
build          构建站点产物（dist/）
cleanup        归档并清理命令面板历史评论（默认 dry-run，--apply 才执行）
doctor         数据一致性自检（--check 供 CI）
snapshot       快照基线 <check|update>
version        打印版本与 registry schema 版本
help           帮助
```

全局约定：默认工作目录即数据根（含 registry.json）；`--root <dir>` 可显式指定（或 env `USM_ROOT`）。

## 子命令要点

### run-command

面板执行入口：解析 `COMMENT_BODY` 中的 `/cmd`，鉴权后执行并回帖。

| env | 说明 |
|---|---|
| `COMMENT_BODY` / `COMMENT_USER` / `ISSUE_NUMBER` | 必填：评论内容、作者、面板 issue 号 |
| `REPO_OWNER` / `GH_REPO_OWNER` | 仓库 owner |
| `PAGES_BASE` | 供 `/build` 等需要站点基址的命令 |
| `AUTHOR_NAME`（默认 `usm`）/ `AUTHOR_NAMESPACE` | 自写脚本作者回填 |
| `GITHUB_TOKEN` / `GITHUB_REPOSITORY` | 回帖与产物提交 |

### doctor

```
usm doctor [--check] [--json] [--root <dir>]
```

数据一致性自检（registry 与源码/分发产物双删对账）；`--check` 供 CI 失败即退出 1。

### build / cleanup / project

见各自的文档页（[build](build.html)、[cleanup](cleanup.html)、[project](project.html)）。

### snapshot

```
usm snapshot check     ← 校验 tests/snapshot 基线与当前代码一致
usm snapshot update    ← 重新生成基线
```

开发者工具：对固定输入跑整站构建、比对/更新基线目录。不读写账本。

### version / help

```
usm version     → usm <ver> + registry-schema-version <N>
usm help        → 帮助
```

## 退出码契约

| 码 | 含义 |
|---|---|
| 0 | 成功（含业务失败的结果文本） |
| 1 | 致命（数据损坏/网络不可恢复/schema 不匹配） |
| 2 | 用法错误（未知子命令/缺参数） |

## 幂等/边界语义

- `USM_REGISTRY_SCHEMA`：action.yml 声明依赖的 registry 格式，与二进制内置不符 → exit 1 拒跑（防新旧混用）；未设置跳过。
- `--json` 时所有子命令输出 `{authorized, changed, result, warnings}`（action.yml 全量走 jq）。

## 代码指针

`cmd/usm/main.go`。
````

- [ ] **步骤 11：docs/dev/extending-sources.md**

````markdown
# 扩展脚本源适配器

本文面向工具仓开发者：新增一个脚本来源站点（如新脚本站）需要做什么。`docs/dev/` 不转换为站点页，请在仓库内阅读。

## 三步走

### 1. 实现 Adapter 接口

`internal/sources`：

```go
type Adapter interface {
	Type() string
	MatchURL(rawurl string) bool
	Fetch(ctx context.Context, d Doer, rawurl string) (*Result, error)
}
```

- `Type()`：返回新的 SourceType 常量（在 sources.go 的常量块登记）。
- `MatchURL()`：用 `MatchHostSuffix(host, "example.org")` 判定 host 白名单——完全相等或以 `.` + pattern 结尾，两侧小写归一 + 去 FQDN 尾点。**必须**用它而非裸 `strings.HasSuffix`：后者会被 `evilgreasyfork.org`、`greasyfork.org.evil.com` 绕过。
- `Fetch()`：`HTTPGet` 拉页面（自带浏览器 UA/Accept/Accept-Language，缺 Accept-Language 会被 Greasy Fork 类站点 403），解析出 `Result{Name, Version, Description, Author, Match, Grant, Code, SourceType}`。禁止真实网络——HTTP 走注入的 `Doer`。

范例：`internal/sources/greasyfork.go`（`MatchURL` 即 `MatchHostSuffix(u.Hostname(), "greasyfork.org")`）。

### 2. init() 注册

```go
func init() { register(example{}{}) }
```

注册顺序不可依赖（文件按字母序初始化）——`MatchURL` 必须精确。

### 3. 测试锁死

`internal/sources/sources_test.go` 三件套：

1. **注册清单硬编码断言**：期望集合 `[]string{"greasyfork", "userscript_zone", "github_gist"}`（不含 direct），新增适配器忘注册/多余注册都红。
2. **fixture 回放**：`httptest` 假服务 + 仓库内 HTML fixture，断言 `Fetch` 解析结果；禁真实网络。
3. **host 白名单回归**：`MatchHostSuffix` 的正反例（子域命中、前缀伪造不命中、path 冒充不命中）。

## direct 适配器为何不注册

`direct` 是 `Detect` 的兜底（任意 http(s) URL 按裸脚本抓取）。它的 `MatchURL` 恒真——若注册会吞掉所有路由，让其他适配器永远匹配不到；文件按字母序初始化又使注册顺序不可依赖。因此「不注册」即区分：`Detect` 白名单无命中时才返回 `direct{}`。

## 明确禁止

- 在 `Detect` 或既有适配器里加 if/else 特判 URL。
- 绕过 `Doer` 注入直连网络。
- 引入 `golang.org/x/net` 等新依赖解析 HTML（优先 stdlib 正则/strings；确需时先过依赖评审）。

## 代码指针

`internal/sources/sources.go`（接口/注册/Detect/MatchHostSuffix/HTTPGet）、`greasyfork.go`（范例）、`sources_test.go`（锁死测试）。
````

- [ ] **步骤 12：Commit**

```powershell
git add docs/
git commit -m "docs: 新增命令文档与扩展开发文档"
```

---

## 任务 4：Pages workflow + 本地构建冒烟

**文件：**
- 创建：`.github/workflows/pages.yml`

- [ ] **步骤 1：写入 pages.yml**

````yaml
name: Pages

on:
  push:
    branches: [master]
  workflow_dispatch:

permissions:
  contents: read
  pages: write
  id-token: write

concurrency:
  group: pages
  cancel-in-progress: false

jobs:
  build:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - name: 生成空账本（工具仓不持有数据，CI 现场生成，不落仓）
        run: echo '{"schema":1,"scripts":[]}' > registry.json
      - name: 构建站点（go run 自举：文档站永远反映当前 master）
        env:
          PAGES_BASE: https://acg-q.github.io/userscript-console
        run: go run ./cmd/usm build
      - uses: actions/configure-pages@v6
      - uses: actions/upload-pages-artifact@v4
        with:
          path: ./dist
  deploy:
    needs: build
    runs-on: ubuntu-latest
    timeout-minutes: 5
    environment:
      name: github-pages
      url: ${{ steps.d.outputs.page_url }}
    steps:
      - id: d
        uses: actions/deploy-pages@v5
````

- [ ] **步骤 2：本地构建冒烟（PowerShell，工具仓根）**

```powershell
Set-Content -Path registry.json -Value '{"schema":1,"scripts":[]}' -NoNewline
go run ./cmd/usm build
Test-Path dist/docs/index.html
Test-Path dist/docs/commands/add.html
Test-Path dist/docs/commands/cli.html
Test-Path dist/docs/commands/cleanup.html
Test-Path dist/docs/dev
```

预期：build 成功；4 个 Test-Path 中前三个 `True`、`dist/docs/dev` 为 `False`（dev 不转换）。若 `dist/docs/dev` 意外存在即 bug，停下检查任务 1。

（`registry.json` 与 `dist/` 均已被 `.gitignore` 忽略，冒烟产物不入库；确认 `git status` 干净。）

- [ ] **步骤 3：快照基线不受影响验证**

```powershell
go run ./cmd/usm snapshot check
```

预期：`快照一致`——快照 Out 指向 `tests/snapshot/site`，其 docsDir 不存在，仓库根新增 docs/ 不改基线。若不一致，停下报告（说明转换逻辑波及了快照构建，需人工审 diff 再 `snapshot update`）。

- [ ] **步骤 4：Commit**

```powershell
git add .github/workflows/pages.yml
git commit -m "ci: 新增自举 Pages 发布 workflow"
```

---

## 任务 5：全量门禁 + 推送 + Pages 线上验证

- [ ] **步骤 1：全量门禁（PowerShell，工具仓根）**

```powershell
gofmt -l .
go vet ./...
go test ./... -cover
$env:GOROOT=(go env GOROOT); golangci-lint run ./...
go run ./cmd/usm snapshot check
```

预期：gofmt 无输出；vet 静默；覆盖率 ≥ 90%；lint 0 issues；快照一致。

- [ ] **步骤 2：推送 master**

```powershell
git push origin master
```

- [ ] **步骤 3：手动开启 Pages（一次性，需要仓库权限）**

仓库 Settings → Pages → Build and deployment → Source = **GitHub Actions**。若此前已是则跳过。

- [ ] **步骤 4：验证 workflow 与线上**

- Actions 页 `Pages` workflow 本次 run 成功（build + deploy 两 job 均绿）。
- 浏览器/PowerShell 验证：

```powershell
(Invoke-WebRequest -Uri 'https://acg-q.github.io/userscript-console/' -UseBasicParsing).StatusCode
(Invoke-WebRequest -Uri 'https://acg-q.github.io/userscript-console/docs/' -UseBasicParsing).StatusCode
(Invoke-WebRequest -Uri 'https://acg-q.github.io/userscript-console/docs/commands/add.html' -UseBasicParsing).StatusCode
```

预期：均 200；`docs/` 首页含"文档"导航与 9 条命令链接；命令页间 TOC 互跳正常；`docs/dev/extending-sources.html` 404（预期不转换）。

- [ ] **步骤 5：确认边界未破**

```powershell
git log --oneline -5
git show --stat HEAD
git show --stat HEAD~1
git show --stat HEAD~2
```

预期：三个提交的文件清单中**无 registry.json**（CI 现场生成，仅存在于 runner）。

---

## 任务 6：userscripts 清理

**文件：**
- 删除：`userscripts/docs/`（git rm -r：index.md、reviews/、superpowers/）
- 修改：`userscripts/SPEC-DATA.md:108-118`（§3 文档归属）

- [ ] **步骤 1：整删 docs/**

```powershell
cd C:\Users\LiuJi\Desktop\project-06\userscripts
git rm -r docs
```

预期：staged 删除 index.md、reviews/、superpowers/ 下全部跟踪文件（约 7 个）；未跟踪残留用 `git status` 确认后手动 `Remove-Item -Recurse docs`（如有）。

- [ ] **步骤 2：SPEC-DATA.md 文档归属表标注**

将 `userscripts/SPEC-DATA.md` :108-118 的 §3 整节替换为：

```markdown
## 3. 文档归属（已收敛，2026-10-09）

| 文档 | 归属 | 说明 |
|---|---|---|
| ~~`docs/index.md`~~ | 已移除（2026-10-09） | `docs/` 整目录删除；站点无 `dist/docs` → `HasDocs=false` → 导航自动隐藏「文档」（防死链契约） |
| ~~`docs/commands/*.md`~~ | 已移除（2026-10-09） | 命令文档落在工具仓 `userscript-console/docs/commands/` |
| ~~`docs/design.md`~~ | 已移除（2026-10-09） | 系统设计在工具仓历史规格 |
| ~~`docs/code-review-2026-10-03.md`~~ | 已移除（2026-10-09） | 审查档案在工具仓历史规格 |
| `README.md` | **本仓** | 面向脚本用户：安装、如何在 Issue #1 发命令、脚本列表；命令速查**链接**到工具仓 README |

> 本表其余历史清单（BASELINE.md / CUTOVER.md / PLAN.md）原样保留，不随本次清理变更。
```

- [ ] **步骤 3：Commit 与推送**

写入临时文件 `$env:TEMP\opencode\msg6.txt`：

```
docs: 移除 docs/ 目录（文档归属收敛至工具仓）

docs/index.md、reviews/、superpowers/ 整删；SPEC-DATA §3
标注已移除。站点侧零改动：无 dist/docs → HasDocs=false
→ 导航自动隐藏「文档」。
```

```powershell
git add -A
git commit -F $env:TEMP\opencode\msg6.txt
git push origin master
```

- [ ] **步骤 4：验证双仓状态**

- userscripts Actions：Validate（userscripts.yml）与 Deploy 双绿。
- 线上 `https://acg-q.github.io/userscripts/` 200，主页导航**无「文档」项**，无死链。
- `Test-Path userscripts/docs` → `False`。

---

## 任务 7：最终清理 superpowers（最后一步）

**文件：**
- 删除：`userscript-console/docs/superpowers/`（含本计划与已批准规格，不留归档——用户明确要求）

> 注意：本任务会删除计划文件自身。执行到此步时所有内容已全部实现并验收，无需再引用；子代理应在开始本任务前已读完全部任务内容。

- [ ] **步骤 1：确认前序验收全部通过**

回看任务 5/6 的验证结果：门禁绿、Pages 线上 200、userscripts 双绿。任一未过，不得进入本任务。

- [ ] **步骤 2：整删并推送（工具仓根）**

```powershell
cd C:\Users\LiuJi\Desktop\project-06\userscript-console
git rm -r docs/superpowers
```

写入临时文件 `$env:TEMP\opencode\msg7.txt`：

```
docs: 清理内部工作档案（最终）

specs/plans 随文档重构验收完成整体删除，不留归档。
```

```powershell
git add -A
git commit -F $env:TEMP\opencode\msg7.txt
git push origin master
```

- [ ] **步骤 3：终态确认**

```powershell
Test-Path docs/superpowers           # False
go run ./cmd/usm snapshot check      # 快照一致（docs/superpowers 本就不转换，构建不受影响）
```

- Actions `Pages` workflow 因 master 推送再跑一轮：成功；`https://acg-q.github.io/userscript-console/docs/` 仍 200。

- 全部验收标准（规格 §7 六条）达成，任务结束。

---

## 验收标准（对照规格 §7，执行完全部任务后逐条核对）

1. `usm build` 产出 `dist/docs/index.html` 与 `dist/docs/commands/*.html`（9 命令 + index），`dev/`、`superpowers/` 不出现在产物。
2. 站点 `https://acg-q.github.io/userscript-console/docs/` 可访问，导航含「文档」，命令页 TOC 互跳正常。
3. 工具仓 git 历史无 `registry.json`。
4. userscripts 仓 `docs/` 不存在，站点无「文档」导航，Validate/Deploy 绿。
5. 全量门禁绿（覆盖率 ≥ 90%、lint 0 issues、快照一致）。
6. `docs/superpowers/` 已删除。
