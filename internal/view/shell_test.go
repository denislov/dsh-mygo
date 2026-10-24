package view

import (
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/attachment"
	"dsh-mygo/internal/projection"
	"github.com/egoist/mygo/ui"
)

func showDetails(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("Conversation details"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
}

func TestConversationDetailsDiscloseDiagnosticsAndKeepDraft(t *testing.T) {
	model := sessionModel()
	model.Detail = "host-build-fixture"
	model.SessionCWD = "C:/private/workspace"
	model.Draft = "unfinished prompt"
	model.Context = projection.ContextView{Window: 128000}
	tt := tester(model)
	if tt.HasText(model.Detail) || tt.HasText(model.SessionCWD) || tt.HasText("Context 128k") {
		t.Fatal("diagnostics crowd the conversation before disclosure")
	}
	if err := tt.Click("Conversation details"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	for _, text := range []string{model.Detail, model.SessionCWD, "Context 128k"} {
		if !tt.HasText(text) {
			t.Fatalf("details missing %q: %v", text, tt.Texts())
		}
	}
	tt.Key(0, ui.KeyEscape)
	tt.Advance(300 * time.Millisecond)
	if tt.HasText(model.Detail) || model.Draft != "unfinished prompt" {
		t.Fatal("closing details changed the draft or kept the panel")
	}
	model.Phase = PhaseRunning
	tt.Frame()
	if err := tt.Click("Conversation details"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText(model.Detail) {
		t.Fatal("read-only diagnostics unavailable during work")
	}
}

func TestSessionActionsUseNativeMenuAndTargetIdentity(t *testing.T) {
	model := sessionModel()
	model.SessionPanel.CanRename, model.SessionPanel.CanArchive = true, true
	var renamed string
	model.SessionPanel.OpenRename = func(id string) { renamed = id }
	model.SessionPanel.SetArchived = func(string, bool) {}
	tt := tester(model)
	if tt.HasText("Rename") || tt.HasText("Archive") {
		t.Fatal("management actions are always visible")
	}
	if err := tt.Click("Conversation actions · Session two"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatal(err)
	}
	if renamed != "two" {
		t.Fatalf("renamed %q", renamed)
	}
	if err := tt.Click("Conversation actions · Session one"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Archive"); err != nil {
		t.Fatal(err)
	}
	if model.SessionPanel.ArchiveID != "one" || !model.SessionPanel.ArchiveValue {
		t.Fatal("archive confirmation lost target")
	}
	model.SessionPanel.ArchiveID = ""
	model.Phase = PhaseRunning
	tt.Frame()
	_ = tt.Click("Conversation actions · Session two")
	if tt.Menu() != nil {
		t.Fatal("management menu remained active during a turn")
	}
}

func TestConversationDetailsYieldToModalWithoutReopening(t *testing.T) {
	model := sessionModel()
	model.Detail = "runtime-details"
	tt := tester(model)
	showDetails(t, tt)
	if err := tt.Click("Preferences"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if model.Shell.DetailsOpen || tt.HasText(model.Detail) {
		t.Fatal("details compete with preferences")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Advance(300 * time.Millisecond)
	if tt.HasText(model.Detail) {
		t.Fatal("details unexpectedly reopened after modal")
	}
	showDetails(t, tt)
	model.SessionsOpen = true
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	if model.Shell.DetailsOpen {
		t.Fatal("details survived session navigation")
	}
}

func TestCompactShellKeepsComposerReachableWithLongActivity(t *testing.T) {
	model := sessionModel()
	model.Send = func(string) {}
	model.Draft = strings.Repeat("next prompt\n", 30)
	model.Shell.TasksExpanded = true
	model.Attachments.Supported = true
	for range 8 {
		model.Attachments.Pending = append(model.Attachments.Pending, attachment.Draft{MediaType: attachment.MediaPNG, Data: []byte("image")})
	}
	for range 64 {
		model.Tasks = append(model.Tasks, projection.Task{Content: "A long task that should not push input off screen", Status: "pending"})
	}
	tt := ui.NewTester(func(c *ui.Context) { View(c, model) }, 560, 420)
	if err := tt.Click("Send"); err != nil {
		t.Fatalf("minimum window lost composer: %v", err)
	}
}
