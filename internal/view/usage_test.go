package view

import (
	"testing"
	"time"

	"dsh-mygo/internal/projection"
	"github.com/egoist/mygo/ui"
)

func TestFormatTokensShortensCounts(t *testing.T) {
	cases := map[int]string{0: "0", 999: "999", 1000: "1k", 1200: "1.2k", 12500: "12.5k", 1_000_000: "1M", 1_240_000: "1.2M"}
	for count, want := range cases {
		if got := formatTokens(count); got != want {
			t.Fatalf("formatTokens(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestReadContextUsageClampsAndClassifies(t *testing.T) {
	model := NewModel()
	model.Usage = projection.UsageView{HasUsage: true, InputTokens: 1200, OutputTokens: 350}
	model.Context = projection.ContextView{Window: 128000}
	usage := readContextUsage(model)
	if usage.used != 1550 || usage.percent() != 1 || usage.ratio <= 0 || usage.severity(tokensFor(nil)) != tokensFor(nil).Accent {
		t.Fatalf("usage = %+v", usage)
	}
	model.Usage.InputTokens = 96000
	if usage := readContextUsage(model); usage.severity(tokensFor(nil)) != tokensFor(nil).Warning {
		t.Fatal("75% did not warn")
	}
	model.Usage.InputTokens = 116000
	if usage := readContextUsage(model); usage.severity(tokensFor(nil)) != tokensFor(nil).Danger {
		t.Fatal("90% did not turn dangerous")
	}
	model.Usage.InputTokens = 200000
	if usage := readContextUsage(model); usage.percent() != 100 || usage.ratio != 1 {
		t.Fatalf("over-capacity usage = %+v", usage)
	}
	model.Usage.InputTokens, model.Usage.OutputTokens = -60, 10
	if usage := readContextUsage(model); usage.used != 0 {
		t.Fatalf("negative usage was not clamped: %+v", usage)
	}
	model.Usage.HasUsage = false
	if usage := readContextUsage(model); usage.percent() != 0 || usage.severity(tokensFor(nil)) != tokensFor(nil).TextFaint {
		t.Fatalf("capacity-only usage = %+v", usage)
	}
}

func TestContextUsageChipOpensPopoverAndDismisses(t *testing.T) {
	model := sessionModel()
	model.Usage = projection.UsageView{HasUsage: true, InputTokens: 1200, OutputTokens: 350, CacheReadTokens: 200}
	model.Context = projection.ContextView{Window: 128000}
	tt := tester(model)
	if !tt.HasText("1% · 128k") {
		t.Fatalf("composer chip missing in %v", tt.Texts())
	}
	if err := tt.Click("1% · 128k"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	for _, text := range []string{"Context used", "1%", "1.6k / 128k", "In", "1.2k", "Out", "350", "Cache read", "200", "Free", "Latest turn only"} {
		if !tt.HasText(text) {
			t.Fatalf("context panel missing %q in %v", text, tt.Texts())
		}
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if model.ContextUsageOpen || tt.HasText("Latest turn only") {
		t.Fatal("escape did not dismiss the context panel")
	}
}

func TestContextUsagePanelWhenCapacityOrUsageIsMissing(t *testing.T) {
	model := sessionModel()
	model.Context = projection.ContextView{Window: 64000}
	tt := tester(model)
	if !tt.HasText("64k") {
		t.Fatalf("capacity chip missing in %v", tt.Texts())
	}
	if err := tt.Click("64k"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("Context window") || !tt.HasText("Waiting for the first turn's usage data") {
		t.Fatalf("capacity-only panel wrong in %v", tt.Texts())
	}

	model = sessionModel()
	model.Usage = projection.UsageView{HasUsage: true, InputTokens: 600, OutputTokens: 100}
	tt = tester(model)
	if !tt.HasText("700") {
		t.Fatalf("usage-only chip missing in %v", tt.Texts())
	}
	if err := tt.Click("700"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("Context used") || !tt.HasText("The Host did not report a context window") {
		t.Fatalf("usage-only panel wrong in %v", tt.Texts())
	}
}

func TestContextUsagePanelIsAlsoAvailableInDetails(t *testing.T) {
	model := sessionModel()
	model.Usage = projection.UsageView{HasUsage: true, InputTokens: 1200, OutputTokens: 350}
	model.Context = projection.ContextView{Window: 128000}
	tt := tester(model)
	showDetails(t, tt)
	for _, text := range []string{"Context used", "1.6k / 128k", "In", "Out", "Free"} {
		if !tt.HasText(text) {
			t.Fatalf("details context panel missing %q in %v", text, tt.Texts())
		}
	}
}

func TestContextUsagePopoverAndDetailsShareOneFrame(t *testing.T) {
	model := sessionModel()
	model.Usage = projection.UsageView{HasUsage: true, InputTokens: 1200, OutputTokens: 350}
	model.Context = projection.ContextView{Window: 128000}
	tt := wideTester(model)
	tt.Frame()
	if err := tt.Click("Toggle Session rail"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("1% · 128k"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("Context used") || !tt.HasText("Latest turn only") {
		t.Fatalf("context panels did not share the frame in %v", tt.Texts())
	}
}

func TestContextUsagePanelFitsMinimumWindow(t *testing.T) {
	model := sessionModel()
	model.Usage = projection.UsageView{HasUsage: true, InputTokens: 116000, OutputTokens: 3500}
	model.Context = projection.ContextView{Window: 128000}
	tt := ui.NewTester(func(c *ui.Context) { View(c, model) }, 560, 420)
	if err := tt.Click("93% · 128k"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	rect, ok := tt.Find("Latest turn only")
	if !ok {
		t.Fatalf("context panel note missing in %v", tt.Texts())
	}
	if rect.X < 0 || rect.Y < 0 || rect.X+rect.W > 560 || rect.Y+rect.H > 420 {
		t.Fatalf("context panel note outside the window: %+v", rect)
	}
}

func TestContextUsagePanelClosesWhenMetricsDisappear(t *testing.T) {
	model := sessionModel()
	model.Usage = projection.UsageView{HasUsage: true, InputTokens: 1200, OutputTokens: 350}
	model.Context = projection.ContextView{Window: 128000}
	tt := tester(model)
	if err := tt.Click("1% · 128k"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !model.ContextUsageOpen {
		t.Fatal("context panel did not open")
	}
	model.Usage = projection.UsageView{}
	model.Context = projection.ContextView{}
	tt.Frame()
	if model.ContextUsageOpen || tt.HasText("Latest turn only") {
		t.Fatal("context panel survived without metrics")
	}
}
