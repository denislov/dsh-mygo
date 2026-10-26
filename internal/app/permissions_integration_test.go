package app

import (
	"context"
	"dsh-mygo/internal/host"
	"dsh-mygo/internal/view"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func choosePermission(t *testing.T, a *App, updates <-chan func(), preset string) {
	t.Helper()
	a.OpenSettings()
	waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SettingsBusy })
	if a.model.SettingsError != "" || !a.model.Permission.Ready {
		t.Fatalf("permission read failed: %s", a.model.SettingsError)
	}
	a.model.Permission.Selected = preset
	a.model.Permission.Confirm = true
	a.ApplyPermission()
	waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SettingsBusy })
	if a.model.SettingsError != "" || a.model.Permission.Snapshot.Current != preset || a.model.Permission.Uncertain {
		t.Fatalf("permission write not confirmed: %s", a.model.SettingsError)
	}
	a.CloseSettings()
}
func TestAgainstMygoPermissions(t *testing.T) {
	for _, mode := range []string{"fake", "real"} {
		t.Run(mode, func(t *testing.T) {
			var launch host.Config
			root := t.TempDir()
			provider := "test"
			if mode == "real" {
				launch, root = realMygoLaunch(t, true)
				provider = "mygo-mock"
			} else {
				node, err := exec.LookPath("node")
				if err != nil {
					t.Skip("node unavailable")
				}
				fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
				if err != nil {
					t.Fatal(err)
				}
				launch = host.Config{Command: node, Args: []string{fixture, "--sessions", "--models", "--permissions"}}
			}
			updates := make(chan func(), 512)
			a := New(Options{Launch: launch, CWD: root, Provider: provider, Model: "echo", Post: func(fn func()) { updates <- fn }})
			defer a.Shutdown(10 * time.Second)
			go a.Start()
			waitFor(t, a, updates, 90*time.Second, func() bool { return a.model.Phase == view.PhaseIdle })
			if !a.model.Permission.Available || !a.model.Permission.Ready {
				t.Fatal("permission capability or startup snapshot missing")
			}
			original := a.sessionID
			a.model.Draft = "keep permission draft"
			choosePermission(t, a, updates, "danger-full-access")
			if a.model.Draft != "keep permission draft" {
				t.Fatal("permission write lost draft")
			}
			a.NewSession()
			waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SessionBusy })
			if a.sessionID == original || a.model.Permission.Snapshot.Current != "workspace-write" {
				t.Fatal("session permission changed future defaults")
			}
			a.SwitchSession(original)
			waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SessionBusy })
			if a.model.Permission.Snapshot.Current != "danger-full-access" || a.model.Draft != "keep permission draft" {
				t.Fatal("session switch lost permission or draft")
			}
			if mode == "real" {
				process, err := os.FindProcess(a.currentSupervisor().PID())
				if err != nil {
					t.Fatal(err)
				}
				if err = process.Kill(); err != nil {
					t.Fatal(err)
				}
				waitFor(t, a, updates, 30*time.Second, func() bool { return a.model.Phase == view.PhaseFailed })
				a.Restart()
				waitFor(t, a, updates, 90*time.Second, func() bool { return a.model.Phase == view.PhaseIdle && !a.model.RecoveryOpen })
				if a.sessionID != original || a.model.Permission.Snapshot.Current != "danger-full-access" {
					t.Fatal("durable permission did not survive Host restart")
				}
			}
			choosePermission(t, a, updates, "workspace-write")
			a.Send("continue after permission switch")
			waitFor(t, a, updates, 30*time.Second, func() bool { return a.model.Phase == view.PhaseIdle && assistantAnswers(a) > 0 })
		})
	}
}
func TestAgainstMygoPermissionEnforcement(t *testing.T) {
	for _, preset := range []string{"workspace-write", "danger-full-access"} {
		t.Run(preset, func(t *testing.T) {
			launch, root := realMygoLaunch(t, true)
			updates := make(chan func(), 512)
			a := New(Options{Launch: launch, CWD: root, Provider: "mygo-mock", Model: "approval-probe", Post: func(fn func()) { updates <- fn }})
			defer a.Shutdown(10 * time.Second)
			go a.Start()
			waitFor(t, a, updates, 90*time.Second, func() bool { return a.model.Phase == view.PhaseIdle })
			choosePermission(t, a, updates, preset)
			a.Send("probe explicit approval policy")
			if preset == "workspace-write" {
				waitFor(t, a, updates, 30*time.Second, func() bool { return len(a.model.Approvals) > 0 })
				a.RespondApproval(a.model.Approvals[0].RequestID, "deny")
			}
			waitFor(t, a, updates, 30*time.Second, func() bool { return a.model.Phase == view.PhaseIdle && assistantAnswers(a) > 0 })
			if len(a.model.Approvals) != 0 {
				t.Fatal("never policy asked for approval")
			}
			if !hasAssistantText(a.model.Items, "Approval result: not-executed:rejected") {
				t.Fatal("approval probe did not reach the enforcing service")
			}
			// DSH never rejects operations that explicitly require approval; it never
			// turns a required approval into an implicit grant.
			if _, err := os.Stat(filepath.Join(root, "approval-marker.txt")); !os.IsNotExist(err) {
				t.Fatal("denied approval executed fixture operation")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			snapshot, err := readPermissions(ctx, a.currentClient(), a.sessionID)
			if err != nil || snapshot.Current != preset {
				t.Fatal("execution changed the selected permission")
			}
		})
	}
}
