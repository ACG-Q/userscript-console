package projector

import (
	"context"
	"testing"

	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/registry"
)

const categoriesResp = `{"data":{"repository":{"discussionCategories":{"nodes":[` +
	`{"id":"DC_1","name":"Q&A","slug":"q-a"},` +
	`{"id":"DC_2","name":"一般","slug":"general"}]}}}}`

// repoIDResp 复用 reconcile_test.go 的定义（同包不重复声明）。

const createDiscResp = `{"data":{"createDiscussion":{"discussion":{"id":"D_1","number":7,` +
	`"title":"T","body":"B","url":"https://github.com/o/r/discussions/7",` +
	`"createdAt":"2026-10-10T00:00:00Z"}}}}`

// publishScript 构造用于发帖的脚本：version 为当前版本，ledger 为已发帖账本。
func publishScript(id, version string, ledger []registry.DiscussionEntry) registry.Script {
	return registry.Script{
		ID: id, Name: "脚本" + id, Version: version, Type: registry.TypeSelf, Enabled: true,
		Issue:       &registry.IssueRef{Number: 1, NodeID: "I_1", URL: "https://github.com/o/r/issues/1"},
		Discussions: ledger,
	}
}

// TestPublishEmptyLedgerPosts 空账本 → 分类+repoID+创建，追加账本 posted=1。
func TestPublishEmptyLedgerPosts(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{categoriesResp, repoIDResp, createDiscResp}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: newTestGHClient(t, d)}
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		publishScript("a", "1.0.0", nil),
	}}
	posted, skipped, errs := publishDiscussions(context.Background(), env, r)
	if posted != 1 || skipped != 0 || errs != 0 {
		t.Fatalf("posted=%d skipped=%d errs=%d, want 1/0/0", posted, skipped, errs)
	}
	led := r.Scripts[0].Discussions
	if len(led) != 1 || led[0].Version != "1.0.0" || led[0].Number != 7 || led[0].NodeID != "D_1" ||
		led[0].URL != "https://github.com/o/r/discussions/7" || led[0].CreatedAt != "2026-10-10T00:00:00Z" {
		t.Errorf("发牌条目错误: %+v", led)
	}
}

// TestPublishSkipsWhenCurrent 末条==当前 → skipped=1，零 API 调用。
func TestPublishSkipsWhenCurrent(t *testing.T) {
	d := &multiRespDoer{t: t, resps: nil} // 任何请求都会 t.Errorf
	env := &Env{Root: "/tmp", RepoOwner: "o", GHClient: newTestGHClient(t, d)}
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		publishScript("a", "1.0.0", []registry.DiscussionEntry{{Version: "1.0.0", Number: 3, NodeID: "D_3"}}),
	}}
	posted, skipped, errs := publishDiscussions(context.Background(), env, r)
	if posted != 0 || skipped != 1 || errs != 0 {
		t.Errorf("posted=%d skipped=%d errs=%d, want 0/1/0", posted, skipped, errs)
	}
}

// TestPublishPostsWhenStale 末条旧于当前 → 发帖+追加。
func TestPublishPostsWhenStale(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{categoriesResp, repoIDResp, createDiscResp}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: newTestGHClient(t, d)}
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		publishScript("a", "1.0.0", []registry.DiscussionEntry{{Version: "0.9.0", Number: 3, NodeID: "D_3"}}),
	}}
	posted, skipped, errs := publishDiscussions(context.Background(), env, r)
	if posted != 1 || skipped != 0 || errs != 0 {
		t.Errorf("posted=%d skipped=%d errs=%d, want 1/0/0", posted, skipped, errs)
	}
	led := r.Scripts[0].Discussions
	if len(led) != 2 || led[1].Version != "1.0.0" {
		t.Errorf("应追加而非覆盖: %+v", led)
	}
}

