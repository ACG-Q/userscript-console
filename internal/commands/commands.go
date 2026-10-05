// Package commands 是命令面板的命令注册与执行层（职责参照 Python `commands/*`）。
//
// 契约要点：
//   - 扩展方式 = 新文件一个 + init() 里 Register 一行（I-5：漏注册由硬编码清单测试兜底）；
//   - 重名注册是编程错误 → panic（在包初始化期炸掉，测试必红）；
//   - Execute 提供统一 panic 边界（I-6）：任何 handler panic 都转成回帖文本，
//     err 恒为 nil（调用方仍会回帖，exit 0 语义由 cli 层保证）；
//   - 业务型失败（找不到、重复添加、幂等提示）→ Result.Text 承载、err=nil；
//     操作型失败（registry/文件 IO、网络、参数用法）→ error 上抛（cli 层映射退出码）。
//
// 依赖方向：registry / script / sources / meta → 标准库；本包不反向依赖 cli、pages。
package commands

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/acg-q/userscript-console/internal/github"
	"github.com/acg-q/userscript-console/internal/registry"
	"github.com/acg-q/userscript-console/internal/sources"
)

// ErrUnknown 未注册命令（调用方用 errors.Is 判定）。
var ErrUnknown = errors.New("未知命令")

// registryFileName 数据根下的账本文件名（SPEC-DATA §1.1）。
const registryFileName = "registry.json"

// ── 契约类型 ────────────────────────────────────────────────

// SiteBuilder 整站生成器，由 cli 层（cmd/usm）注入。
//
// SPEC-ARCH-TEST §1：internal/commands 不得反向 import internal/pages，
// 所以 /build 的整站生成走接口；Env.Site 为 nil → 只产出 dist/ 脚本副本
// （单测与不需要站点的场景），整站降级为可选项。
type SiteBuilder interface {
	// Build 渲染并落盘整站产物。
	// 返回页面数、是否发生文件变更、渲染/抓取告警。
	Build(reg *registry.Registry) (pages int, changed bool, warnings []string, err error)
}

// Env 由 cli 层构造（测试直接构造）。零值字段按各命令的兜底语义处理。
type Env struct {
	Root            string // 数据根（含 registry.json）
	RepoOwner       string
	RepoName        string // 仓库名，格式 owner/repo；用于 GitHub API
	CommentUser     string
	IssueNumber     int
	PagesBase       string // https://<owner>.github.io/<repo> —— downloadURL/updateURL 前缀
	AuthorName      string // 自写脚本头默认作者
	AuthorNamespace string // 自写脚本头默认命名空间
	Doer            sources.Doer
	GHClient        *github.Client // GitHub GraphQL 客户端；nil → 跳过 GitHub 操作
	Site            SiteBuilder    // 整站生成器；nil → /build 只产出 dist/ 脚本副本
	Now             time.Time      // I-7 可注入；零值 → time.Now()
}

// Result 回帖结果。
type Result struct {
	Text     string   // 回帖正文（Markdown，中文）
	Changed  bool     // 本次是否写入 registry/scripts/dist 任一文件
	Warnings []string // 降级/告警信息（action.yml 的 warnings 输出；nil = 无告警）
	Pages    int      // build 产出的站点页面数（SPEC-CLI §3）
}

// Handler 一个命令的执行体。
type Handler func(env *Env, args string, codeBlocks []string) (Result, error)

// Command 命令描述与执行体。
type Command struct {
	Name  string
	Help  string
	Usage string
	Run   Handler
}

// ── 注册器（I-5） ────────────────────────────────────────────

var (
	regMu     sync.RWMutex
	regByName = map[string]Command{}
)

