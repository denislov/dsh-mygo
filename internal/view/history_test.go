package view

import (
	"dsh-mygo/internal/projection"
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestHistoryControlsStayAccessibleAndGateSending(t *testing.T) {
	model := sessionModel()
	model.Draft = "draft"
	model.History = HistoryState{Available: true, Start: 30000, Detached: true}
	earlier, latest, sent := 0, 0, 0
	model.History.LoadEarlier = func() { earlier++ }
	model.History.Latest = func() { latest++; model.History.Detached = false; model.History.Start = 0 }
	model.Send = func(string) { sent++ }
	tester := ui.NewTester(func(c *ui.Context) { View(c, model) }, 560, 420)
	tester.Frame()
	_ = tester.Click("Send")
	if sent != 0 || model.Draft != "draft" {
		t.Fatal("old cache allowed sending")
	}
	if err := tester.Click("Load earlier messages"); err != nil {
		t.Fatal(err)
	}
	model.SessionBusy = true
	model.History.Busy = true
	tester.Frame()
	_ = tester.Click("Load earlier messages")
	if earlier != 1 {
		t.Fatal("duplicate history read")
	}
	model.SessionBusy = false
	model.History.Busy = false
	model.History.Error = "Retry loading earlier messages"
	tester.Frame()
	if !tester.HasText(model.History.Error) {
		t.Fatal("history failure hidden")
	}
	if err := tester.Click("Return to latest"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Frame()
	if latest != 1 || model.History.Detached {
		t.Fatal("latest action lost")
	}
	model.History.Start = 1
	tester.Frame()
	tester.Frame()
	if !tester.HasText("Load earlier messages") {
		t.Fatal("history controls did not return")
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