// TestPublishFailureNoLedgerAppend 发牌失败 → errs=1，不追加，且继续下一脚本。
func TestPublishFailureNoLedgerAppend(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		categoriesResp, repoIDResp,
		`{"errors":[{"message":"server error"}]}`, // 脚本 a 发帖失败
		createDiscResp, // 脚本 b 发帖成功（repoID 已缓存）
	}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: newTestGHClient(t, d)}
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		publishScript("a", "1.0.0", nil),
		publishScript("b", "2.0.0", nil),
	}}
	posted, _, errs := publishDiscussions(context.Background(), env, r)
	if posted != 1 || errs != 1 {
		t.Errorf("posted=%d errs=%d, want 1/1", posted, errs)
	}
	if len(r.Scripts[0].Discussions) != 0 {
		t.Errorf("失败脚本不应追加发牌: %+v", r.Scripts[0].Discussions)
	}
	if len(r.Scripts[1].Discussions) != 1 {
		t.Errorf("成功脚本应记录发牌: %+v", r.Scripts[1].Discussions)
	}
}

// TestSelectCategory 分类选择 q-a > 列表第一个。
func TestSelectCategory(t *testing.T) {
	cases := []struct {
		name string
		cats []github.Category
		want string
	}{
		{"q-a 优先", []github.Category{{NodeID: "X", Slug: "general"}, {NodeID: "Q", Slug: "q-a"}}, "Q"},
		{"无 q-a 取第一个", []github.Category{{NodeID: "A", Slug: "a"}, {NodeID: "B", Slug: "b"}}, "A"},
		{"仅一个分类", []github.Category{{NodeID: "Z", Slug: "z"}}, "Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := selectCategory(c.cats); got != c.want {
				t.Errorf("selectCategory = %q, want %q", got, c.want)
			}
		})
	}
}

// TestPublishCategoriesError 分类查询失败 → 待发帖脚本 errs++，失败缓存不重复请求。
func TestPublishCategoriesError(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{`{"errors":[{"message":"boom"}]}`}}
	env := &Env{Root: "/tmp", RepoOwner: "o", GHClient: newTestGHClient(t, d)}
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{
		publishScript("a", "1.0.0", nil),
		publishScript("b", "1.0.0", nil),
	}}
	posted, _, errs := publishDiscussions(context.Background(), env, r)
	if posted != 0 || errs != 2 {
		t.Errorf("posted=%d errs=%d, want 0/2", posted, errs)
	}
	if d.calls != 1 {
		t.Errorf("分类查询应缓存只发 1 次: calls=%d", d.calls)
	}
}

// TestPublishSkipsAndNilClient Deleted 跳过；GHClient nil 零动作。
func TestPublishSkipsAndNilClient(t *testing.T) {
	d := &multiRespDoer{t: t, resps: nil}
	env := &Env{Root: "/tmp", RepoOwner: "o", GHClient: newTestGHClient(t, d)}
	del := publishScript("a", "1.0.0", nil)
	del.Deleted = true
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{del}}
	if posted, skipped, errs := publishDiscussions(context.Background(), env, r); posted != 0 || skipped != 0 || errs != 0 {
		t.Errorf("Deleted 应全部跳过: %d/%d/%d", posted, skipped, errs)
	}
	envNo := &Env{Root: "/tmp", RepoOwner: "o", GHClient: nil}
	r2 := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{publishScript("a", "1.0.0", nil)}}
	if posted, _, errs := publishDiscussions(context.Background(), envNo, r2); posted != 0 || errs != 0 {
		t.Errorf("GHClient nil 应零动作: %d/%d", posted, errs)
	}
}

// TestProjectStageOrder 阶段一错误进 Errors，阶段二 Posted 独立累计。
func TestProjectStageOrder(t *testing.T) {
	d := &multiRespDoer{t: t, resps: []string{
		`{"errors":[{"message":"boom"}]}`,          // 阶段一 GetIssue 失败
		categoriesResp, repoIDResp, createDiscResp, // 阶段二成功
	}}
	env := &Env{Root: "/tmp", RepoOwner: "o", RepoName: "o/r", GHClient: newTestGHClient(t, d)}
	s := publishScript("a", "1.0.0", nil)
	r := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{s}}
	res, err := Project(context.Background(), env, r)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if res.Errors != 1 || res.Posted != 1 {
		t.Errorf("Errors=%d Posted=%d, want 1/1（阶段一失败不影响阶段二）", res.Errors, res.Posted)
	}
}
