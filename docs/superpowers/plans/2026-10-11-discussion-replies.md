# 版本帖多层评论（replies）显示 实现计划

> **面向 AI 代理的工作者：** 用户偏好内联执行（executing-plans，勿用子代理）。步骤使用复选框（`- [ ]`）跟踪进度。

**目标：** 脚本详情页完整显示版本帖嵌套回复；badge 回复数含回复；SSR 降级同构。

**架构：** GraphQL 查询内嵌 `replies(first:100)` → `Comment.Replies` 递归结构 → `site.go`/payload/SSR 模板三路映射；前端 JS 零改动（已消费 `c.replies`）。

**技术栈：** Go 标准库 + httptest 假 GitHub + Go template 递归；TDD。

**依据规格：** `docs/superpowers/specs/2026-10-11-discussion-replies-design.md`

---

### 任务 1：github 数据层带 replies

**文件：**
- 修改：`internal/github/queries.go:133-152`（DISCUSSION_NODE_QUERY）
- 修改：`internal/github/helpers.go:25-30`（Comment）、`:83-101`（gqlComment/toComment）
- 测试：`internal/github/github_test.go`（TestDiscussion系列 内新增子测试）

- [ ] **步骤 1：新增失败测试**

`github_test.go` 在 `TestDiscussion系列` 的「评论分页」子测试之后追加：

```go
	t.Run("嵌套回复", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"node": map[string]any{
			"__typename": "Discussion", "id": "D_1",
			"comments": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				"nodes": []any{map[string]any{
					"id": "DCM_1", "author": map[string]any{"login": "u1"}, "body": "顶层", "createdAt": "c1",
					"replies": map[string]any{"nodes": []any{
						map[string]any{"id": "DR_1", "author": map[string]any{"login": "u2"}, "body": "回复1", "createdAt": "c2"},
						map[string]any{"id": "DR_2", "body": "回复2-无作者", "createdAt": "c3"},
					}},
				}},
			},
		}}), nil}}}
		c := newTestClient(t, d)
		th, err := c.DiscThread(context.Background(), "D_1")
		if err != nil || len(th.Comments) != 1 {
			t.Fatalf("comments=%v err=%v", th.Comments, err)
		}
		rs := th.Comments[0].Replies
		if len(rs) != 2 || rs[0].NodeID != "DR_1" || rs[0].Author != "u2" || rs[0].Body != "回复1" {
			t.Errorf("回复解析错误: %+v", rs)
		}
		if rs[1].Author != "" || rs[1].Body != "回复2-无作者" {
			t.Errorf("无作者回复应留空: %+v", rs[1])
		}
	})
	t.Run("回复缺id报错", func(t *testing.T) {
		d := &fakeDoer{t: t, resps: []resp{{200, okBody(t, map[string]any{"node": map[string]any{
			"__typename": "Discussion", "id": "D_1",
			"comments": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				"nodes": []any{map[string]any{
					"id": "DCM_1", "body": "顶层", "createdAt": "c1",
					"replies": map[string]any{"nodes": []any{map[string]any{"body": "无id回复", "createdAt": "c2"}}},
				}},
			},
		}}), nil}}}
		c := newTestClient(t, d)
		if _, err := c.DiscThread(context.Background(), "D_1"); err == nil {
			t.Fatal("回复缺 id 应报错")
		}
	})
}
```

（注意：插在 `TestDiscussion系列` 函数体的结尾大括号之前，即 619 行 `}` 之前。）

- [ ] **步骤 2：运行确认失败**

运行：`go test ./internal/github/ -run 'TestDiscussion系列' -count=1`
预期：FAIL（Replies 字段不存在 → 编译错误，属预期失败信号）

- [ ] **步骤 3：改实现**

`queries.go` DISCUSSION_NODE_QUERY 的 nodes 行：

```graphql
        nodes { id author { login } body createdAt replies(first: 100) { nodes { id author { login } body createdAt } } }
```

`helpers.go`：

```go
type Comment struct {
	NodeID    string
	Author    string
	Body      string
	CreatedAt string
	Replies   []Comment
}
```

