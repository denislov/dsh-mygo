package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/view"
)

func TestAgainstMygoSessionManagement(t *testing.T) {
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
				root = t.TempDir()
				launch = host.Config{Command: node, Args: []string{fixture, "--sessions", "--session-management"}}
			}
			updates := make(chan func(), 512)
			a := New(Options{Launch: launch, CWD: root, Provider: provider, Model: "echo", Post: func(fn func()) { updates <- fn }})
			t.Cleanup(func() { a.Shutdown(10 * time.Second) })
			go a.Start()
			waitFor(t, a, updates, 90*time.Second, func() bool { return a.model.Phase == view.PhaseIdle && !a.model.SessionPanel.Refreshing })
			if !a.model.SessionPanel.CanRename || !a.model.SessionPanel.CanArchive || len(a.model.Sessions) == 0 {
				t.Fatal("startup catalog or management capabilities missing")
			}
			original := a.sessionID
			a.model.Draft = "original draft"
			a.NewSession()
			waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SessionBusy && a.sessionID != original })
			second := a.sessionID
			a.model.Draft = "second draft"
			a.OpenSessionRename(original)
			a.model.SessionPanel.Title = "  Cold conversation  "
			a.RenameSession()
			waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SessionBusy })
			if a.model.SessionPanel.Uncertain || a.model.SessionsError != "" || a.sessionID != second || a.model.Draft != "second draft" {
				t.Fatalf("cold rename changed active session or draft: %s", a.model.SessionsError)
			}
			a.SwitchSession(original)
			waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SessionBusy && a.sessionID == original })
			if a.model.Draft != "original draft" || a.model.ActiveSessionLabel() != "Cold conversation" {
				t.Fatal("cold rename lost draft or title")
			}
			a.OpenSessionRename(original)
			a.model.SessionPanel.Title = "Current conversation"
			a.RenameSession()
			waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SessionBusy })
			if a.model.SessionPanel.Uncertain || a.model.ActiveSessionLabel() != "Current conversation" {
				t.Fatalf("current rename unconfirmed: %s", a.model.SessionsError)
			}
			a.SetSessionArchived(original, true)
			waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SessionBusy })
			if !a.model.SessionPanel.ActiveArchived || a.model.Draft != "original draft" {
				t.Fatalf("archive lost the active draft: %s", a.model.SessionsError)
			}
			a.Send("must not send while archived")
			if a.model.Phase != view.PhaseIdle || a.model.Draft != "original draft" {
				t.Fatal("archived conversation sent a prompt")
			}
			if mode == "real" {
				supervisor := a.currentSupervisor()
				process, err := os.FindProcess(supervisor.PID())
				if err != nil {
					t.Fatal(err)
				}
				if err := process.Kill(); err != nil {
					t.Fatal(err)
				}
				waitFor(t, a, updates, 30*time.Second, func() bool { return a.model.Phase == view.PhaseFailed && a.model.Recoverable })
				a.Restart()
				waitFor(t, a, updates, 30*time.Second, func() bool {
					return a.model.Phase == view.PhaseFailed || a.model.Phase == view.PhaseIdle && a.sessionID != original && !a.model.SessionPanel.Refreshing
				})
				if a.model.Phase == view.PhaseFailed {
					t.Fatalf("archived recovery failed: %s", a.model.RecoveryError)
				}
				if a.model.Draft != "" || a.drafts[original] != "original draft" {
					t.Fatal("archived recovery mixed drafts between conversations")
				}
			}
			a.SetSessionArchived(original, false)
			waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SessionBusy })
			if a.model.SessionPanel.ActiveArchived || a.model.SessionPanel.Uncertain {
				t.Fatal("restore did not unlock the current conversation")
			}
			if mode == "real" {
				a.SwitchSession(original)
				waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SessionBusy && a.sessionID == original })
				if a.model.Draft != "original draft" {
					t.Fatal("restore after recovery lost the archived draft")
				}
			}
			a.Send("continue after restore")
			waitFor(t, a, updates, 30*time.Second, func() bool { return a.model.Phase == view.PhaseIdle && assistantAnswers(a) == 1 })
		})
	}
}
