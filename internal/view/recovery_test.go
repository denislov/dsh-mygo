package view

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func recoveryModel() *Model {
	model := NewModel()
	model.Phase = PhaseFailed
	model.RecoveryOpen = true
	model.Recoverable = true
	model.CanOpenLogs = true
	model.Error = "The host exited"
	model.ErrorDetail = "exit status 1"
	return model
}

func TestRecoveryModalOffersRestartLogsAndQuit(t *testing.T) {
	model := recoveryModel()
	restarted, opened, quit := 0, 0, 0
	model.Restart = func() { restarted++ }
	model.OpenLogs = func() { opened++ }
	model.Quit = func() { quit++ }
	tt := tester(model)

	for _, text := range []string{"Host stopped", "The host exited", "exit status 1", "Restart host", "View logs", "Quit"} {
		if !tt.HasText(text) {
			t.Errorf("missing %q in %v", text, tt.Texts())
		}
	}
	if err := tt.Click("View logs"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Quit"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Restart host"); err != nil {
		t.Fatal(err)
	}
	if opened != 1 || quit != 1 || restarted != 1 {
		t.Fatalf("actions: logs=%d quit=%d restart=%d", opened, quit, restarted)
	}
}

func TestRecoveryModalStaysOpenOnEscape(t *testing.T) {
	model := recoveryModel()
	tt := tester(model)
	tt.Key(0, ui.KeyEscape)
	if !model.RecoveryOpen {
		t.Fatal("Escape dismissed the required recovery dialog")
	}
}

func TestResyncPhasePausesTheComposer(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseResyncing
	model.RecoveryOpen = true
	model.Recoverable = true
	model.Error = "The host event stream has a gap"
	sent := 0
	model.Send = func(string) { sent++ }
	tt := tester(model)

	if !tt.HasText("Session needs a restart") || !tt.HasText("Syncing history…") {
		t.Fatalf("resync state missing: %v", tt.Texts())
	}
	tt.Type("hello")
	tt.Key(0, ui.KeyEnter)
	if sent != 0 {
		t.Fatalf("resyncing accepted a prompt: %d", sent)
	}
}

func TestRecoveryModalHiddenWhenHealthy(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	model.RecoveryOpen = true
	tt := tester(model)
	if tt.HasText("Restart host") {
		t.Fatal("recovery dialog rendered for a healthy host")
	}
}
