package view

import (
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/interaction"
	"dsh-mygo/internal/projection"
	"github.com/egoist/mygo/ui"
)

func toolModel() *Model {
	model := NewModel()
	model.Phase = PhaseIdle
	model.Items = []projection.Item{{ID: "tool:c1", Kind: projection.KindTool, Title: "Tool · edit", Text: "arguments preview", Detail: "result preview", State: "done", Tool: &projection.ToolDetails{Arguments: "full arguments", Result: "full result", Diffs: []projection.FileDiff{{Path: "a.txt", OldText: "old", NewText: "new"}, {Path: "a.txt", OldText: "second old", NewText: "second new"}}}}}
	model.Activity = map[string]*ActivityState{"activity:tool:c1": {Expanded: true}}
	return model
}

func TestToolCardsStartCollapsedAndKeepIdentityAcrossUpdates(t *testing.T) {
	model := toolModel()
	tester := tester(model)
	if tester.HasText("arguments preview") || tester.HasText("result preview") {
		t.Fatal("tool details not collapsed")
	}
	if err := tester.Click("Tool · edit"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !tester.HasText("arguments preview") || !tester.HasText("result preview") {
		t.Fatal("expanded details missing")
	}
	model.Items[0].State = "error"
	model.Items[0].IsError = true
	tester.Frame()
	if !model.ToolCards["tool:c1"].Expanded {
		t.Fatal("result update reset disclosure")
	}
	model.Items = append([]projection.Item{{ID: "user:1", Kind: projection.KindUser, Title: "You", Text: "Earlier"}}, model.Items...)
	tester.Frame()
	if !model.ToolCards["tool:c1"].Expanded {
		t.Fatal("row movement reset disclosure")
	}
}

func TestToolInspectorSwitchesHunksReadOnlyAndCloses(t *testing.T) {
	model := toolModel()
	tester := tester(model)
	if err := tester.Click("Tool · edit"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(300 * time.Millisecond)
	if err := tester.Click("View details"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !tester.HasText("full arguments") {
		t.Fatalf("full arguments unavailable: %v", tester.Texts())
	}
	if err := tester.Click("Result"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("full result") {
		t.Fatal("full result unavailable")
	}
	if err := tester.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("a.txt") || model.ToolInspector.Diff == nil || model.ToolInspector.Diff.Editable || model.ToolInspector.Diff.HunkActions {
		t.Fatal("missing or editable diff")
	}
	if err := tester.Click("Next change"); err != nil {
		t.Fatal(err)
	}
	if model.ToolInspector.Diff.Old.String() != "second old" || model.ToolInspector.Diff.New.String() != "second new" {
		t.Fatal("hunk navigation mixed source")
	}
	tester.Key(0, ui.KeyEscape)
	if model.ToolInspector != nil {
		t.Fatal("dismissed inspector retained buffers")
	}
}

func TestToolInspectorLargeDiffUsesExplicitTextFallback(t *testing.T) {
	model := toolModel()
	model.Items[0].Tool.Diffs = []projection.FileDiff{{Path: "large.txt", OldText: strings.Repeat("old\n", 600), NewText: "new"}}
	model.OpenToolDetails(model.Items[0].ID)
	model.ToolInspector.Tab = 2
	tester := tester(model)
	if !tester.HasText("Large hunk: read-only before/after text (not a computed diff).") || model.ToolInspector.Diff != nil {
		t.Fatal("large diff computed on UI thread")
	}
}

func TestToolInspectorUpdatesAndEvictionDoNotRetainOldBuffers(t *testing.T) {
	model := toolModel()
	model.OpenToolDetails(model.Items[0].ID)
	model.ToolInspector.Tab = 2
	tester := tester(model)
	model.Items[0].Tool = &projection.ToolDetails{Diffs: []projection.FileDiff{{Path: "updated.txt", OldText: "changed old", NewText: "changed new"}}}
	tester.Frame()
	if model.ToolInspector.Diff.New.String() != "changed new" || !tester.HasText("updated.txt") {
		t.Fatal("result update left an old diff")
	}
	model.Items[0].Tool = nil
	model.Items[0].ToolEvicted = true
	tester.Frame()
	if model.ToolInspector.Diff != nil || model.ToolInspector.source != nil || !tester.HasText("Full details evicted from the bounded cache; only previews remain.") {
		t.Fatal("evicted details retained by inspector")
	}
}

func TestToolInspectorYieldsToHumanInteraction(t *testing.T) {
	model := toolModel()
	model.OpenToolDetails(model.Items[0].ID)
	model.CanApprove = true
	model.ApprovalOpen = true
	model.Approvals = []interaction.Approval{{RequestID: "approval", SessionID: "s1", ToolName: "write", External: true}}
	tester := tester(model)
	if !tester.HasText("Tool approval") || tester.HasText("Tool details") {
		t.Fatal("inspector obscured pending approval")
	}
	model.Approvals = nil
	model.ApprovalOpen = false
	tester.Frame()
	if !tester.HasText("Tool details") {
		t.Fatal("inspector did not return")
	}
}
