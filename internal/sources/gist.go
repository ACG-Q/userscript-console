package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/acg-q/userscript-console/internal/meta"
)

func init() { register(gistAdapter{}) }

// gistAdapter GitHub Gist：页面 URL 推导 gist id → GitHub API 取文件内容。
// 宽松策略：头解析失败只填 Code 不报错（gist 内容不保证是用户脚本）。
type gistAdapter struct{}

func (gistAdapter) Type() string { return TypeGist }

var gistHosts = []string{"gist.github.com", "api.github.com"}

func (gistAdapter) MatchURL(rawurl string) bool {
	u, err := url.Parse(rawurl)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := u.Hostname()
	for _, p := range gistHosts {
		if MatchHostSuffix(host, p) {
			return true
		}
	}
	return false
}

// gistID 从两种形态推导 id：
//   - gist.github.com/<user>/<id> → 取第 2 段；
//   - api.github.com/gists/<id>   → 取 gists 后一段。
func gistID(rawurl string) (string, bool) {
	u, err := url.Parse(rawurl)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	seg := strings.Split(strings.Trim(u.Path, "/"), "/")
	host := u.Hostname()
	switch {
	case MatchHostSuffix(host, "gist.github.com"):
		if len(seg) >= 2 && seg[1] != "" {
			return seg[1], true
		}
	case MatchHostSuffix(host, "api.github.com"):
		if len(seg) >= 2 && seg[0] == "gists" && seg[1] != "" {
			return seg[1], true
		}
	}
	return "", false
}

// gistFile gist API files 条目（只取需要的字段）。
type gistFile struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// pickGistFile 按键排序选文件：*.user.js 优先，否则首个（键序稳定保证确定性）。
func pickGistFile(files map[string]gistFile) (gistFile, bool) {
	if len(files) == 0 {
		return gistFile{}, false
	}
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.HasSuffix(strings.ToLower(k), ".user.js") {
			return files[k], true
		}
	}
	return files[keys[0]], true
}

func (gistAdapter) Fetch(ctx context.Context, d Doer, rawurl string) (*Result, error) {
	id, ok := gistID(rawurl)
	if !ok {
		return nil, fmt.Errorf("github_gist: 无法从 URL 推导 gist id: %s", rawurl)
	}
	api := "https://api.github.com/gists/" + id
	body, err := HTTPGet(ctx, d, api)
	if err != nil {
		return nil, fmt.Errorf("github_gist: 请求失败 %s: %w", api, err)
	}
	var payload struct {
		Files map[string]gistFile `json:"files"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("github_gist: API 响应 JSON 解析失败 %s: %w", api, err)
	}
	f, ok := pickGistFile(payload.Files)
	if !ok {
		return nil, fmt.Errorf("github_gist: gist %s 不含任何文件", id)
	}

	// 宽松策略：头解析失败只填 Code，不报错。
	res := &Result{Code: f.Content, SourceType: TypeGist}
	if h, ok := meta.Parse(f.Content); ok {
		res.Name = h.Name
		res.Version = h.Version
		res.Description = h.Description
		res.Author = h.Author
		res.Match = h.Match
		res.Grant = h.Grant
	}
	return res, nil
}
