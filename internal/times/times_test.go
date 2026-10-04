package times

import (
	"strings"
	"testing"
	"time"
)

func TestRelativeTime单位分支(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		ago  time.Duration
		want string
	}{
		{"刚刚-10秒", 10 * time.Second, "刚刚"},
		{"刚刚-59秒", 59 * time.Second, "刚刚"},
		{"恰60秒是1分钟前", 60 * time.Second, "1 分钟前"},
		{"59分钟", 59 * time.Minute, "59 分钟前"},
		{"恰60分是1小时前", 60 * time.Minute, "1 小时前"},
		{"23小时", 23 * time.Hour, "23 小时前"},
		{"恰24h是1天前", 24 * time.Hour, "1 天前"},
		{"7天", 7 * 24 * time.Hour, "7 天前"},
		{"29天", 29 * 24 * time.Hour, "29 天前"},
		{"恰30天落日期分支", 30 * 24 * time.Hour, "2026-09-05"},
		{"远超30天", 100 * 24 * time.Hour, "2026-06-27"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iso := now.Add(-tt.ago).Format(time.RFC3339)
			if got := RelativeTime(iso, now); got != tt.want {
				t.Errorf("RelativeTime(%q) = %q, want %q", iso, got, tt.want)
			}
		})
	}
}

func TestRelativeTime未来时间为刚刚(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	future := now.Add(48 * time.Hour).Format(time.RFC3339)
	if got := RelativeTime(future, now); got != "刚刚" {
		t.Errorf("未来时间 = %q, want 刚刚", got)
	}
}

func TestRelativeTime容错解析(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		iso  string
		want string
	}{
		{"RFC3339带偏移", "2026-10-05T11:00:00+08:00", "9 小时前"}, // = UTC 03:00，now=12:00 UTC → 9h 前
		{"小数秒", "2026-10-05T11:59:30.123Z", "刚刚"},
		{"无时区T", "2026-10-05T11:59:30", "刚刚"},
		{"空格分隔", "2026-10-05 11:59:30", "刚刚"},
		{"仅日期", "2026-10-05", "12 小时前"}, // 零点 → now 12:00 → 12h
		{"首尾空白", "  2026-10-05T11:59:30  ", "刚刚"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RelativeTime(tt.iso, now); got != tt.want {
				t.Errorf("RelativeTime(%q) = %q, want %q", tt.iso, got, tt.want)
			}
		})
	}
}

func TestRelativeTime解析失败原样返回(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	for _, iso := range []string{"", "  ", "未知时间", "2024-13-40", "25:00:00", "2026-10-05T"} {
		if got := RelativeTime(iso, now); got != iso {
			t.Errorf("RelativeTime(%q) = %q, 应原样返回", iso, got)
		}
	}
}

func TestRelativeTime注入now决定输出(t *testing.T) {
	iso := "2026-10-05T11:59:30Z"
	if got := RelativeTime(iso, time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)); got != "刚刚" {
		t.Errorf("now=12:00 → %q", got)
	}
	if got := RelativeTime(iso, time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)); got != "1 天前" {
		t.Errorf("now=次日12:00 → %q", got)
	}
}

func TestRelativeTime无时区按now时区解释(t *testing.T) {
	zone := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, zone)
	// 朴素时间按 +8 解释 → 与 now 同时区差 1 小时 → 1 小时前
	if got := RelativeTime("2026-10-05 11:00:00", now); got != "1 小时前" {
		t.Errorf("got %q, want 1 小时前", got)
	}
}

func TestClip折叠与截断(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{"空串", "", 5, ""},
		{"全空白", "   \t\n  ", 5, ""},
		{"折叠空白", "  a \n b\tc  ", 0, "a b c"},
		{"Unicode空白含NBSP", "a b", 0, "a b"},
		{"不超限原样", "hello", 10, "hello"},
		{"超限截断", "abcdefghij", 3, "abc…"},
		{"中文按rune截断", "中文四字", 2, "中文…"},
		{"limit为0只折叠", "a   b", 0, "a b"},
		{"limit为负只折叠", "a   b", -1, "a b"},
		{"恰等长不加省略", "abc", 3, "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Clip(tt.in, tt.limit); got != tt.want {
				t.Errorf("Clip(%q, %d) = %q, want %q", tt.in, tt.limit, got, tt.want)
			}
		})
	}
}

func TestClip截断不产生非法UTF8(t *testing.T) {
	got := Clip("中文测试文本", 3)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("应以省略号结尾: %q", got)
	}
	if strings.ToValidUTF8(got, "") != got {
		t.Errorf("非法 UTF-8: %q", got)
	}
}
