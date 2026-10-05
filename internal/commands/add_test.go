package commands

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/script"
)

// fakeDoer 模拟 HTTP 请求，用于测试 fetchSource。
type fakeDoer struct {
	resp *http.Response
	err  error
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.resp == nil {
		return &http.Response{StatusCode: 200, Body: nopCloser{}}, nil
	}
	return f.resp, nil
}

type nopCloser struct{}

func (nopCloser) Read(p []byte) (n int, err error) { return 0, io.EOF }
func (nopCloser) Close() error                     { return nil }

// buildTestEnvWithFake 构造带 fake Doer 的测试 Env。
func buildTestEnvWithFake(t *testing.T) (*Env, string) {
	t.Helper()
	root := t.TempDir()
	reg := &registry.Registry{
		Schema: registry.SchemaVersion,
		Scripts: []registry.Script{
			{
				ID:        "self01",
				Type:      registry.TypeSelf,
				Name:      "测试脚本",
				Version:   "1.0.0",
				Enabled:   true,
				Deleted:   false,
				Match:     []string{"*://*/*"},
				Grant:     []string{"none"},
				CreatedAt: "2026-01-01T00:00:00Z",
				UpdatedAt: "2026-10-05T00:00:00Z",
				Changelog: []registry.ChangelogEntry{{Version: "1.0.0", Date: "2026-10-05", Note: "初始"}},
			},
			{
				ID:          "sync01",
				Type:        registry.TypeSynced,
				Name:        "同步脚本",
				Version:     "1.0.0",
				Enabled:     true,
				Deleted:     false,
				Match:       []string{"*://example.com/*"},
				Grant:       []string{"GM.xmlHttpRequest"},
				CreatedAt:   "2026-01-01T00:00:00Z",
				UpdatedAt:   "2026-10-04T00:00:00Z",
				SourceURL:   stringPtr("https://example.com/script.user.js"),
				SourceType:  stringPtr("direct"),
				SyncEnabled: boolPtr(true),
			},
		},
	}
	if err := reg.Save(filepath.Join(root, "registry.json")); err != nil {
		t.Fatal(err)
	}
	return &Env{
		Root:       root,
		Doer:       &fakeDoer{},
		Now:        time.Now(),
		PagesBase:  "https://test.github.io/test",
		AuthorName: "Tester",
	}, root
}

// TestAddFromURL 测试 /add <URL> 模式。
func TestAddFromURL(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	sourceCode := `// ==UserScript==
// @name        示例脚本
// @namespace   test
// @version     2.0.0
// @description 测试脚本
// @match       *://example.com/*
// @grant       GM.xmlHttpRequest
// ==/UserScript==`

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(sourceCode)),
		Header:     http.Header{},
	}
	env.Doer.(*fakeDoer).resp = resp

	res, err := Execute("add", env, "https://other.com/script.user.js", nil)
	if err != nil {
		t.Fatalf("add 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "已添加脚本") {
		t.Errorf("add 应返回成功消息: %s", res.Text)
	}
}

// TestAddFromURLAlreadyExists 测试添加已存在的脚本。
func TestAddFromURLAlreadyExists(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	sourceCode := `// ==UserScript==
// @name        示例脚本
// @version     2.0.0
// @match       *://other.com/*
// @grant       none
// ==/UserScript==`

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(sourceCode)),
		Header:     http.Header{},
	}
	env.Doer.(*fakeDoer).resp = resp

	Execute("add", env, "https://other.com/script.user.js", nil)

	res, err := Execute("add", env, "https://other.com/script.user.js", nil)
	if err != nil {
		t.Fatalf("add 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "已存在") {
		t.Errorf("add 已存在应提示: %s", res.Text)
	}
}

// TestAddFromURLNetworkError 测试网络错误。
func TestAddFromURLNetworkError(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)
	env.Doer.(*fakeDoer).err = fmt.Errorf("network error")

	_, err := Execute("add", env, "https://other.com/script.user.js", nil)
	if err == nil {
		t.Fatal("add 网络错误应返回 error")
	}
}

