package view

import (
	"testing"
	"time"
)

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
