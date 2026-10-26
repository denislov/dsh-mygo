package view

import (
	"strings"
	"testing"

	"dsh-mygo/internal/interaction"
	"github.com/egoist/mygo/ui"
)

func TestCanSendPromptWaitsForBusinessState(t *testing.T) {
	for _, test := range []struct {
		name  string
		block func(*Model)
	}{
		{"starting", func(m *Model) { m.Phase = PhaseStarting }},
		{"running", func(m *Model) { m.Phase = PhaseRunning }},
		{"resyncing", func(m *Model) { m.Phase = PhaseResyncing }},
		{"failed", func(m *Model) { m.Phase = PhaseFailed }},
		{"runtime setup", func(m *Model) { m.Runtime.SetupRequired = true }},
		{"workspace setup", func(m *Model) { m.Setup.WorkspaceRequired = true }},
		{"folder picker", func(m *Model) { m.Setup.Picking = true }},
		{"question", func(m *Model) { m.Questions = []*interaction.QuestionRequest{{}} }},
		{"session job", func(m *Model) { m.SessionBusy = true }},
		{"detached history", func(m *Model) { m.History.Detached = true }},
		{"session uncertain", func(m *Model) { m.SessionPanel.Uncertain = true }},
		{"settings", func(m *Model) { m.SettingsOpen = true }},
		{"settings job", func(m *Model) { m.SettingsBusy = true }},
		{"model uncertain", func(m *Model) { m.ModelUncertain = true }},
		{"configuration uncertain", func(m *Model) { m.ConfigurationUncertain = true }},
		{"permission uncertain", func(m *Model) { m.Permission.Uncertain = true }},
		{"inspection", func(m *Model) { m.Inspection.Open = true }},
		{"closing", func(m *Model) { m.LocalState.Closing = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := NewModel()
			model.Phase = PhaseIdle
			if !model.CanSendPrompt() {
				t.Fatal("idle model is not ready; content and transport belong to callers")
			}
			test.block(model)
			if model.CanSendPrompt() {
				t.Fatal("blocked business state accepted a prompt")
			}
		})
	}
}

func TestCanSendPromptAllowsBackgroundReads(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	model.History.Busy, model.SessionPanel.Refreshing = true, true
	if !model.CanSendPrompt() {
		t.Fatal("background reads became a global input gate")
	}
}

func TestComposerSendAndEnterWaitForConfirmation(t *testing.T) {
	for _, kind := range []string{"model", "configuration", "permission", "history"} {
		t.Run(kind, func(t *testing.T) {
			model := sessionModel()
			sent := 0
			model.Send = func(string) { sent++ }
			tt := tester(model)
			tt.Type("keep draft")
			switch kind {
			case "model":
				model.ModelUncertain = true
			case "configuration":
				model.ConfigurationUncertain = true
			case "permission":
				model.Permission.Uncertain = true
			case "history":
				model.History.Detached = true
			}
			tt.Frame()
			_ = tt.Click("Send")
			tt.Key(0, ui.KeyEnter)
			if sent != 0 || !strings.HasPrefix(model.Draft, "keep draft") {
				t.Fatal("blocked action sent or discarded the draft")
			}
			model.ModelUncertain, model.ConfigurationUncertain, model.Permission.Uncertain, model.History.Detached = false, false, false, false
			tt.Frame()
			if err := tt.Click("Send"); err != nil {
				t.Fatal(err)
			}
			if sent != 1 {
				t.Fatal("confirmed state did not enable sending")
			}
		})
	}
}
