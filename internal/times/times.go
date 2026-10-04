// Package times 实现相对时间与文本裁剪（不变量 I-7：now 必须可注入，测试才可重复）。
package times

import (
	"fmt"
	"strings"
	"time"
)

// parseLayouts 按优先级尝试：RFC3339（含小数秒，time.Parse 解析时天然接受）
// → 无时区 T → 空格分隔 → 仅日期。无时区输入按 now 的时区解释。
var parseLayouts = []string{
	time.RFC3339,          // 2006-01-02T15:04:05Z07:00
	"2006-01-02T15:04:05", // Python fromisoformat 朴素形态
	"2006-01-02 15:04:05", // 空格分隔
	"2006-01-02",          // 仅日期
}

// RelativeTime 中文相对时间。分支左闭右开：
//
//	<1m → 刚刚（含未来时间，d<0 落入本分支）
//	<1h → N 分钟前
//	<24h → N 小时前
//	<30d → N 天前
//	否则 → YYYY-MM-DD
//
// 解析失败原样返回 iso。
func RelativeTime(iso string, now time.Time) string {
	t, ok := parse(iso, now)
	if !ok {
		return iso
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d/time.Hour))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d 天前", int(d/(24*time.Hour)))
	default:
		return t.Format("2006-01-02")
	}
}

func parse(iso string, now time.Time) (time.Time, bool) {
	s := strings.TrimSpace(iso)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range parseLayouts {
		// ParseInLocation：带时区偏移的输入以输入为准，无时区输入按 now 的时区解释。
		if t, err := time.ParseInLocation(layout, s, now.Location()); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Clip 空白折叠（所有 Unicode 空白 → 单空格、去首尾）+ 按 rune 截断到 limit，
// 超长追加 "…"（省略号不计入 limit）。limit<=0 只折叠不截断。
// 注意：按 rune 截断，不保证 ZWJ emoji 序列整体性（已知取舍）。
func Clip(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if limit <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}
