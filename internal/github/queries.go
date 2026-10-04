package github

import (
	"fmt"
	"strconv"
	"strings"
)

// 13 个固定 GraphQL 查询文档（D-05：固定文档走常量 + 字段回归锁测试；
// 动态批量统计走 BuildStatsQuery 模板，不 codegen）。
// 每个常量上方注释其消费者。
//
// ⚠️ 外部事实备注：DELETE_COMMENT_MUTATION 的根字段按 `deleteComment` 实现，
// 待接入真实 schema 校验工具（CI）后复核（测试按常量自身根字段锁定，不依赖外部真值）。

const (
	// REPO_QUERY —— GetIssue / repoID()（CREATE_* 系列取 repositoryId）。
	// issue 恰 9 个字段，对应 gqlIssue 结构（§3.2 字段正确性）。
	REPO_QUERY = `
query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    id
    nameWithOwner
    issue(number: $number) {
      id
      number
      title
      body
      state
      createdAt
      updatedAt
      author { login }
      comments { totalCount }
    }
  }
}`

	// LABELS_QUERY —— projector 经 Raw 直调（校验/创建标签）。
	LABELS_QUERY = `
query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    id
    labels(first: 100) {
      nodes { id name color description }
    }
  }
}`

	// LIST_QUERY —— ListRepoIssues（分页 + 状态过滤，updatedAt 倒序）。
	LIST_QUERY = `
query($owner: String!, $name: String!, $states: [IssueState!], $cursor: String) {
  repository(owner: $owner, name: $name) {
    issues(first: 100, after: $cursor, states: $states, orderBy: {field: UPDATED_AT, direction: DESC}) {
      totalCount
      pageInfo { hasNextPage endCursor }
      nodes {
        id
        number
        title
        body
        state
        createdAt
        updatedAt
        author { login }
        comments { totalCount }
      }
    }
  }
}`

	// CREATE_LABEL_MUTATION —— CreateLabel（label 变量命名避让 $name 注入）。
	CREATE_LABEL_MUTATION = `
mutation($repositoryId: ID!, $labelName: String!, $color: String!, $labelDescription: String) {
  createLabel(input: {repositoryId: $repositoryId, name: $labelName, color: $color, description: $labelDescription}) {
    label { id name }
  }
}`

	// CREATE_MUTATION —— CreateIssue。
	CREATE_MUTATION = `
mutation($repositoryId: ID!, $title: String!, $body: String) {
  createIssue(input: {repositoryId: $repositoryId, title: $title, body: $body}) {
    issue {
      id number title body state createdAt updatedAt
      author { login }
      comments { totalCount }
    }
  }
}`

	// UPDATE_MUTATION —— UpdateIssue（幂等投影的写路径）。
	UPDATE_MUTATION = `
mutation($issueId: ID!, $title: String!, $body: String!) {
  updateIssue(input: {id: $issueId, title: $title, body: $body}) {
    issue {
      id number title body state createdAt updatedAt
      author { login }
      comments { totalCount }
    }
  }
}`

	// CLOSE_MUTATION —— CloseIssue（墓碑化）。
	CLOSE_MUTATION = `
mutation($issueId: ID!) {
  closeIssue(input: {id: $issueId}) {
    issue { id number state }
  }
}`

	// REOPEN_MUTATION —— ReopenIssue（复活重开）。
	REOPEN_MUTATION = `
mutation($issueId: ID!) {
  reopenIssue(input: {id: $issueId}) {
    issue { id number state }
  }
}`

	// DISCUSSION_CATEGORIES_QUERY —— DiscussionCategories（选默认分类）。
	DISCUSSION_CATEGORIES_QUERY = `
query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    discussionCategories(first: 25) {
      nodes { id name slug }
    }
  }
}`

	// CREATE_DISCUSSION_MUTATION —— CreateDiscussion（版本帖发布）。
	CREATE_DISCUSSION_MUTATION = `
mutation($repositoryId: ID!, $categoryId: ID!, $title: String!, $body: String!) {
  createDiscussion(input: {repositoryId: $repositoryId, categoryId: $categoryId, title: $title, body: $body}) {
    discussion { id number title body url createdAt }
  }
}`

	// DISCUSSION_NODE_QUERY —— DiscussionByNode（cursor=null）与 DiscussionComments（分页）共用。
	// ⚠️ 回归锁（SPEC-ARCH-TEST §3.1）：必须是 node(id:) 入口，绝不允许 discussion(id:)。
	DISCUSSION_NODE_QUERY = `
query($id: ID!, $cursor: String) {
  node(id: $id) {
    ... on Discussion {
      __typename
      id
      number
      title
      body
      url
      createdAt
      category { id name slug }
      comments(first: 100, after: $cursor) {
        pageInfo { hasNextPage endCursor }
        nodes { id author { login } body createdAt }
      }
    }
  }
}`

	// PANEL_QUERY —— ListIssueComments（清理器分页拉命令面板评论）。
	PANEL_QUERY = `
query($owner: String!, $name: String!, $number: Int!, $cursor: String) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
      comments(first: 100, after: $cursor) {
        pageInfo { hasNextPage endCursor }
        nodes { id author { login } body createdAt }
      }
    }
  }
}`

	// DELETE_COMMENT_MUTATION —— DeleteComment（清理器）。
	DELETE_COMMENT_MUTATION = `
mutation($commentId: ID!) {
  deleteComment(input: {id: $commentId}) {
    clientMutationId
  }
}`
)

// BuildStatsQuery 动态别名模板（D-05 保留模板）：
// 生成 n 个唯一别名 i0..i(n-1) 的 node(id:) 查询，Issue/Discussion 双片段取评论数与创建时间。
func BuildStatsQuery(n int) (string, error) {
	if n < 1 {
		return "", fmt.Errorf("BuildStatsQuery: n 必须 ≥1，实际 %d", n)
	}
	vars := make([]string, 0, n)
	var body strings.Builder
	for i := 0; i < n; i++ {
		id := strconv.Itoa(i)
		vars = append(vars, "$id"+id)
		body.WriteString("  i" + id + ": node(id: $id" + id + ") {\n" +
			"    ... on Issue { id comments { totalCount } createdAt }\n" +
			"    ... on Discussion { id comments { totalCount } createdAt }\n" +
			"  }\n")
	}
	return "query(" + strings.Join(vars, ", ") + ") {\n" + body.String() + "}", nil
}
