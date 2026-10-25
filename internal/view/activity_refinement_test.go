package view

import (
	"slices"
	"testing"
	"time"

	"dsh-mygo/internal/projection"
	"github.com/egoist/mygo/ui"
)

func TestReasoningRowsFoldIndependentlyAndSurviveCommit(t *testing.T) {
	m := activityModel()
	m.Activity = map[string]*ActivityState{"activity:1:1:r:0": {Expanded: true}}
	m.Items[1].Text = "First thought\n\nFull thought body"
	m.Items[1].State = "streaming"
	m.Items = append(m.Items[:3], projection.Item{ID: "r2", Kind: projection.KindReasoning, Text: "Second thought", Turn: 1})
	tt := tester(m)
	if slices.Contains(tt.Texts(), "Full thought body") {
		t.Fatal("thought body should remain folded inside an open activity")
	}
	if !tt.HasText("First thought Full thought body") || !tt.HasText("Second thought") {
		t.Fatalf("missing single-line reasoning summaries: %v", tt.Texts())
	}
	if err := tt.Click("Thought · First thought Full thought body"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !slices.Contains(tt.Texts(), "Full thought body") {
		t.Fatal("thought did not expand")
	}
	if m.Reasoning["r2"].Expanded {
		t.Fatal("expanding a thought opened its neighbor")
	}
	m.Items[1].ID, m.Items[1].State = "committed-reasoning", "done"
	tt.Frame()
	if !slices.Contains(tt.Texts(), "Full thought body") {
		t.Fatal("commit reset reasoning disclosure")
	}
	if err := tt.Click("Thought · First thought Full thought body"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if slices.Contains(tt.Texts(), "Full thought body") {
		t.Fatal("thought did not collapse")
	}
}

func TestReasoningDisclosureIsPrunedAndResetWithTranscript(t *testing.T) {
	m := NewModel()
	m.Reasoning = map[string]*ActivityState{"stable": {Expanded: true}, "old": {Expanded: true}}
	m.PruneTranscriptState([]projection.Item{{ID: "committed", Kind: projection.KindReasoning, ActivityKey: "stable"}})
	if m.Reasoning["stable"] == nil || m.Reasoning["old"] != nil {
		t.Fatal("reasoning state did not follow durable identity")
	}
	m.ResetTranscript(nil)
	if len(m.Reasoning) != 0 {
		t.Fatal("reasoning state leaked to another conversation")
	}
}

func TestCollapsedToolRowShowsDescriptionOrPathWithoutRawPayload(t *testing.T) {
	m := activityModel()
	m.Activity = map[string]*ActivityState{"activity:1:1:r:0": {Expanded: true}}
	m.Items[2].Title = "Tool · exec_command"
	m.Items[2].Text = `{"cmd":"go test ./...","description":"Run focused checks","secret_extra":"do not show"}`
	tt := tester(m)
	if !tt.HasText("Run command") || !tt.HasText("Run focused checks") || tt.HasText("do not show") {
		t.Fatalf("bad command summary: %v", tt.Texts())
	}
	m.Items[2].Title = "Tool · read_file"
	m.Items[2].Text = `{"file_path":"docs/subsystem/ui-shell.md"}`
	tt.Frame()
	if !tt.HasText("docs/subsystem/ui-shell.md") {
		t.Fatal("file summary missing")
	}
	if err := tt.Click("Tool · read_file"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText(m.Items[2].Text) {
		t.Fatal("summary replaced complete arguments")
	}
}

func TestWorkspacePanelsHaveGapsAcrossBothSplitDirections(t *testing.T) {
	m := sessionModel()
	m.Workspace.Open = map[WorkspacePanel]bool{PanelTerminal: true, PanelChanges: true, PanelFiles: true, PanelPreview: true}
	tt := ui.NewTester(func(c *ui.Context) { View(c, m) }, 1600, 900)
	tt.Frame()
	a, ok := tt.Find("Workspace panel · Terminal")
	b, found := tt.Find("Workspace panel · Changes")
	d, other := tt.Find("Workspace panel · Preview")
	if !ok || !found || !other {
		t.Fatal("panel geometry unavailable")
	}
	if b.Y-(a.Y+a.H) < 10 || d.X-(a.X+a.W) < 10 {
		t.Fatalf("panel gap missing: a=%+v b=%+v d=%+v", a, b, d)
	}
}