```go
type gqlComment struct {
	ID     string `json:"id"`
	Author *struct {
		Login string `json:"login"`
	} `json:"author"`
	Body      string        `json:"body"`
	CreatedAt string        `json:"createdAt"`
	Replies   []gqlComment `json:"replies"`
}

func (g gqlComment) toComment() (Comment, error) {
	if g.ID == "" {
		return Comment{}, errors.New("github: comment 缺 id 字段")
	}
	c := Comment{NodeID: g.ID, Body: g.Body, CreatedAt: g.CreatedAt}
	if g.Author != nil {
		c.Author = g.Author.Login
	}
	for _, r := range g.Replies {
		rc, err := r.toComment()
		if err != nil {
			return Comment{}, err
		}
		c.Replies = append(c.Replies, rc)
	}
	return c, nil
}
```

注意：`gqlComment` 同时被 Issue 评论路径（helpers.go:299 附近）复用；Issue 评论响应无 `replies` 字段 → JSON 解码为 nil，不影响。

- [ ] **步骤 4：运行 github 全测试**

运行：`go test ./internal/github/ -count=1`
预期：PASS（含固定查询文档回归锁——该锁检查 `nodes { id author { login } body createdAt }` 子串？若锁内容含旧 nodes 字符串需同步更新，见步骤 4a）

- [ ] **步骤 4a：同步查询回归锁**

运行如报 `Test固定查询文档回归锁` 失败：grep `DISCUSSION_NODE_QUERY` 在 `github_test.go` 的锁 map 条目，将期望串更新为含 `replies(first: 100)` 的新 nodes 行。

- [ ] **步骤 5：Commit**

```bash
git add internal/github/queries.go internal/github/helpers.go internal/github/github_test.go
git commit -m "feat(github): 版本帖查询内嵌 replies 嵌套回复"
```

---

### 任务 2：pages 映射、ReplyCount 口径与 SSR/payload

**文件：**
- 修改：`internal/pages/pages.go:94-99`（Comment）、`:415-446`（cmtView/discPayload*）、`:643`、`:790-828`
- 修改：`internal/pages/templates/assets.tmpl:821`（cmt 模板）
- 测试：`internal/pages/pages_test.go`

- [ ] **步骤 1：新增失败测试**

`pages_test.go` 追加：

```go
// TestBuildDetailReplies 嵌套回复进 payload 与 SSR；ReplyCount 含回复（spec）。
func TestBuildDetailReplies(t *testing.T) {
	reg := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{{
		ID: "s1", Type: registry.TypeSelf, Name: "脚本", Version: "1.0.0", Enabled: true,
		Match: []string{"*://*/*"},
		Issue: &registry.IssueRef{Number: 1, NodeID: "I_1", URL: "https://github.com/o/r/issues/1"},
		Discussions: []registry.DiscussionEntry{{Version: "1.0.0", NodeID: "D_1",
			URL: "https://github.com/o/r/discussions/1", CreatedAt: "2026-01-01T00:00:00Z"}},
	}}}
	data := Data{Discussions: map[string]Thread{"D_1": {
		Comments: []Comment{
			{Author: "u1", Body: "顶层", CreatedAt: "2026-01-01T00:00:00Z",
				Replies: []Comment{{Author: "u2", Body: "回复", CreatedAt: "2026-01-02T00:00:00Z"}}},
			{Author: "u3", Body: "另一条", CreatedAt: "2026-01-03T00:00:00Z"},
		},
	}}}
	out, err := Build(reg, Options{Out: t.TempDir(), Now: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)}, data)
	if err != nil {
		t.Fatal(err)
	}
	html := out.DetailHTMLs["s1"]
	if !strings.Contains(html, "cmt-replies") || !strings.Contains(html, "回复") {
		t.Errorf("SSR 应含嵌套回复结构:\n%s", html)
	}
	if strings.Count(html, "条回复") == 0 {
		t.Error("应有回复数 badge")
	}
	// badge 口径：2 顶层 + 1 回复 = 3
	if !strings.Contains(html, "3 条回复") {
		t.Errorf("ReplyCount 应为 3（含回复）:\n%s", html)
	}
	// payload 含 replies
	var payload []struct {
		ReplyCount int `json:"reply_count"`
		Comments   []struct {
			Body    string `json:"body"`
			Replies []struct {
				Body string `json:"body"`
			} `json:"replies"`
		} `json:"comments"`
	}
	if err := json.Unmarshal([]byte(extractDiscJSON(t, html)), &payload); err != nil {
		t.Fatal(err)
	}
	if payload[0].ReplyCount != 3 || len(payload[0].Comments[0].Replies) != 1 ||
		payload[0].Comments[0].Replies[0].Body != "回复" {
		t.Errorf("payload 错误: %+v", payload[0])
	}
}
```

