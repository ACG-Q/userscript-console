package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeleteCommentByNumberDELETE路径与头(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	c, err := New("tok123", "o/r", WithRESTEndpoint(ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteCommentByNumber(context.Background(), 42); err != nil {
		t.Fatalf("DeleteCommentByNumber 失败: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("方法应为 DELETE, got %s", gotMethod)
	}
	if want := "/repos/o/r/issues/comments/42"; gotPath != want {
		t.Errorf("路径错误:\n got  %s\n want %s", gotPath, want)
	}
	if want := "Bearer tok123"; gotAuth != want {
		t.Errorf("Authorization 错误: %q, want %q", gotAuth, want)
	}
}

func TestDeleteCommentByNumber非2xx报错(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	defer ts.Close()

	c, err := New("tok123", "o/r", WithRESTEndpoint(ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	err = c.DeleteCommentByNumber(context.Background(), 7)
	if err == nil {
		t.Fatal("500 应返回错误")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("错误应含状态码, got %v", err)
	}
}

func TestDeleteCommentByNumber非法id(t *testing.T) {
	c, err := New("tok123", "o/r")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{0, -1} {
		if err := c.DeleteCommentByNumber(context.Background(), id); err == nil {
			t.Errorf("id=%d 应返回错误", id)
		}
	}
}
