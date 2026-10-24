package view

import (
	"testing"
	"time"

	"dsh-mygo/internal/interaction"
	"dsh-mygo/internal/projection"
	"github.com/egoist/mygo/ui"
)

func inspectionModel() *Model {
	m := sessionModel()
	p := &m.Inspection
	p.Available = true
	p.Show = func() {
		p.Open, p.ParentID = true, m.SessionID
		p.Children = []SubagentSummary{{ID: "child", Label: "Inspect files", Mode: "continuable", Status: "stored"}}
	}
	p.Close = func() { p.Open, p.Selected, p.Content = false, "", nil }
	p.Select = func(id string) {
		p.Selected = id
		if id != "" {
			p.Content = NewModel()
			p.Content.Items = []projection.Item{{ID: "child-answer", Title: "Assistant", Kind: projection.KindAssistant, Text: "**Child result**"}}
			p.EndReason, p.HasMore = "completed", true
		}
	}
	p.Refresh = func() { p.Earlier = false; p.Select(p.Selected) }
	p.EarlierPage = func() {
		p.Earlier, p.HasMore = true, false
		p.Content.Items = []projection.Item{{ID: "earlier", Title: "Assistant", Kind: projection.KindAssistant, Text: "Earlier child reply"}}
	}
	return m
}

func TestSubagentPanelOpensDuringWorkCopiesOutputAndKeepsParentDraft(t *testing.T) {
	m := inspectionModel()
	m.Phase, m.Draft = PhaseRunning, "parent draft"
	tt := tester(m)
	if err := tt.Click("Subagents"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if err := tt.Click("Inspect · Inspect files"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("Child result") || !tt.HasText("Completed") {
		t.Fatalf("child output missing: %v", tt.Texts())
	}
	if err := tt.Click("Copy Markdown"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != "**Child result**" || m.Draft != "parent draft" {
		t.Fatal("copy lost source or changed parent")
	}
	m.OpenPreferences()
	if m.SettingsOpen || m.CanRefreshSessions() || m.CanEditSettings() {
		t.Fatal("inspection allowed competing navigation")
	}
	if err := tt.Click("Earlier activity"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("Earlier child reply") {
		t.Fatal("earlier output missing")
	}
	if err := tt.Click("Latest activity"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("Child result") {
		t.Fatal("latest output not restored")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Advance(300 * time.Millisecond)
	if m.Inspection.Open || m.Draft != "parent draft" {
		t.Fatal("closing inspection damaged the parent")
	}
	_ = tt.Click("Subagents")
	tt.Advance(300 * time.Millisecond)
	_ = tt.Click("Inspect · Inspect files")
	tt.Frame()
	tt.Frame()
	if !tt.HasText("Child result") {
		t.Fatal("returning content lost widget identity")
	}
}

func TestSubagentPanelYieldsToQuestionsAndOldHostsHideEntry(t *testing.T) {
	m := inspectionModel()
	m.Inspection.Show()
	m.Inspection.Select("child")
	tt := tester(m)
	m.CanQuestion, m.QuestionOpen = true, true
	m.Questions = []*interaction.QuestionRequest{{RequestID: "q", SessionID: "one"}}
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	if tt.HasText("Read-only tasks and output") || !m.Inspection.Open {
		t.Fatal("inspection overlapped a question or lost its content")
	}
	m.Questions = nil
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("Child result") {
		t.Fatal("inspection did not return after question")
	}
	m.Inspection.Close()
	m.Inspection.Available = false
	tt.Frame()
	tt.Frame()
	if _, ok := tt.Find("Subagents"); ok {
		t.Fatal("old host exposes inspection")
	}
}

func TestSubagentPanelMinimumWindowKeepsCloseRefreshAndParentStopReachable(t *testing.T) {
	m := inspectionModel()
	m.Phase, m.CanCancel = PhaseRunning, true
	m.Cancel = func() { m.CancelPending = true }
	m.Inspection.Show()
	m.Inspection.Select("child")
	m.Inspection.Error = "Could not load subagents. Refresh to retry"
	tt := ui.NewTester(func(c *ui.Context) { View(c, m) }, 560, 420)
	tt.Advance(300 * time.Millisecond)
	for _, label := range []string{"Refresh", "Stop turn", "Close subagents"} {
		if err := tt.Click(label); err != nil {
			t.Fatalf("minimum window: %s: %v", label, err)
		}
	}
	if !m.CancelPending || m.Inspection.Open {
		t.Fatal("footer actions did not reach their owners")
	}
}
