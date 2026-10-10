package main

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/acg-q/userscript-console/internal/issuepage"
	"github.com/acg-q/userscript-console/internal/pages"
	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/snapshot"
)

// 快照生成器（snapshot.RegisterDefault）：输入 = tests/corpus/inputs/registry.json，
// 输出 = 投影标题/正文/墓碑/版本帖 + registry 规范形态 + 站点整页（site/）。

const corpusPath = "tests/corpus/inputs/registry.json"

// 站点快照的确定性参数（相对时间/链接/版本都随这些值变化，必须钉死）。
var (
	siteSnapshotBase = "https://acg-q.github.io/userscript-console"
	siteSnapshotNow  = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	// 页脚/会随 buildinfo.Version() 漂移，快照钉死固定值，
	// 否则每次发版都会把整站快照 diff 出来。
	siteSnapshotVersion = "0.0.0"
)

func init() {
	snapshot.RegisterDefault(buildSnapshotFiles)
}

func buildSnapshotFiles() (map[string]string, error) {
	reg, err := registry.Load(corpusPath)
	if err != nil {
		return nil, fmt.Errorf("加载语料 %s: %w", corpusPath, err)
	}
	files := map[string]string{}

	// registry 规范形态（I-2 round-trip 锁）
	canon, err := reg.Bytes()
	if err != nil {
		return nil, err
	}
	files["registry/canonical.json"] = string(canon)

	// 投影产物（按 id 排序保证稳定）
	scripts := make([]registry.Script, len(reg.Scripts))
	copy(scripts, reg.Scripts)
	sort.Slice(scripts, func(i, j int) bool { return scripts[i].ID < scripts[j].ID })

	for _, s := range scripts {
		base := path.Join("issue", s.ID)
		files[base+"-title.txt"] = issuepage.BuildTitle(s)
		files[base+"-body.md"] = issuepage.BuildBody(s)
		if s.Deleted {
			files[base+"-tombstone-title.txt"] = issuepage.TombstoneTitle(s)
			files[base+"-tombstone-body.md"] = issuepage.TombstoneBody(s)
		}
		if len(s.Discussions) > 0 {
			v := s.Discussions[len(s.Discussions)-1].Version // 账本末条 = 当前版本帖
			files[base+"-discussion.md"] = issuepage.DiscussionBody(s, v)
		}
	}

	site, err := buildSiteSnapshot(reg)
	if err != nil {
		return nil, err
	}
	for name, content := range site {
		files[name] = content
	}
	return files, nil
}

// buildSiteSnapshot 生成整站快照（index/scripts.json/详情页/告警）。
//
// Out 指到 tests/snapshot/site 只是为了让 archive 探测路径
// （pages.renderCommands 读 <Out 的父目录>/archive/commands.json）落在
// 永远不存在的 tests/snapshot/archive/ 下 —— 否则本机残留的 archive/
// 会让快照因环境而异。Build 本身不写盘，这里的 Out 只影响该探测。
func buildSiteSnapshot(reg *registry.Registry) (map[string]string, error) {
	out, err := pages.Build(reg, pages.Options{
		Out:       filepath.Join("tests", "snapshot", "site"),
		PagesBase: siteSnapshotBase,
		Version:   siteSnapshotVersion,
		Now:       siteSnapshotNow,
	}, pages.Data{})
	if err != nil {
		return nil, fmt.Errorf("生成站点快照: %w", err)
	}

	files := map[string]string{
		"site/index.html":         out.IndexHTML,
		"site/scripts.json":       out.ScriptsJSON,
		"site/build-warnings.txt": strings.Join(out.BuildWarnings, "\n"),
	}
	for id, html := range out.DetailHTMLs {
		files[path.Join("site", "scripts", id+".html")] = html
	}
	for n, html := range out.CommandPages {
		files[fmt.Sprintf("site/commands/page-%d.html", n)] = html
	}
	if out.CommandsIndex != "" {
		files["site/commands/index.html"] = out.CommandsIndex
	}
	return files, nil
}