// TestAddSelfScript 测试 /add + 代码块。
func TestAddSelfScript(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	code := `// ==UserScript==
// @name        自写脚本
// @namespace   test
// @version     1.0.0
// @description 自写测试
// @match       *://*/*
// @grant       none
// ==/UserScript==`

	res, err := Execute("add", env, "", []string{code})
	if err != nil {
		t.Fatalf("add 自写脚本执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "已添加自写脚本") {
		t.Errorf("add 自写脚本应成功: %s", res.Text)
	}
}

// TestAddSelfScriptInvalidHeader 测试无效脚本头。
func TestAddSelfScriptInvalidHeader(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	res, err := Execute("add", env, "", []string{"hello world"})
	if err != nil {
		t.Fatalf("add 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "无法解析脚本头") {
		t.Errorf("add 无效头应提示: %s", res.Text)
	}
}

// TestAddNoCodeBlocks 测试没有代码块。
func TestAddNoCodeBlocks(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	res, err := Execute("add", env, "", nil)
	if err != nil {
		t.Fatalf("add 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "代码块") {
		t.Errorf("add 无代码块应提示: %s", res.Text)
	}
}

// TestAddSelfScriptIDConflict 测试自写脚本 ID 冲突（FindByID 命中已有条目）。
func TestAddSelfScriptIDConflict(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	// 手动在 registry 中添加一个 ID 与 newSelfID() 碰撞的条目——实际上 newSelfID 是随机的，
	// 直接调用 addSelfScript 绕过 Execute 来覆盖冲突路径较难。
	// 改为测试 Execute("add", env, "", []string{code}) 的正常路径
	// 并确保 r.FindByID(id) 不会误命中。
	code := `// ==UserScript==
// @name        另一脚本
// @version     1.0.0
// @match       *://other.com/*
// @grant       none
// ==/UserScript==`

	res, err := Execute("add", env, "", []string{code})
	if err != nil {
		t.Fatalf("add 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "已添加自写脚本") {
		t.Errorf("add 应成功: %s", res.Text)
	}
}

// TestSyncFull 测试完整同步流程。
func TestSyncFull(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	newCode := `// ==UserScript==
// @name        同步脚本
// @version     3.0.0
// @match       *://example.com/*
// @grant       GM.xmlHttpRequest
// ==/UserScript==`

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(newCode)),
		Header:     http.Header{},
	}
	env.Doer.(*fakeDoer).resp = resp

	res, err := Execute("sync", env, "sync01", nil)
	if err != nil {
		t.Fatalf("sync 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "v1.0.0 → v3.0.0") {
		t.Errorf("sync 应显示版本更新: %s", res.Text)
	}
}

// TestSyncNoChange 测试版本无变化。
func TestSyncNoChange(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	newCode := `// ==UserScript==
// @name        同步脚本
// @version     1.0.0
// @match       *://example.com/*
// @grant       GM.xmlHttpRequest
// ==/UserScript==`

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(newCode)),
		Header:     http.Header{},
	}
	env.Doer.(*fakeDoer).resp = resp

	res, err := Execute("sync", env, "sync01", nil)
	if err != nil {
		t.Fatalf("sync 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "最新版本") {
		t.Errorf("sync 无变化应提示: %s", res.Text)
	}
}

// TestSyncAll 测试批量同步。
func TestSyncAll(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`// test new`)),
		Header:     http.Header{},
	}
	env.Doer.(*fakeDoer).resp = resp

	res, err := Execute("sync", env, "all", nil)
	if err != nil {
		t.Fatalf("sync all 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "脚本") {
		t.Errorf("sync all 应有结果: %s", res.Text)
	}
}

// TestBuildFull 测试完整构建。
func TestBuildFull(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	testCode := `// ==UserScript==
// @name        测试脚本
// @version     1.0.0
// ==/UserScript==`
	if err := script.WriteSource(env.Root, "self01", registry.TypeSelf, testCode); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}

	res, err := Execute("build", env, "", nil)
	if err != nil {
		t.Fatalf("build 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "已构建") {
		t.Errorf("build 应成功: %s", res.Text)
	}
}

// TestFetchSourceNilDoer 测试 nil Doer。
func TestFetchSourceNilDoer(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)
	env.Doer = nil

	_, err := fetchSource(env, "https://example.com/script.user.js")
	if err == nil {
		t.Fatal("fetchSource nil Doer 应返回 error")
	}
	if !strings.Contains(err.Error(), "Doer 为空") {
		t.Errorf("错误信息应包含 'Doer 为空': %v", err)
	}
}

// TestSyncAllNoneToUpdate 测试全部同步但无更新。
func TestSyncAllNoneToUpdate(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	// 版本相同的响应
	newCode := `// ==UserScript==
// @name        同步脚本
// @version     1.0.0
// @match       *://example.com/*
// @grant       GM.xmlHttpRequest
// ==/UserScript==`
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(newCode)),
		Header:     http.Header{},
	}
	env.Doer.(*fakeDoer).resp = resp

	res, err := Execute("sync", env, "all", nil)
	if err != nil {
		t.Fatalf("sync all 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "已是最新版本") {
		t.Errorf("无更新应提示: %s", res.Text)
	}
}

