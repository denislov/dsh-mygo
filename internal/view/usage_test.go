package view

import (
	"testing"

	"dsh-mygo/internal/projection"
)

func TestFormatTokensShortensCounts(t *testing.T) {
	cases := map[int]string{0: "0", 999: "999", 1000: "1k", 1200: "1.2k", 12500: "12.5k", 1_000_000: "1M", 1_240_000: "1.2M"}
	for count, want := range cases {
		if got := formatTokens(count); got != want {
			t.Fatalf("formatTokens(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestUsageContextLabelShowsCapacityAndStepUsage(t *testing.T) {
	model := NewModel()
	model.Usage = projection.UsageView{HasUsage: true, InputTokens: 1200, OutputTokens: 350, CacheReadTokens: 200}
	model.Context = projection.ContextView{Provider: "deepseek-official", Model: "deepseek-flash", Window: 128000}
	label := usageContextLabel(model)
	want := "Context 1.6k / 128k · 1% · In 1.2k · Out 350 · cache 200"
	if label != want {
		t.Fatalf("label = %q, want %q", label, want)
	}
	model.Prefs.Value.Locale = "zh-CN"
	want = "上下文 1.6k / 128k · 1% · 输入 1.2k · 输出 350 · 缓存 200"
	if label := usageContextLabel(model); label != want {
		t.Fatalf("label = %q, want %q", label, want)
	}
}

func TestUsageContextLabelHandlesPartialMetrics(t *testing.T) {
	model := NewModel()
	if label := usageContextLabel(model); label != "" {
		t.Fatalf("empty metrics rendered %q", label)
	}
	model.Context = projection.ContextView{Window: 64000}
	if label := usageContextLabel(model); label != "Context 64k" {
		t.Fatalf("label = %q", label)
	}
	model.Context = projection.ContextView{}
	model.Usage = projection.UsageView{HasUsage: true, InputTokens: 600, OutputTokens: 100}
	if label := usageContextLabel(model); label != "In 600 · Out 100" {
		t.Fatalf("label = %q", label)
	}
}

func TestHeaderShowsUsageContextRow(t *testing.T) {
	model := sessionModel()
	model.Usage = projection.UsageView{HasUsage: true, InputTokens: 1200, OutputTokens: 350}
	model.Context = projection.ContextView{Provider: "deepseek-official", Model: "deepseek-flash", Window: 128000}
	tt := tester(model)
	showDetails(t, tt)
	if !tt.HasText("Context 1.6k / 128k · 1% · In 1.2k · Out 350") {
		t.Fatalf("metrics row missing in %v", tt.Texts())
	}
	model.Usage = projection.UsageView{}
	model.Context = projection.ContextView{}
	tt.Frame()
	if tt.HasText("Context") {
		t.Fatalf("empty metrics row still shown in %v", tt.Texts())
	}
}
