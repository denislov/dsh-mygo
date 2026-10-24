package app

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/preferences"
	"dsh-mygo/internal/view"
)

func TestAgainstMygoFirstUse(t *testing.T) {
	for _, mode := range []string{"fake", "real"} {
		t.Run(mode, func(t *testing.T) {
			initial, project := t.TempDir(), t.TempDir()
			var launch host.Config
			provider := "test"
			if mode == "real" {
				launch, _ = realMygoLaunch(t, true)
				provider = "mygo-mock"
			} else {
				node, err := exec.LookPath("node")
				if err != nil {
					t.Skip("node unavailable")
				}
				fixture, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
				launch = host.Config{Command: node, Args: []string{fixture, "--sessions", "--models", "--configuration"}, Dir: initial}
			}
			updates := make(chan func(), 512)
			state := preferences.StatePath(t.TempDir(), initial, "first-use")
			a := New(Options{Launch: launch, CWD: initial, Provider: provider, Model: "echo", RequireWorkspace: true, PreferencesPath: state, PickDirectory: func(string) (string, error) { return project, nil }, Post: func(fn func()) { updates <- fn }})
			defer a.Shutdown(10 * time.Second)
			go a.Start()
			waitFor(t, a, updates, 90*time.Second, func() bool { return a.model.Phase == view.PhaseIdle })
			if !a.model.Setup.WorkspaceRequired || !a.model.ConfigurationReady {
				t.Fatal("first use readiness missing")
			}
			a.model.Draft = "keep initial draft"
			a.Send(a.model.Draft)
			if a.model.Phase != view.PhaseIdle || a.model.Draft != "keep initial draft" {
				t.Fatal("sent before folder confirmation")
			}
			original := a.model.SessionID
			a.ChooseProjectFolder()
			waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.Setup.Picking && !a.model.SessionBusy })
			if a.model.SessionCWD != project || a.model.Setup.WorkspaceRequired || a.drafts[original] != "keep initial draft" {
				t.Fatal("folder setup changed old session or lost selection")
			}
			a.OpenConnectionSettings()
			waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SettingsBusy })
			if a.model.SettingsError != "" || !a.model.ConfigurationReady {
				t.Fatal("connection status unavailable")
			}
			a.CloseSettings()
			a.Send("first use probe")
			waitFor(t, a, updates, 30*time.Second, func() bool { return a.model.Phase == view.PhaseIdle && len(a.model.Items) > 0 })
			id := a.model.SessionID
			a.Shutdown(10 * time.Second)
			if mode != "real" {
				return
			}
			coldUpdates := make(chan func(), 512)
			cold := New(Options{Launch: launch, CWD: initial, Provider: provider, Model: "echo", RequireWorkspace: true, PreferencesPath: state, Post: func(fn func()) { coldUpdates <- fn }})
			defer cold.Shutdown(10 * time.Second)
			go cold.Start()
			waitFor(t, cold, coldUpdates, 90*time.Second, func() bool { return cold.model.Phase == view.PhaseIdle })
			if cold.model.SessionID != id || cold.model.SessionCWD != project || cold.model.Setup.WorkspaceRequired {
				t.Fatal("restart did not restore the configured project")
			}
		})
	}
}
