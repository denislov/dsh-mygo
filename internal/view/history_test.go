package view

import (
	"dsh-mygo/internal/projection"
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestDetachedHistoryUsesArrowAndRetryWithoutSending(t *testing.T) {
	model := sessionModel()
	model.Draft = "draft"
	model.History = HistoryState{Available: true, Start: 30000, Detached: true}
	earlier, latest, sent := 0, 0, 0
	model.History.Latest = func() { latest++; model.History.Detached = false; model.History.Start = 0 }
	model.Send = func(string) { sent++ }
	tt := ui.NewTester(func(c *ui.Context) { View(c, model) }, 560, 420)
	_ = tt.Click("Send")
	if sent != 0 || model.Draft != "draft" {
		t.Fatal("old cache allowed sending")
	}
	if err := tt.Click("Return to latest"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if latest != 1 || model.History.Detached {
		t.Fatal("latest action lost")
	}
	model.History.Start = 64
	model.History.Detached = true
	model.History.Error = "Could not load history. Retry loading earlier messages"
	model.History.LoadEarlier = func() { earlier++; model.History.Busy = true }
	tt.Frame()
	if !tt.HasText(model.History.Error) {
		t.Fatal("history failure hidden")
	}
	if err := tt.Click("Retry loading earlier messages"); err != nil {
		t.Fatal(err)
	}
	_ = tt.Click("Retry loading earlier messages")
	if earlier != 1 {
		t.Fatal("retry duplicated history read")
	}
}

func TestPruneHistoryDropsOnlyEvictedWidgetBuffers(t *testing.T) {
	model := sessionModel()
	model.ToolCards = map[string]*ToolCardState{"keep": {Expanded: true}, "evicted": {Expanded: true}}
	model.ToolInspector = &ToolInspectorState{ID: "evicted"}
	model.CopiedMessageID = "evicted"
	model.CopiedMessageText = "old source"
	model.PruneTranscriptState([]projection.Item{{ID: "keep"}})
	if len(model.ToolCards) != 1 || !model.ToolCards["keep"].Expanded || model.ToolInspector != nil || model.CopiedMessageText != "" {
		t.Fatal("evicted widget buffers survived")
	}
	model.SessionPanel.ActiveArchived = true
	model.History.Available = true
	if !model.CanLoadHistory() {
		t.Fatal("archived conversation lost readable history")
	}
}
