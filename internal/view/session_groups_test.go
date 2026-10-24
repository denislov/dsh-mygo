package view

import (
	"testing"
	"time"
)

func TestSessionGroupsFollowCalendarDaysAndPreserveHostOrder(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.Local)
	at := func(daysAgo int) int64 {
		return now.AddDate(0, 0, -daysAgo).UnixMilli()
	}
	model := NewModel()
	model.Sessions = []SessionSummary{
		{ID: "today", Title: "Today", UpdatedAt: at(0)},
		{ID: "yesterday", Title: "Yesterday", UpdatedAt: at(1)},
		{ID: "week", Title: "Week", UpdatedAt: at(4)},
		{ID: "older", Title: "Older", UpdatedAt: at(30)},
		{ID: "unknown", Title: "Unknown"},
	}
	entries := model.sessionListEntries(now)
	var keys []string
	var headers []string
	for _, entry := range entries {
		keys = append(keys, entry.Key)
		if entry.Header {
			headers = append(headers, entry.Group.labelKey())
		}
	}
	wantKeys := []string{
		"header:0", "session:today",
		"header:1", "session:yesterday",
		"header:2", "session:week",
		"header:3", "session:older", "session:unknown",
	}
	if len(keys) != len(wantKeys) {
		t.Fatalf("keys = %v", keys)
	}
	for index := range wantKeys {
		if keys[index] != wantKeys[index] {
			t.Fatalf("keys[%d] = %q, want %q (all %v)", index, keys[index], wantKeys[index], keys)
		}
	}
	wantHeaders := []string{"Today", "Yesterday", "Previous 7 days", "Older"}
	for index := range wantHeaders {
		if headers[index] != wantHeaders[index] {
			t.Fatalf("headers = %v", headers)
		}
	}
}

func TestSessionGroupBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.Local)
	cases := []struct {
		name string
		then time.Time
		want sessionGroup
	}{
		{"later today", now.Add(2 * time.Hour), groupToday},
		{"yesterday evening", time.Date(2026, 10, 23, 23, 0, 0, 0, time.Local), groupYesterday},
		{"seven days", time.Date(2026, 10, 17, 9, 0, 0, 0, time.Local), groupPrevious7},
		{"eight days", time.Date(2026, 10, 16, 9, 0, 0, 0, time.Local), groupOlder},
		{"zero", time.Time{}, groupOlder},
	}
	for _, test := range cases {
		if got := sessionGroupFor(now, test.then); got != test.want {
			t.Errorf("%s: got %v want %v", test.name, got, test.want)
		}
	}
}

func TestRelativeTimeLabels(t *testing.T) {
	model := NewModel()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.Local)
	cases := []struct {
		millis int64
		want   string
	}{
		{0, ""},
		{now.Add(-20 * time.Second).UnixMilli(), "Just now"},
		{now.Add(-5 * time.Minute).UnixMilli(), "5m"},
		{now.Add(-3 * time.Hour).UnixMilli(), "3h"},
		{now.Add(-2 * 24 * time.Hour).UnixMilli(), "2d"},
		{now.Add(-30 * 24 * time.Hour).UnixMilli(), "09-24"},
	}
	for _, test := range cases {
		if got := relativeTimeLabel(model, now, test.millis); got != test.want {
			t.Errorf("millis=%d: got %q want %q", test.millis, got, test.want)
		}
	}
}
