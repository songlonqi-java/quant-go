package news

import (
	"testing"
	"time"

	"quant/internal/data"
)

func TestNewsFromRecentCalendarDaysUsesTodayAndYesterday(t *testing.T) {
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	items := []data.NewsItem{
		{Datetime: "2026-08-01 08:00:00", Title: "人工智能人工智能"},
		{Datetime: "2026-07-31T20:00:00+08:00", Title: "人工智能人工智能"},
		{Datetime: "20260730 23:59:59", Title: "应被排除"},
		{Datetime: "unknown", Title: "无日期"},
	}
	recent := newsFromRecentCalendarDays(items, now, 2)
	if len(recent) != 2 {
		t.Fatalf("recent=%+v", recent)
	}
	topics := extractKeywords(recent, 10)
	if len(topics) == 0 {
		t.Fatal("expected a repeated topic from the recent news")
	}
}

func TestNewsFromRecentCalendarDaysRejectsInvalidWindow(t *testing.T) {
	if got := newsFromRecentCalendarDays([]data.NewsItem{{Datetime: "20260801"}}, time.Now(), 0); got != nil {
		t.Fatalf("recent=%+v, want nil", got)
	}
}
