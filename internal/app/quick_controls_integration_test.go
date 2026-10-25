package app

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/view"
	"github.com/egoist/mygo/ui"
)

func TestQuickControlsApplyThroughHostAndPreserveSessionDraft(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	fixture, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
	updates := make(chan func(), 512)
	a := New(Options{Launch: host.Config{Command: node, Args: []string{fixture, "--sessions", "--models", "--permissions"}}, CWD: t.TempDir(), Provider: "test", Model: "echo", Post: func(fn func()) { updates <- fn }})
	t.Cleanup(func() { a.Shutdown(10 * time.Second) })
	go a.Start()
	waitFor(t, a, updates, 20*time.Second, func() bool { return a.model.Phase == view.PhaseIdle && !a.model.SessionPanel.Refreshing })
	a.model.Draft = "keep quick-control draft"
	id := a.sessionID
	tt := ui.NewTester(func(c *ui.Context) { view.View(c, a.model) }, 1280, 820)
	if err := tt.Click("Choose model"); err != nil {
		t.Fatal(err)
	}
	completeSettingsJob(t, a, updates)
	tt.Advance(300 * time.Millisecond)
	if !a.model.SettingsOpen || a.model.QuickSelector != "model" || tt.HasText("Model ID") {
		t.Fatal("quick model opened the settings center")
	}
	if err := tt.Click("Use model · Route probe"); err != nil {
		t.Fatal(err)
	}
	completeSettingsJob(t, a, updates)
	if a.model.ActiveRoute.Model != "route-probe" || a.model.SettingsOpen || a.model.QuickSelector != "" {
		t.Fatalf("model not confirmed: %s", a.model.SettingsError)
	}
	tt.Frame()
	if err := tt.Click("Choose permissions"); err != nil {
		t.Fatal(err)
	}
	completeSettingsJob(t, a, updates)
	tt.Advance(300 * time.Millisecond)
	if err := tt.Click("Permission option · Full access"); err != nil {
		t.Fatal(err)
	}
	if !a.model.Permission.Writing || a.model.Permission.Snapshot.Current == "danger-full-access" {
		t.Fatal("mode click did not start a write or applied before Host confirmation")
	}
	completeSettingsJob(t, a, updates)
	if a.model.Permission.Snapshot.Current != "danger-full-access" || a.model.SettingsOpen || a.model.QuickSelector != "" {
		t.Fatalf("permission not confirmed: %s", a.model.SettingsError)
	}
	if a.sessionID != id || a.model.Draft != "keep quick-control draft" {
		t.Fatal("quick controls replaced session or lost draft")
	}
}