`extractDiscJSON` 若测试包无此辅助，用内联正则从 html 提取 `<script type="application/json" id="discData">…</script>` 内容（strings.Index 截取即可）。

- [ ] **步骤 2：运行确认失败**

运行：`go test ./internal/pages/ -run TestBuildDetailReplies -count=1`
预期：FAIL（ReplyCount=2、payload 无 replies）

- [ ] **步骤 3：改实现**

`pages.go`：

```go
type Comment struct {
	Author    string
	Body      string
	CreatedAt string
	Replies   []Comment
}
```

```go
type cmtView struct {
	Author  string
	Time    string
	Body    string
	Replies []cmtView
}
```

```go
type discPayloadComment struct {
	Author  string                `json:"author"`
	Time    string                `json:"time"`
	Body    string                `json:"body"`
	Replies []discPayloadComment  `json:"replies,omitempty"`
}
```

卡片 ReplyCount（:643）改为：

```go
		c.ReplyCount += len(th.Comments)
		for _, cm := range th.Comments {
			c.ReplyCount += len(cm.Replies)
		}
```

详情构建（:790-828）改为递归：

```go
		p := discPostView{
			Label:      label,
			Version:    dsc.Version,
			Number:     dsc.Number,
			URL:        safeURL(dsc.URL),
			Answered:   th.HasAnswer,
			ReplyCount: countComments(th.Comments),
		}
		for _, c := range th.Comments {
			p.Comments = append(p.Comments, newCmtView(c, opts.Now))
		}
		d.Posts = append(d.Posts, p)
```

新增辅助（放在 cmtView 定义附近）：

```go
// countComments 顶层评论 + 全部回复数（badge 口径，spec §2）。
func countComments(cs []Comment) int {
	n := len(cs)
	for _, c := range cs {
		n += len(c.Replies)
	}
	return n
}

func newCmtView(c Comment, now time.Time) cmtView {
	who := c.Author
	if who == "" {
		who = "未知用户"
	}
	v := cmtView{
		Author: who,
		Time:   times.RelativeTime(c.CreatedAt, now),
		Body:   times.Clip(c.Body, 400),
	}
	for _, r := range c.Replies {
		v.Replies = append(v.Replies, newCmtView(r, now))
	}
	return v
}

func newPayloadComment(c cmtView) discPayloadComment {
	p := discPayloadComment{Author: c.Author, Time: c.Time, Body: c.Body}
	for _, r := range c.Replies {
		p.Replies = append(p.Replies, newPayloadComment(r))
	}
	return p
}
```

payload 构建（:824-826）改为：

```go
			for _, c := range p.Comments {
				dp.Comments = append(dp.Comments, newPayloadComment(c))
			}
```

`assets.tmpl:821` cmt 模板改为自递归：

```
{{define "cmt"}}<div class="cmt"><span class="cmt-who"><b>{{.Author}}</b></span><time>{{.Time}}</time><p>{{.Body}}</p>{{if .Replies}}<div class="cmt-replies">{{range .Replies}}{{template "cmt" .}}{{end}}</div>{{end}}</div>{{end}}
```

- [ ] **步骤 4：运行 pages 全测试**

运行：`go test ./internal/pages/ -count=1`
预期：PASS。若既有断言 `payload[0].ReplyCount != 1`（pages_test.go:981 附近）失败，检查其语料是否含回复；语料无回复则 ReplyCount 不变，应仍通过。

- [ ] **步骤 5：Commit**

```bash
git add internal/pages/pages.go internal/pages/templates/assets.tmpl internal/pages/pages_test.go
git commit -m "feat(pages): 详情页嵌套回复渲染与回复数口径"
```

---

### 任务 3：site.go 映射带 Replies

**文件：**
- 修改：`cmd/usm/site.go:97-108`
- 测试：`cmd/usm/site_test.go`

- [ ] **步骤 1：新增失败测试**

`site_test.go` 的 `TestSiteFetchDataWithGH` 中 fake 语料改为带回复并断言：