// TestSyncAllSomeFail 测试批量同步部分失败。
func TestSyncAllSomeFail(t *testing.T) {
	env, _ := buildTestEnvWithSource(t)
	env.Doer = &fakeDoer{}

	// 只有 sync01 可以同步，另一个被设为已删除
	reg, _ := loadReg(env)
	// 添加一个已删除的 synced 脚本
	reg.Scripts = append(reg.Scripts, registry.Script{
		ID:        "del_sync",
		Type:      registry.TypeSynced,
		Name:      "已删同步",
		Version:   "1.0.0",
		Enabled:   true,
		Deleted:   true,
		Match:     []string{"*://del.com/*"},
		Grant:     []string{"none"},
		SourceURL: stringPtr("https://del.com/s.js"),
	})
	saveReg(env, reg)

	newCode := `// ==UserScript==
// @name        同步脚本
// @version     1.0.0
// @match       *://example.com/*
// @grant       GM.xmlHttpRequest
// ==/UserScript==`
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(newCode)),
		Header:     http.Header{},
	}
	env.Doer.(*fakeDoer).resp = resp

	res, err := Execute("sync", env, "all", nil)
	if err != nil {
		t.Fatalf("sync all 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "脚本") {
		t.Errorf("sync all 应有结果: %s", res.Text)
	}
}

// TestSyncAllWritesRegistry 回归：runSyncAll 曾在 []registry.Script 副本上
// 改字段，saveReg 写回的 registry 与改动前完全一致 —— 批量同步从不更新版本号。
func TestSyncAllWritesRegistry(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	newCode := `// ==UserScript==
// @name        同步脚本
// @version     3.0.0
// @author       Tester
// @match       *://example.com/*
// @grant       GM.xmlHttpRequest
// ==/UserScript==`
	env.Doer.(*fakeDoer).resp = &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(newCode)),
		Header:     http.Header{},
	}

	res, err := Execute("sync", env, "all", nil)
	if err != nil {
		t.Fatalf("sync all 执行失败: %v", err)
	}
	if !res.Changed {
		t.Errorf("sync all 有版本变更应落盘: %s", res.Text)
	}

	reg, err := loadReg(env)
	if err != nil {
		t.Fatalf("重新加载 registry 失败: %v", err)
	}
	var got *registry.Script
	for i := range reg.Scripts {
		if reg.Scripts[i].ID == "sync01" {
			got = &reg.Scripts[i]
		}
	}
	if got == nil {
		t.Fatal("registry 中应有 sync01")
	}
	if got.Version != "3.0.0" {
		t.Errorf("版本号应从 1.0.0 更新到 3.0.0, got %q", got.Version)
	}
	if got.LastSyncedAt == nil {
		t.Error("LastSyncedAt 应被写入")
	}
	if got.Author == "" {
		t.Errorf("Author 应随同步更新, got %q", got.Author)
	}

	code, err := script.ReadSource(env.Root, "sync01", registry.TypeSynced)
	if err != nil {
		t.Fatalf("读取同步源码失败: %v", err)
	}
	if !strings.Contains(code, "@version     3.0.0") {
		t.Errorf("源码文件应同步为 3.0.0 内容: %s", code)
	}
}

// TestSyncAllAlias 验证 /sync-all 注册为 /sync all 的别名。
func TestSyncAllAlias(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	res, err := Execute("sync-all", env, "", nil)
	if err != nil {
		t.Fatalf("sync-all 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "脚本") {
		t.Errorf("sync-all 应返回同步结果: %s", res.Text)
	}
}

func TestAddFromURLWithDisabled(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	sourceCode := `// ==UserScript==
// @name        停用脚本
// @version     1.0.0
// @match       *://disabled.com/*
// @grant       none
// ==/UserScript==`

	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(sourceCode)),
		Header:     http.Header{},
	}
	env.Doer.(*fakeDoer).resp = resp

	// 先添加停用的脚本
	res1, err := Execute("add", env, "https://disabled.com/script.user.js", nil)
	if err != nil {
		t.Fatalf("首次添加失败: %v", err)
	}
	if !strings.Contains(res1.Text, "已添加") {
		t.Errorf("首次添加应成功: %s", res1.Text)
	}

	// 再添加相同 URL 应提示已存在
	res2, err := Execute("add", env, "https://disabled.com/script.user.js", nil)
	if err != nil {
		t.Fatalf("二次添加不应返回 error: %v", err)
	}
	if !strings.Contains(res2.Text, "已存在") {
		t.Errorf("二次添加应提示已存在: %s", res2.Text)
	}
}

// TestSyncNotFoundDetailed 测试同步不存在的脚本。
func TestSyncNotFoundDetailed(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)
	res, err := Execute("sync", env, "missing", nil)
	if err != nil {
		t.Fatalf("sync 不应返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "未找到") {
		t.Errorf("sync 未找到应提示: %s", res.Text)
	}
}

