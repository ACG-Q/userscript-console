package parser

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		ok      bool
		command string
		args    string
	}{
		{"标准命令", "/add https://example.com/a.user.js", true, "add", "https://example.com/a.user.js"},
		{"仅命令", "/list", true, "list", ""},
		{"斜杠后空格仍识别", "/  sync-all   ", true, "sync-all", ""},
		{"sync-all 完整", "/sync-all", true, "sync-all", ""},
		{"前导空行", "\n\n/list", true, "list", ""},
		{"CRLF 首行", "/info\r\nnext", true, "info", ""},
		{"非命令文本", "这是一条评论", false, "", ""},
		{"斜杠后无命令字符", "/你好", false, "", ""},
		{"空 body", "", false, "", ""},
		{"只有斜杠", "/", false, "", ""},
		{"内联参数", "/enable 3f45ee3c-1", true, "enable", "3f45ee3c-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.body)
			if got.Ok != tt.ok {
				t.Fatalf("Ok = %v, want %v", got.Ok, tt.ok)
			}
			if tt.ok && got.Command != tt.command {
				t.Errorf("Command = %q, want %q", got.Command, tt.command)
			}
			if tt.ok && got.Args != tt.args {
				t.Errorf("Args = %q, want %q", got.Args, tt.args)
			}
		})
	}
}

func TestExtractCodeBlocks(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{"无代码块", "plain\n/text", nil},
		{"单个围栏", "```js\nconst a = 1;\n```", []string{"const a = 1;"}},
		{"围栏前有命令行", "/add\n```\ncode line\n```", []string{"code line"}},
		{"多块有序", "```\na\n```\ntext\n~~~\nb\n~~~", []string{"a", "b"}},
		{"信息串不入内容", "```js title=x\nbody\n```", []string{"body"}},
		{"未闭合延伸到文末", "```\nopen\nstill", []string{"open\nstill"}},
		{"关闭围栏须同字符", "```\ncontent\n~~~\nmore\n```", []string{"content\n~~~\nmore"}},
		{"缩进围栏", "  ```\n  indented\n  ```", []string{"  indented"}},
		{"短横不视为围栏", "- ```no", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCodeBlocks(tt.body)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractCodeBlocks = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestRemoveCodeBlocks(t *testing.T) {
	body := "/add\n```\nsecret code\n```\n保留这段说明"
	got := RemoveCodeBlocks(body)
	if strings.Contains(got, "secret code") {
		t.Error("代码块未被移除")
	}
	if !strings.Contains(got, "保留这段说明") {
		t.Error("正文被误删")
	}
	if !strings.Contains(got, "/add") {
		t.Error("命令行应保留")
	}
}

func TestParseCarriesCode(t *testing.T) {
	// /add 空参 + 代码块：命令层从 Code[0] 取源码（I-4 复活契约的输入路径）。
	c := Parse("/add\n```js\n// src\n```")
	if !c.Ok || c.Command != "add" || c.Args != "" {
		t.Fatalf("解析错误: %+v", c)
	}
	if len(c.Code) != 1 || c.Code[0] != "// src" {
		t.Fatalf("Code = %#v", c.Code)
	}
}