```go
			"DSC_OK": {
				Comments: []github.Comment{{
					Author: "alice", Body: "发布说明", CreatedAt: "2026-01-01T00:00:00Z",
					Replies: []github.Comment{{Author: "bob", Body: "收到", CreatedAt: "2026-01-02T00:00:00Z"}},
				}},
				HasAnswer: true,
			},
```

断言段（:70-73 之后）追加：

```go
	if len(th.Comments[0].Replies) != 1 || th.Comments[0].Replies[0].Author != "bob" {
		t.Errorf("回复应递归进 Data, got %+v", th.Comments[0])
	}
```

- [ ] **步骤 2：运行确认失败**

运行：`go test ./cmd/usm/ -run TestSiteFetchDataWithGH -count=1`
预期：FAIL（site.go 未映射 Replies → len=0）

- [ ] **步骤 3：改实现**

`site.go:97-108` 映射改递归：

```go
			data.Discussions[d.NodeID] = pages.Thread{
				Comments:  mapComments(th.Comments),
				HasAnswer: th.HasAnswer,
			}
```

新增：

```go
func mapComments(cs []github.Comment) []pages.Comment {
	out := make([]pages.Comment, 0, len(cs))
	for _, c := range cs {
		out = append(out, pages.Comment{
			Author:    c.Author,
			Body:      c.Body,
			CreatedAt: c.CreatedAt,
			Replies:   mapComments(c.Replies),
		})
	}
	return out
}
```

- [ ] **步骤 4：运行确认通过**

运行：`go test ./cmd/usm/ -count=1`
预期：PASS

- [ ] **步骤 5：Commit**

```bash
git add cmd/usm/site.go cmd/usm/site_test.go
git commit -m "feat(usm): site 抓取递归映射版本帖回复"
```

---

### 任务 4：门禁、快照与发版 v1.4.3

- [ ] **步骤 1：全量门禁**

```powershell
gofmt -l .
go vet ./...
$lint = Join-Path (go env GOPATH) "bin\golangci-lint.exe"; & $lint run
go test ./... -count=1 -covermode=atomic -coverprofile="cover.out"
go tool cover "-func=cover.out" | Select-Object -Last 1
go run ./cmd/usm snapshot update
go run ./cmd/usm snapshot check
go build ./...
```
预期：格式/lint 干净、全 PASS、覆盖 ≥90%、快照 update 后 diff 仅回复相关内容且 check 通过、build 通过。gofumpt 有报再 `& (Join-Path (go env GOPATH) "bin\gofumpt.exe") -w <文件>`。

- [ ] **步骤 2：Commit + 发版**

```bash
git add -A
git commit -m "chore(release): v1.4.3 快照基线"
git push origin master
git tag v1.4.3
git push origin v1.4.3
```

- [ ] **步骤 3：确认 CI success 且 v1 major 更新**

轮询 actions runs；`git ref tags/v1` 指向新 master。

---

### 任务 5：内容仓部署验收

- [ ] **步骤 1：触发内容仓 Pages 部署**

内容仓 registry 无变更 → 上次 Deploy 是 skipped。空 commit 触发：

```powershell
git -C C:\Users\LiuJi\Desktop\project-06\userscripts pull
git -C C:\Users\LiuJi\Desktop\project-06\userscripts commit --allow-empty -m "chore: redeploy pages for discussion replies"
git -C C:\Users\LiuJi\Desktop\project-06\userscripts push
```

- [ ] **步骤 2：等 Deploy success 后核验线上页面**

`https://acg-q.github.io/userscripts/scripts/39280503d459.html`：
- 「测试」下有 3 条嵌套回复（测试2/测试3/测试4，缩进结构）
- badge 显示「3 条回复」（2 顶层 + …注意 discussion #3 实际是 2 顶层（测试、测试5）+ 3 回复 = 5 条回复；以实际数据为准）

- [ ] **步骤 3：汇报**

---

## 计划自检

- 规格覆盖：§1→任务 1；§2→任务 2/3；§3→任务 2（模板）；§测试→各任务；§发布验收→任务 4/5。无遗漏。
- 占位符：无；extractDiscJSON 给出内联截取方案。
- 类型一致：`Comment.Replies`/`cmtView.Replies`/`discPayloadComment.Replies`/`mapComments`/`newCmtView`/`newPayloadComment`/`countComments` 签名均已定义。
