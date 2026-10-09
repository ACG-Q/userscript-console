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
func init() { register(example{}) }
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
