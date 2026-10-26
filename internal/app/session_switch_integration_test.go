package app

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/view"
)

func assistantAnswers(application *App) int {
	count := 0
	for _, item := range application.model.Items {
		if item.Kind == projection.KindAssistant {
			count++
		}
	}
	return count
}

func TestAgainstMygoSessionSwitching(t *testing.T) {
	for _, mode := range []string{"fake", "real"} {
		t.Run(mode, func(t *testing.T) {
			var launch host.Config
			var root string
			provider := "test"
			if mode == "real" {
				launch, root = realMygoLaunch(t, true)
				provider = "mygo-mock"
			} else {
				node, err := exec.LookPath("node")
				if err != nil {
					t.Skip("node is not installed")
				}
				fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
				if err != nil {
					t.Fatal(err)
				}
				launch = host.Config{Command: node, Args: []string{fixture, "--sessions"}}
				root = t.TempDir()
			}
			start := func() (*App, chan func()) {
				updates := make(chan func(), 512)
				application := New(Options{Launch: launch, CWD: root, Provider: provider, Model: "echo", Post: func(fn func()) { updates <- fn }})
				t.Cleanup(func() { application.Shutdown(10 * time.Second) })
				go application.Start()
				waitFor(t, application, updates, 90*time.Second, func() bool { return application.model.Phase == view.PhaseIdle })
				if !application.model.CanHistory {
					t.Fatal("history was not negotiated")
				}
				return application, updates
			}
			application, updates := start()
			original := application.sessionID
			application.Send("first turn")
			waitFor(t, application, updates, 30*time.Second, func() bool { return application.model.Phase == view.PhaseIdle && assistantAnswers(application) == 1 })
			application.model.Draft = "original draft"
			application.NewSession()
			waitFor(t, application, updates, 30*time.Second, func() bool { return !application.model.SessionBusy && application.sessionID != original })
			created := application.sessionID
			if len(application.model.Items) != 0 || application.model.Draft != "" {
				t.Fatal("new session inherited content")
			}
			application.model.Draft = "new session draft"
			application.OpenSessions()
			waitFor(t, application, updates, 30*time.Second, func() bool { return !application.model.SessionBusy && !application.model.SessionPanel.Refreshing })
			found := false
			for _, session := range application.model.Sessions {
				if session.ID == original {
					found = true
				}
			}
			if !found {
				t.Fatal("session list lost the original conversation")
			}
			application.SwitchSession(original)
			waitFor(t, application, updates, 30*time.Second, func() bool { return !application.model.SessionBusy && application.sessionID == original })
			if assistantAnswers(application) != 1 || application.model.Draft != "original draft" {
				t.Fatal("resume lost history or draft")
			}
			application.Send("second turn")
			waitFor(t, application, updates, 30*time.Second, func() bool { return application.model.Phase == view.PhaseIdle && assistantAnswers(application) == 2 })
			application.SwitchSession(created)
			waitFor(t, application, updates, 30*time.Second, func() bool { return !application.model.SessionBusy && application.sessionID == created })
			if application.model.Draft != "new session draft" || len(application.model.Items) != 0 {
				t.Fatal("second session was not isolated")
			}
			if mode != "real" {
				return
			}
			application.Shutdown(10 * time.Second)
			restarted, restartedUpdates := start()
			restarted.OpenSessions()
			waitFor(t, restarted, restartedUpdates, 30*time.Second, func() bool { return !restarted.model.SessionBusy && !restarted.model.SessionPanel.Refreshing })
			found = false
			for _, session := range restarted.model.Sessions {
				if session.ID == original {
					found = true
				}
			}
			if !found {
				t.Fatal("restarted app did not list the persisted session")
			}
			restarted.SwitchSession(original)
			waitFor(t, restarted, restartedUpdates, 30*time.Second, func() bool { return !restarted.model.SessionBusy && restarted.sessionID == original })
			if assistantAnswers(restarted) != 2 || restarted.model.Draft != "" {
				t.Fatal("restart did not replay history or persisted a local draft")
			}
			restarted.Send("third turn after restart")
			waitFor(t, restarted, restartedUpdates, 30*time.Second, func() bool { return restarted.model.Phase == view.PhaseIdle && assistantAnswers(restarted) == 3 })
		})
	}
}
