package main

import (
	"fmt"
	"path"
	"sort"

	"github.com/acg-q/userscript-console/internal/issuepage"
	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/snapshot"
)

// 快照生成器（snapshot.RegisterDefault）：输入 = tests/corpus/inputs/registry.json，
// 输出 = 投影标题/正文/墓碑/版本帖 + registry 规范形态。
// 站点整页快照在 pages 包接线后追加（site/ 目录）。

const corpusPath = "tests/corpus/inputs/registry.json"

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
	return files, nil
}
