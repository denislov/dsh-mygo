package view

import (
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/interaction"
	"dsh-mygo/internal/projection"
	"github.com/egoist/mygo/ui"
)

func sessionModel() *Model {
	model := NewModel()
	model.Phase = PhaseIdle
	model.CanHistory = true
	model.SessionID = "one"
	model.Sessions = []SessionSummary{{ID: "one", CreatedAt: 1, Status: "idle"}, {ID: "two", CreatedAt: 2, Status: "stored", CWD: "C:/work"}}
	model.OpenSessions = func() { model.SessionsOpen = true }
	model.CloseSessions = func() { model.SessionsOpen = false }
	return model
}

func TestSessionPickerOpensSelectsAndRestoresFocus(t *testing.T) {
	model := sessionModel()
	selected := ""
	model.SwitchSession = func(id string) { selected = id; model.SessionsOpen = false }
	tester := tester(model)
	if err := clickToolbarMenu(tester, "Sessions"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(300 * time.Millisecond)
	for _, text := range []string{"Conversations", "Current · Session one", "Session two", "C:/work"} {
		if !tester.HasText(text) {
			t.Fatalf("missing %q in %v", text, tester.Texts())
		}
	}
	if err := tester.Click("Session two"); err != nil {
		t.Fatal(err)
	}
	if selected != "two" || model.SessionsOpen {
		t.Fatal("session selection did not reach the app")
	}
	tester.Frame()
	tester.Frame()
	if err := clickToolbarMenu(tester, "Sessions"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(300 * time.Millisecond)
	tester.Key(0, ui.KeyEscape)
	if model.SessionsOpen {
		t.Fatal("Escape did not close the picker")
	}
	model.CanHistory = false
	tester.Frame()
	if tester.HasText("Sessions") {
		t.Fatal("old hosts show the history entry")
	}
}

func TestSessionPickerLoadingErrorsNewAndRefresh(t *testing.T) {
	model := sessionModel()
	model.SessionsOpen = true
	model.SessionBusy = true
	model.SessionSwitching = true
	model.Draft = "draft"
	created, refreshed := 0, 0
	model.NewSession = func() { created++ }
	model.RefreshSessions = func() { refreshed++ }
	model.Send = func(string) { t.Fatal("sent while loading") }
	tester := tester(model)
	if !tester.HasText("Loading session history…") {
		t.Fatal("loading status missing")
	}
	tester.Key(0, ui.KeyEscape)
	_ = tester.Click("New conversation")
	if !model.SessionsOpen || created != 0 || model.Draft != "draft" {
		t.Fatal("loading was dismissed or the draft changed")
	}
	model.SessionBusy = false
	model.SessionSwitching = false
	model.SessionsError = "History load failed. Retry."
	tester.Frame()
	tester.Advance(300 * time.Millisecond)
	if !tester.HasText(model.SessionsError) {
		t.Fatal("history error missing")
	}
	if err := tester.Click("Refresh"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("New conversation"); err != nil {
		t.Fatal(err)
	}
	if created != 1 || refreshed != 1 {
		t.Fatal("picker actions were lost")
	}
}

func TestSessionPickerYieldsToQuestionsAndDisablesNavigationWhileRunning(t *testing.T) {
	model := sessionModel()
	model.SessionsOpen = true
	model.CanQuestion = true
	model.QuestionOpen = true
	model.Questions = []*interaction.QuestionRequest{{RequestID: "q", SessionID: "one"}}
	tester := tester(model)
	if tester.HasText("Conversations") {
		t.Fatal("session picker overlaps a question")
	}
	model.SessionsOpen = false
	model.Questions = nil
	model.Phase = PhaseRunning
	tester.Frame()
	_ = clickToolbarMenu(tester, "Sessions")
	if model.SessionsOpen {
		t.Fatal("navigation was enabled during a turn")
	}
}

func TestSessionSwitchUsesFreshTranscriptAndComposerIdentity(t *testing.T) {
	model := sessionModel()
	model.Draft = "one draft"
	model.Items = []projection.Item{{ID: "assistant:0", Kind: projection.KindAssistant, Title: "Assistant", Text: "old answer"}}
	tester := tester(model)
	model.SessionID = "two"
	model.Draft = "two draft"
	model.ResetTranscript([]projection.Item{{ID: "assistant:0", Kind: projection.KindAssistant, Title: "Assistant", Text: "new answer"}})
	tester.Frame()
	tester.Frame()
	if tester.HasText("old answer") || !tester.HasText("new answer") {
		t.Fatal("a different session inherited transcript widget state")
	}
	tester.Type("!")
	if !strings.Contains(model.Draft, "two draft") || strings.Contains(model.Draft, "one draft") {
		t.Fatalf("stale composer buffer: %q", model.Draft)
	}
}

func TestSessionPickerFitsMinimumWindow(t *testing.T) {
	model := sessionModel()
	model.SessionsOpen = true
	model.NewSession = func() {}
	model.RefreshSessions = func() {}
	tester := ui.NewTester(func(context *ui.Context) { View(context, model) }, 560, 420)
	if err := tester.Click("New conversation"); err != nil {
		t.Fatalf("minimum window cannot reach the new-session action: %v", err)
	}
}