// Register 注册命令（init() 调用）。重名 panic——那是编程错误，必须在测试期炸出来。
func Register(c Command) {
	if strings.TrimSpace(c.Name) == "" {
		panic("commands: 命令名不能为空")
	}
	if c.Run == nil {
		panic(fmt.Sprintf("commands: 命令 %q 的 handler 为 nil", c.Name))
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := regByName[c.Name]; dup {
		panic(fmt.Sprintf("commands: 命令 %q 重复注册", c.Name))
	}
	regByName[c.Name] = c
}

// Get 取已注册命令；未注册 → ok=false（由调用方处理）。
func Get(name string) (Command, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	c, ok := regByName[name]
	return c, ok
}

// Names 全部命令名（字典序，稳定）。
func Names() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(regByName))
	for n := range regByName {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ── 执行与 panic 边界（I-6） ─────────────────────────────────

// Execute 查找并执行命令：
//   - 未注册 → Result{} + error（errors.Is(err, ErrUnknown)）；
//   - handler panic → ❌ 文本 + err=nil + Changed=false（I-6，不向上抛）。
func Execute(name string, env *Env, args string, code []string) (Result, error) {
	if env == nil {
		return Result{}, errors.New("commands: Env 未初始化")
	}
	c, ok := Get(name)
	if !ok {
		return Result{}, fmt.Errorf("%w: %s", ErrUnknown, name)
	}
	return Wrap(name, c.Run)(env, args, code)
}

// Wrap 给任意 handler 套上统一 panic 边界（SPEC-CLI §6 指向的 Wrap 落点）。
// 捕获后返回回帖文本：命令名 + 恢复后栈前 3 帧，err 恒为 nil。
func Wrap(name string, h Handler) Handler {
	return func(env *Env, args string, code []string) (res Result, err error) {
		defer func() {
			if recover() != nil {
				res = Result{Text: panicText(name), Changed: false}
				err = nil
			}
		}()
		return h(env, args, code)
	}
}

// panicText 生成 I-6 回帖文本：标题行 + ``` + 恢复后栈前 3 帧 + ```。
func panicText(name string) string {
	return fmt.Sprintf(
		"❌ 命令 /%s 执行时发生内部错误，已中止（仓库状态可能未变更）。\n```\n%s\n```",
		name, topFrames(3),
	)
}

// topFrames 当前 goroutine 栈的前 n 个有效帧（丢弃 "goroutine N [running]:" 头行）。
func topFrames(n int) string {
	buf := make([]byte, 64<<10)
	size := runtime.Stack(buf, false)
	lines := strings.Split(strings.TrimRight(string(buf[:size]), "\n"), "\n")
	frames := make([]string, 0, n)
	for _, ln := range lines[1:] {
		ln = strings.TrimRight(ln, "\r")
		if strings.TrimSpace(ln) == "" {
			continue
		}
		frames = append(frames, ln)
		if len(frames) == n {
			break
		}
	}
	return strings.Join(frames, "\n")
}

// ── 包内公共辅助 ────────────────────────────────────────────

// regPath 数据根下 registry.json 的路径。
func regPath(env *Env) string { return filepath.Join(env.Root, registryFileName) }

// loadReg 读取并校验账本；任何失败都是操作型错误（上抛）。
func loadReg(env *Env) (*registry.Registry, error) {
	if strings.TrimSpace(env.Root) == "" {
		return nil, errors.New("commands: 数据根 --root 未配置")
	}
	r, err := registry.Load(regPath(env))
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", registryFileName, err)
	}
	return r, nil
}

// saveReg 原子落盘账本；成功 → Changed=true。
func saveReg(env *Env, r *registry.Registry) (bool, error) {
	if err := r.Save(regPath(env)); err != nil {
		return false, fmt.Errorf("写入 %s 失败: %w", registryFileName, err)
	}
	return true, nil
}

// newSelfID 自写脚本 ID：crypto/rand UUID v4 形态（8-4-4-4-12）。
// rand 读取失败属不可恢复环境错误，panic 交给 I-6 边界转回帖。
func newSelfID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("commands: 生成脚本 ID 失败: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// nowOf 取本次执行时刻（I-7：env.Now 可注入，零值兜底真实时间）。
func nowOf(env *Env) time.Time {
	if env.Now.IsZero() {
		return time.Now()
	}
	return env.Now
}

// dateStr changelog 日期（YYYY-MM-DD）。
func dateStr(t time.Time) string { return t.UTC().Format("2006-01-02") }

// rfc3339 UTC RFC3339 时间戳（last_synced_at 等）。
func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// distURL 分发产物链接：PagesBase/dist/<id>.user.js（PagesBase 为空 → 空串 = 不注入）。
func distURL(env *Env, id string) string {
	base := strings.TrimSuffix(strings.TrimSpace(env.PagesBase), "/")
	if base == "" {
		return ""
	}
	return base + "/dist/" + id + ".user.js"
}

// isURL 判定参数是否是来源 URL（http/https 前缀）。
func isURL(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")
}

// findEntry 按 id → 来源 URL → 精确名称 的顺序查找（含已删除条目；调用方决定墓碑语义）。
func findEntry(r *registry.Registry, key string) *registry.Script {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	if s := r.FindByID(key); s != nil {
		return s
	}
	if isURL(key) {
		return r.FindBySourceURL(key)
	}
	for i := range r.Scripts {
		if r.Scripts[i].Name == key {
			return &r.Scripts[i]
		}
	}
	return nil
}

// fetchSource 识别来源并抓取解析（env.Doer 注入，测试走 fake，禁止真实网络）。
func fetchSource(env *Env, rawurl string) (*sources.Result, error) {
	if env.Doer == nil {
		return nil, errors.New("commands: 网络客户端未配置（Env.Doer 为空）")
	}
	a, err := sources.Detect(rawurl)
	if err != nil {
		return nil, fmt.Errorf("识别来源失败: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := a.Fetch(ctx, env.Doer, rawurl)
	if err != nil {
		return nil, fmt.Errorf("抓取来源失败: %w", err)
	}
	if res == nil {
		return nil, fmt.Errorf("抓取来源失败: %s 返回空结果", rawurl)
	}
	return res, nil
}

// reply 构造业务型结果（err 恒 nil，信息由 Text 承载）。
func reply(changed bool, format string, args ...any) (Result, error) {
	return Result{Text: fmt.Sprintf(format, args...), Changed: changed}, nil
}

// fail 构造业务型失败（❌ 前缀，Changed=false，err=nil）。
func fail(format string, args ...any) (Result, error) {
	return reply(false, "❌ "+format, args...)
}