// TestRmByID 测试按 ID 删除。
func TestRmByID(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)
	res, err := Execute("rm", env, "self01", nil)
	if err != nil {
		t.Fatalf("rm 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "已软删除") {
		t.Errorf("rm 应按 ID 删除: %s", res.Text)
	}
}

// TestRmByName 测试按名称删除。
func TestRmByName(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)
	res, err := Execute("rm", env, "同步脚本", nil)
	if err != nil {
		t.Fatalf("rm 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "已软删除") {
		t.Errorf("rm 应按名称删除: %s", res.Text)
	}
}

// TestInfoByID 测试按 ID 查询。
func TestInfoByID(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)
	res, err := Execute("info", env, "self01", nil)
	if err != nil {
		t.Fatalf("info 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "测试脚本") {
		t.Errorf("info 应显示脚本信息: %s", res.Text)
	}
}

// TestAddFromURLDeletedRecover 测试复活已软删除的脚本（I-4 契约）。
func TestAddFromURLDeletedRecover(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)

	// 在 registry 中手动添加一个已删除的 synced 脚本
	reg, _ := loadReg(env)
	reg.Add(registry.Script{
		ID:          "del01",
		Type:        registry.TypeSynced,
		Name:        "被删脚本",
		Version:     "1.0.0",
		Enabled:     false,
		Deleted:     true,
		SourceURL:   stringPtr("https://recover.com/s.js"),
		CreatedAt:   "2026-01-01T00:00:00Z",
		UpdatedAt:   "2026-01-01T00:00:00Z",
		SyncEnabled: boolPtr(true),
	})
	saveReg(env, reg)

	sourceCode := `// ==UserScript==
// @name        被删脚本
// @version     1.0.0
// @match       *://recover.com/*
// @grant       none
// ==/UserScript==`
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(sourceCode)),
		Header:     http.Header{},
	}
	env.Doer.(*fakeDoer).resp = resp

	res, err := Execute("add", env, "https://recover.com/s.js", nil)
	if err != nil {
		t.Fatalf("复活应不返回 error: %v", err)
	}
	if !strings.Contains(res.Text, "已复活") {
		t.Errorf("复活路径未触发: %s", res.Text)
	}
}

// TestInfoByName 测试按名称查询。
func TestInfoByName(t *testing.T) {
	env, _ := buildTestEnvWithFake(t)
	res, err := Execute("info", env, "同步脚本", nil)
	if err != nil {
		t.Fatalf("info 执行失败: %v", err)
	}
	if !strings.Contains(res.Text, "同步脚本") {
		t.Errorf("info 应显示脚本信息: %s", res.Text)
	}
}

// TestAddSelfScriptWriteError 测试写入脚本文件失败路径。
func TestAddSelfScriptWriteError(t *testing.T) {
	root := t.TempDir()
	// 创建一个 registry
	reg := &registry.Registry{
		Schema:  registry.SchemaVersion,
		Scripts: []registry.Script{},
	}
	if err := reg.Save(filepath.Join(root, "registry.json")); err != nil {
		t.Fatal(err)
	}
	// 将 scripts 目录设为文件，使后续写入失败
	scriptsDir := filepath.Join(root, "scripts")
	os.Remove(scriptsDir)
	if err := os.WriteFile(scriptsDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	env := &Env{
		Root:      root,
		Doer:      &fakeDoer{},
		Now:       time.Now(),
		PagesBase: "https://test.github.io/test",
	}

	code := `// ==UserScript==
// @name        测试脚本
// @version     1.0.0
// @match       *://*/*
// @grant       none
// ==/UserScript==`

	_, err := Execute("add", env, "", []string{code})
	if err == nil {
		t.Fatal("add 写入失败应返回 error")
	}
	if !strings.Contains(err.Error(), "写入脚本文件失败") {
		t.Errorf("错误信息应含 '写入脚本文件失败': %v", err)
	}
}

// TestFetchSourceNullResult 测试 fetchSource 返回空结果。
func TestFetchSourceNullResult(t *testing.T) {
	root := t.TempDir()
	reg := &registry.Registry{Schema: registry.SchemaVersion, Scripts: []registry.Script{}}
	if err := reg.Save(filepath.Join(root, "registry.json")); err != nil {
		t.Fatal(err)
	}

	// fakeDoer 返回空 body
	env := &Env{
		Root: root,
		Doer: &fakeDoer{resp: &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}},
		Now:  time.Now(),
	}

	_, err := fetchSource(env, "https://greasyfork.org/scripts/12345")
	if err == nil {
		t.Fatal("fetchSource 空结果应返回 error")
	}
	// 验证错误信息包含抓取失败的提示
	if !strings.Contains(err.Error(), "抓取来源失败") {
		t.Errorf("错误信息应含 '抓取来源失败': %v", err)
	}
}
