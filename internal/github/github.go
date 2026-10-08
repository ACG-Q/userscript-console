// Package github 提供 GitHub GraphQL 客户端与 13 个固定查询文档的 typed helpers。
//
// 关键设计（SPEC-ARCH-TEST §3.2 假客户端改造）：
//   - 测试以 Doer 注入 fake，按**请求结构**断言（POST/Bearer/variables），不按查询子串路由；
//   - 字段正确性靠 typed 解码结构与真实 schema 一一对应 + canned JSON 解码测试；
//   - context.Context 贯穿全部网络调用；5xx/429/408/传输错误指数退避重试，
//     次级限速（rate limit/abuse）**独立只重试一次**，4xx 其余不重试。
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const defaultEndpoint = "https://api.github.com/graphql"

// Doer 网络注入点。
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// GraphQLError 聚合 GraphQL 200 响应中的 errors[]。
type GraphQLError struct {
	Messages []string
}

func (e *GraphQLError) Error() string {
	return "github GraphQL 错误: " + strings.Join(e.Messages, "; ")
}

// rateLimitRe 次级限速/滥用保护判定（200 errors 与 4xx body 通用）。
var rateLimitRe = regexp.MustCompile(`(?i)rate limit|abuse detection|secondary rate`)

func isRateLimited(msg string) bool { return rateLimitRe.MatchString(msg) }

// Client GraphQL 客户端。
type Client struct {
	token    string
	owner    string
	name     string
	endpoint string
	doer     Doer
	retry    int
	backoff  time.Duration

	mu            sync.Mutex
	cachedRepoID  string
	repoIDFetched bool
}

type Option func(*Client)

func WithDoer(d Doer) Option       { return func(c *Client) { c.doer = d } }
func WithEndpoint(u string) Option { return func(c *Client) { c.endpoint = u } }
func WithRetry(n int) Option       { return func(c *Client) { c.retry = n } }

// New 构造客户端。token 空、repo 非 "owner/name" → error。
func New(token, repo string, opts ...Option) (*Client, error) {
	if token == "" {
		return nil, errors.New("github: token 为空")
	}
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("github: repo 必须为 owner/name，实际 %q", repo)
	}
	c := &Client{
		token:    token,
		owner:    parts[0],
		name:     parts[1],
		endpoint: defaultEndpoint,
		doer:     http.DefaultClient,
		retry:    2,
		backoff:  100 * time.Millisecond,
	}
	for _, o := range opts {
		o(c)
	}
	if c.retry < 0 {
		c.retry = 0
	}
	return c, nil
}

// $owner / $name 变量注入（词边界匹配，避免 $labelName 之类误伤）。
var (
	varOwnerRe = regexp.MustCompile(`\$owner\b`)
	varNameRe  = regexp.MustCompile(`\$name\b`)
)

type envelope struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// Raw 执行查询：把 owner/name 注入声明了对应变量的查询；data 解到 out（out 可 nil）。
func (c *Client) Raw(ctx context.Context, query string, variables map[string]any, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if variables == nil {
		variables = map[string]any{}
	}
	if varOwnerRe.MatchString(query) {
		variables["owner"] = c.owner
	}
	if varNameRe.MatchString(query) {
		variables["name"] = c.name
	}

	attempts := 0        // 普通预算（1+retry 次）
	rateRetried := false // 次级限速：独立仅一次

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		attempts++
		status, body, err := c.doOnce(ctx, query, variables)
		if err != nil { // 传输错误（含 ctx 中断在 Do 内）
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if attempts > c.retry {
				return fmt.Errorf("github: 请求失败: %w", err)
			}
			if err := c.sleep(ctx, attempts); err != nil {
				return err
			}
			continue
		}

		// 解析响应
		var env envelope
		_ = json.Unmarshal(body, &env) // 非 JSON body（网关 HTML 等）走下方 status 分支
		msg := string(body)

		switch {
		case status == http.StatusOK:
			if len(env.Errors) > 0 {
				var msgs []string
				for _, e := range env.Errors {
					msgs = append(msgs, e.Message)
				}
				gqlErr := &GraphQLError{Messages: msgs}
				if isRateLimited(strings.Join(msgs, " ")) && !rateRetried {
					rateRetried = true
					if err := c.sleep(ctx, attempts); err != nil {
						return err
					}
					continue
				}
				return gqlErr
			}
			if out != nil && len(env.Data) > 0 {
				if err := json.Unmarshal(env.Data, out); err != nil {
					return fmt.Errorf("github: 响应解码失败: %w", err)
				}
			}
			return nil

		case status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500:
			if attempts > c.retry {
				return fmt.Errorf("github: HTTP %d（重试 %d 次后放弃）", status, c.retry)
			}
			if err := c.sleep(ctx, attempts); err != nil {
				return err
			}
			continue

		default: // 其余 4xx
			if isRateLimited(msg) && !rateRetried {
				rateRetried = true
				if err := c.sleep(ctx, attempts); err != nil {
					return err
				}
				continue
			}
			snippet := strings.TrimSpace(string(body))
			if len(snippet) > 300 {
				snippet = snippet[:300] + "…"
			}
			return fmt.Errorf("github: HTTP %d: %s", status, snippet)
		}
	}
}

// doOnce 发一次请求，返回状态码与 body。
func (c *Client) doOnce(ctx context.Context, query string, variables map[string]any) (int, []byte, error) {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doer.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// sleep 指数退避：base * 2^(attempt-1)，与 ctx.Done 双选。
func (c *Client) sleep(ctx context.Context, attempt int) error {
	d := c.backoff << (attempt - 1)
	if d <= 0 || d > 30*time.Second {
		d = 30 * time.Second
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// repoID 复用 REPO_QUERY 取 repositoryId（number 传 1；issue 为 null 不影响读 id），加锁缓存。
func (c *Client) repoID(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.repoIDFetched && c.cachedRepoID != "" {
		id := c.cachedRepoID
		c.mu.Unlock()
		return id, nil
	}
	c.mu.Unlock()

	var out struct {
		Repository struct {
			ID string `json:"id"`
		} `json:"repository"`
	}
	if err := c.Raw(ctx, REPO_QUERY, map[string]any{"number": 1}, &out); err != nil {
		return "", fmt.Errorf("取 repositoryId: %w", err)
	}
	if out.Repository.ID == "" {
		return "", errors.New("取 repositoryId: repository.id 为空")
	}
	c.mu.Lock()
	c.cachedRepoID, c.repoIDFetched = out.Repository.ID, true
	c.mu.Unlock()
	return out.Repository.ID, nil
}
