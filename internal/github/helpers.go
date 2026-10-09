package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ── 公开数据结构 ────────────────────────────────────────────

type Issue struct {
	Number    int
	NodeID    string
	Title     string
	Body      string
	State     string
	Author    string
	CreatedAt string
	UpdatedAt string
	Comments  int
}

type Comment struct {
	NodeID    string
	Author    string
	Body      string
	CreatedAt string
}

type Category struct {
	NodeID string
	Name   string
	Slug   string
}

type Discussion struct {
	Number     int
	NodeID     string
	Title      string
	Body       string
	URL        string
	CategoryID string
	CreatedAt  string
}

// ── 解码结构（与真实 GitHub schema 字段一一对应） ─────────────

type gqlIssue struct {
	ID        string `json:"id"`
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	State     string `json:"state"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	Author    *struct {
		Login string `json:"login"`
	} `json:"author"`
	Comments *struct {
		TotalCount int `json:"totalCount"`
	} `json:"comments"`
}

func (g gqlIssue) toIssue() (Issue, error) {
	if g.ID == "" || g.Number <= 0 {
		return Issue{}, fmt.Errorf("github: issue 缺字段（id=%q number=%d）", g.ID, g.Number)
	}
	iss := Issue{
		Number: g.Number, NodeID: g.ID, Title: g.Title, Body: g.Body,
		State: g.State, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
	}
	if g.Author != nil {
		iss.Author = g.Author.Login
	}
	if g.Comments != nil {
		iss.Comments = g.Comments.TotalCount
	}
	return iss, nil
}

type gqlComment struct {
	ID     string `json:"id"`
	Author *struct {
		Login string `json:"login"`
	} `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

func (g gqlComment) toComment() (Comment, error) {
	if g.ID == "" {
		return Comment{}, errors.New("github: comment 缺 id 字段")
	}
	c := Comment{NodeID: g.ID, Body: g.Body, CreatedAt: g.CreatedAt}
	if g.Author != nil {
		c.Author = g.Author.Login
	}
	return c, nil
}

type gqlPageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type gqlCategory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// ── Issue 系列 ─────────────────────────────────────────────

// ListRepoIssues 分页拉取全部 issue（states 空/ALL → 不过滤变量 null）。
func (c *Client) ListRepoIssues(ctx context.Context, state string) ([]Issue, error) {
	var states any
	switch strings.ToUpper(state) {
	case "", "ALL":
		states = nil
	default:
		states = []string{strings.ToUpper(state)}
	}
	var all []Issue
	cursor := any(nil)
	for page := 0; page < 50; page++ {
		var out struct {
			Repository struct {
				Issues struct {
					TotalCount int `json:"totalCount"`
					PageInfo   gqlPageInfo
					Nodes      []gqlIssue
				} `json:"issues"`
			} `json:"repository"`
		}
		if err := c.Raw(ctx, LIST_QUERY, map[string]any{"states": states, "cursor": cursor}, &out); err != nil {
			return nil, fmt.Errorf("ListRepoIssues: %w", err)
		}
		for _, n := range out.Repository.Issues.Nodes {
			iss, err := n.toIssue()
			if err != nil {
				return nil, err
			}
			all = append(all, iss)
		}
		pi := out.Repository.Issues.PageInfo
		if !pi.HasNextPage {
			return all, nil
		}
		if pi.EndCursor == "" {
			return nil, errors.New("ListRepoIssues: hasNextPage=true 但 endCursor 为空")
		}
		cursor = pi.EndCursor
	}
	return nil, errors.New("ListRepoIssues: 超过 50 页上限")
}

// GetIssue 按面板号取单个 issue（null → error）。
func (c *Client) GetIssue(ctx context.Context, number int) (*Issue, error) {
	var out struct {
		Repository struct {
			Issue *gqlIssue `json:"issue"`
		} `json:"repository"`
	}
	if err := c.Raw(ctx, REPO_QUERY, map[string]any{"number": number}, &out); err != nil {
		return nil, fmt.Errorf("GetIssue: %w", err)
	}
	if out.Repository.Issue == nil {
		return nil, fmt.Errorf("GetIssue: issue #%d 不存在", number)
	}
	iss, err := out.Repository.Issue.toIssue()
	if err != nil {
		return nil, err
	}
	return &iss, nil
}

// CreateIssue 新建 issue（自动取 repositoryId）。
func (c *Client) CreateIssue(ctx context.Context, title, body string) (*Issue, error) {
	rid, err := c.repoID(ctx)
	if err != nil {
		return nil, err
	}
	var out struct {
		CreateIssue struct {
			Issue *gqlIssue `json:"issue"`
		} `json:"createIssue"`
	}
	if err := c.Raw(ctx, CREATE_MUTATION, map[string]any{
		"repositoryId": rid, "title": title, "body": body,
	}, &out); err != nil {
		return nil, fmt.Errorf("CreateIssue: %w", err)
	}
	if out.CreateIssue.Issue == nil {
		return nil, errors.New("CreateIssue: 返回 issue 为 null")
	}
	iss, err := out.CreateIssue.Issue.toIssue()
	if err != nil {
		return nil, err
	}
	return &iss, nil
}

// UpdateIssue 更新标题与正文（幂等投影的写路径）。
func (c *Client) UpdateIssue(ctx context.Context, nodeID, title, body string) error {
	var out struct {
		UpdateIssue struct {
			Issue *gqlIssue `json:"issue"`
		} `json:"updateIssue"`
	}
	if err := c.Raw(ctx, UPDATE_MUTATION, map[string]any{
		"issueId": nodeID, "title": title, "body": body,
	}, &out); err != nil {
		return fmt.Errorf("UpdateIssue: %w", err)
	}
	if out.UpdateIssue.Issue == nil {
		return errors.New("UpdateIssue: 返回 issue 为 null")
	}
	return nil
}

// CloseIssue 关闭（墓碑化流程）。
func (c *Client) CloseIssue(ctx context.Context, nodeID string) error {
	return c.transition(ctx, nodeID, CLOSE_MUTATION, "closeIssue", "CLOSED", "CloseIssue")
}

// ReopenIssue 重开（复活流程）。
func (c *Client) ReopenIssue(ctx context.Context, nodeID string) error {
	return c.transition(ctx, nodeID, REOPEN_MUTATION, "reopenIssue", "OPEN", "ReopenIssue")
}

func (c *Client) transition(ctx context.Context, nodeID, query, key, wantState, name string) error {
	var out map[string]struct {
		Issue *gqlIssue `json:"issue"`
	}
	if err := c.Raw(ctx, query, map[string]any{"issueId": nodeID}, &out); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	payload, ok := out[key]
	if !ok || payload.Issue == nil {
		return fmt.Errorf("%s: 返回 issue 为 null", name)
	}
	if payload.Issue.State != wantState {
		return fmt.Errorf("%s: 期望状态 %s，实际 %s", name, wantState, payload.Issue.State)
	}
	return nil
}

// CreateLabel 创建标签；「already exists」（大小写不敏感）视为成功。
func (c *Client) CreateLabel(ctx context.Context, name, color, desc string) error {
	rid, err := c.repoID(ctx)
	if err != nil {
		return err
	}
	var out struct {
		CreateLabel struct {
			Label *struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"label"`
		} `json:"createLabel"`
	}
	err = c.Raw(ctx, CREATE_LABEL_MUTATION, map[string]any{
		"repositoryId": rid, "labelName": name, "color": color, "labelDescription": desc,
	}, &out)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return nil // 幂等：已存在不是失败
		}
		return fmt.Errorf("CreateLabel: %w", err)
	}
	if out.CreateLabel.Label == nil {
		return errors.New("CreateLabel: 返回 label 为 null")
	}
	return nil
}

// ── 评论与清理 ─────────────────────────────────────────────

// ListIssueComments 分页拉取 issue 评论（pageSize 钳 1..100；调用方循环传 cursor 不必要——
// 本 helper 一次拉全，最多 50 页）。issue 为 null → error。
func (c *Client) ListIssueComments(ctx context.Context, number, pageSize int) ([]Comment, error) {
	if pageSize < 1 {
		pageSize = 100
	}
	if pageSize > 100 {
		pageSize = 100
	}
	_ = pageSize // PANEL_QUERY 固定 first:100；参数保留供未来调整
	var all []Comment
	cursor := any(nil)
	for page := 0; page < 50; page++ {
		var out struct {
			Repository struct {
				Issue *struct {
					Comments struct {
						PageInfo gqlPageInfo  `json:"pageInfo"`
						Nodes    []gqlComment `json:"nodes"`
					} `json:"comments"`
				} `json:"issue"`
			} `json:"repository"`
		}
		if err := c.Raw(ctx, PANEL_QUERY, map[string]any{"number": number, "cursor": cursor}, &out); err != nil {
			return nil, fmt.Errorf("ListIssueComments: %w", err)
		}
		if out.Repository.Issue == nil {
			return nil, fmt.Errorf("ListIssueComments: issue #%d 不存在", number)
		}
		for _, n := range out.Repository.Issue.Comments.Nodes {
			cm, err := n.toComment()
			if err != nil {
				return nil, err
			}
			all = append(all, cm)
		}
		pi := out.Repository.Issue.Comments.PageInfo
		if !pi.HasNextPage {
			return all, nil
		}
		if pi.EndCursor == "" {
			return nil, errors.New("ListIssueComments: hasNextPage=true 但 endCursor 为空")
		}
		cursor = pi.EndCursor
	}
	return nil, errors.New("ListIssueComments: 超过 50 页上限")
}

// DeleteComment 删除评论（清理器）。payload 恒成功即 nil。
func (c *Client) DeleteComment(ctx context.Context, nodeID string) error {
	var out struct {
		DeleteComment json.RawMessage `json:"deleteIssueComment"`
	}
	if err := c.Raw(ctx, DELETE_COMMENT_MUTATION, map[string]any{"commentId": nodeID}, &out); err != nil {
		return fmt.Errorf("DeleteComment: %w", err)
	}
	return nil
}

// ── Discussions（版本帖） ───────────────────────────────────

// DiscussionCategories 列出分类。
func (c *Client) DiscussionCategories(ctx context.Context) ([]Category, error) {
	var out struct {
		Repository struct {
			DiscussionCategories struct {
				Nodes []gqlCategory `json:"nodes"`
			} `json:"discussionCategories"`
		} `json:"repository"`
	}
	if err := c.Raw(ctx, DISCUSSION_CATEGORIES_QUERY, nil, &out); err != nil {
		return nil, fmt.Errorf("DiscussionCategories: %w", err)
	}
	nodes := out.Repository.DiscussionCategories.Nodes
	if len(nodes) == 0 {
		return nil, errors.New("DiscussionCategories: 分类为空")
	}
	cats := make([]Category, 0, len(nodes))
	for _, n := range nodes {
		cats = append(cats, Category{NodeID: n.ID, Name: n.Name, Slug: n.Slug})
	}
	return cats, nil
}

// CreateDiscussion 发布版本帖。
func (c *Client) CreateDiscussion(ctx context.Context, categoryID, title, body string) (*Discussion, error) {
	rid, err := c.repoID(ctx)
	if err != nil {
		return nil, err
	}
	var out struct {
		CreateDiscussion struct {
			Discussion *struct {
				ID        string `json:"id"`
				Number    int    `json:"number"`
				Title     string `json:"title"`
				Body      string `json:"body"`
				URL       string `json:"url"`
				CreatedAt string `json:"createdAt"`
			} `json:"discussion"`
		} `json:"createDiscussion"`
	}
	if err := c.Raw(ctx, CREATE_DISCUSSION_MUTATION, map[string]any{
		"repositoryId": rid, "categoryId": categoryID, "title": title, "body": body,
	}, &out); err != nil {
		return nil, fmt.Errorf("CreateDiscussion: %w", err)
	}
	d := out.CreateDiscussion.Discussion
	if d == nil {
		return nil, errors.New("CreateDiscussion: 返回 discussion 为 null")
	}
	return &Discussion{
		NodeID: d.ID, Number: d.Number, Title: d.Title,
		Body: d.Body, URL: d.URL, CreatedAt: d.CreatedAt,
	}, nil
}

type gqlDiscussionNode struct {
	Typename  string `json:"__typename"`
	ID        string `json:"id"`
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	URL       string `json:"url"`
	CreatedAt string `json:"createdAt"`
	Category  *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"category"`
	Answer *struct {
		ID string `json:"id"`
	} `json:"answer"`
	Comments *struct {
		PageInfo gqlPageInfo  `json:"pageInfo"`
		Nodes    []gqlComment `json:"nodes"`
	} `json:"comments"`
}

// DiscussionByNode 按 node(id:) 取版本帖（回归锁：绝不允许 discussion(id:) 入口）。
func (c *Client) DiscussionByNode(ctx context.Context, nodeID string) (*Discussion, error) {
	n, _, err := c.fetchDiscussionNode(ctx, nodeID, nil)
	if err != nil {
		return nil, fmt.Errorf("DiscussionByNode: %w", err)
	}
	d := &Discussion{
		NodeID: n.ID, Number: n.Number, Title: n.Title,
		Body: n.Body, URL: n.URL, CreatedAt: n.CreatedAt,
	}
	if n.Category != nil {
		d.CategoryID = n.Category.ID
	}
	return d, nil
}

// Thread 单个版本帖（Discussion）的讨论数据：全部评论 + 是否有最佳答案。
type Thread struct {
	Comments  []Comment
	HasAnswer bool
}

// DiscThread 分页拉取版本帖评论与最佳答案标记（复用同一查询文档）。
func (c *Client) DiscThread(ctx context.Context, nodeID string) (Thread, error) {
	var all []Comment
	cursor := any(nil)
	hasAnswer := false
	for page := 0; page < 50; page++ {
		n, pi, err := c.fetchDiscussionNode(ctx, nodeID, cursor)
		if err != nil {
			return Thread{}, fmt.Errorf("DiscThread: %w", err)
		}
		if n.Answer != nil {
			hasAnswer = true
		}
		if n.Comments != nil {
			for _, cn := range n.Comments.Nodes {
				cm, err := cn.toComment()
				if err != nil {
					return Thread{}, err
				}
				all = append(all, cm)
			}
		}
		if !pi.HasNextPage {
			return Thread{Comments: all, HasAnswer: hasAnswer}, nil
		}
		if pi.EndCursor == "" {
			return Thread{}, errors.New("DiscThread: hasNextPage=true 但 endCursor 为空")
		}
		cursor = pi.EndCursor
	}
	return Thread{}, errors.New("DiscThread: 超过 50 页上限")
}

func (c *Client) fetchDiscussionNode(ctx context.Context, nodeID string, cursor any) (*gqlDiscussionNode, gqlPageInfo, error) {
	var out struct {
		Node json.RawMessage `json:"node"`
	}
	if err := c.Raw(ctx, DISCUSSION_NODE_QUERY, map[string]any{"id": nodeID, "cursor": cursor}, &out); err != nil {
		return nil, gqlPageInfo{}, err
	}
	if len(out.Node) == 0 || string(out.Node) == "null" {
		return nil, gqlPageInfo{}, fmt.Errorf("node %s 不存在", nodeID)
	}
	var n gqlDiscussionNode
	if err := json.Unmarshal(out.Node, &n); err != nil {
		return nil, gqlPageInfo{}, err
	}
	if n.Typename != "" && n.Typename != "Discussion" {
		return nil, gqlPageInfo{}, fmt.Errorf("node %s 不是 Discussion（__typename=%s）", nodeID, n.Typename)
	}
	if n.ID == "" {
		return nil, gqlPageInfo{}, fmt.Errorf("node %s 缺 id 字段（查询字段与 schema 不匹配？）", nodeID)
	}
	pi := gqlPageInfo{}
	if n.Comments != nil {
		pi = n.Comments.PageInfo
	}
	return &n, pi, nil
}
