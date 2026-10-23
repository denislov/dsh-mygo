package view

import (
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestComposerPublishesBoundDraftAndClosingBlocksEditing(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	observed := ""
	model.DraftChanged = func() { observed = model.Draft }
	tester := tester(model)
	if err := tester.Click("Message"); err != nil {
		t.Fatal(err)
	}
	tester.Type("unsent text")
	tester.Frame()
	if observed != "unsent text" {
		t.Fatal("draft callback did not observe the bound editor text")
	}
	model.LocalState.Closing = true
	tester.Frame()
	tester.Type("do not append")
	tester.Frame()
	if model.Draft != "unsent text" {
		t.Fatal("closing allowed edits after snapshot")
	}
}
func TestLocalSaveErrorActionsFitMinimumWindow(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	model.LocalState.Error = "Could not save local drafts; keep this window open and retry"
	retries, discards := 0, 0
	model.LocalState.Retry = func() { retries++ }
	model.LocalState.QuitWithoutSaving = func() { discards++ }
	tester := ui.NewTester(func(c *ui.Context) { View(c, model) }, 560, 420)
	if err := tester.Click("Retry saving drafts"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Quit without saving drafts"); err != nil {
		t.Fatal(err)
	}
	if retries != 1 || discards != 1 {
		t.Fatal("draft recovery actions are unreachable")
	}
}

func TestHostRecoveryStillAllowsExplicitDiscardAfterDraftSaveFailure(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseFailed
	model.RecoveryOpen = true
	model.Error = "The Host stopped"
	model.LocalState.Error = "Could not save local drafts; keep this window open and retry"
	discarded := false
	model.Quit = func() { t.Fatal("ordinary quit bypassed the failed draft save") }
	model.LocalState.QuitWithoutSaving = func() { discarded = true }
	tester := ui.NewTester(func(c *ui.Context) { View(c, model) }, 560, 420)
	if err := tester.Click("Quit without saving drafts"); err != nil {
		t.Fatal(err)
	}
	if !discarded {
		t.Fatal("recovery modal trapped the local save error actions")
	}
}
