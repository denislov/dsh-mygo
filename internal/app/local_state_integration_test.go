package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"dsh-mygo/internal/preferences"
	"dsh-mygo/internal/view"
)

func TestAgainstMygoLocalEditingState(t *testing.T) {
	launch, root := realMygoLaunch(t, true)
	path := filepath.Join(t.TempDir(), "editing.json")
	open := func() (*App, chan func()) {
		updates := make(chan func(), 512)
		a := New(Options{Launch: launch, CWD: root, Provider: "mygo-mock", Model: "echo", PreferencesPath: path, Post: func(fn func()) { updates <- fn }})
		t.Cleanup(func() { a.Shutdown(10 * time.Second) })
		go a.Start()
		waitFor(t, a, updates, 90*time.Second, func() bool { return a.model.Phase == view.PhaseIdle })
		return a, updates
	}
	first, updates := open()
	firstID := first.sessionID
	first.model.Draft = "first conversation draft\n你好"
	first.DraftChanged()
	first.model.OpenPreferences()
	first.model.Prefs.Draft.Theme = "dark"
	first.model.Prefs.Draft.Locale = "zh-CN"
	first.model.Prefs.Draft.Send = "Ctrl+Enter"
	first.ApplyUIPreferences()
	first.NewSession()
	waitFor(t, first, updates, 30*time.Second, func() bool { return !first.model.SessionBusy && first.sessionID != firstID })
	secondID := first.sessionID
	first.model.Draft = "second conversation draft"
	first.DraftChanged()
	first.Shutdown(10 * time.Second)

	second, updates := open()
	if second.model.Prefs.Value.Theme != "dark" || second.model.Prefs.Value.Locale != "zh-CN" || second.model.Prefs.Value.Send != "Ctrl+Enter" {
		t.Fatal("UI preferences did not survive restart")
	}
	if second.sessionID != secondID || second.model.Draft != "second conversation draft" {
		t.Fatal("restart did not reopen the last conversation and its draft")
	}
	second.SwitchSession(firstID)
	waitFor(t, second, updates, 30*time.Second, func() bool { return !second.model.SessionBusy && second.sessionID == firstID })
	if second.model.Draft != "first conversation draft\n你好" {
		t.Fatal("other conversation lost its draft")
	}
	second.Send(second.model.Draft)
	waitFor(t, second, updates, 30*time.Second, func() bool {
		return second.model.Phase == view.PhaseIdle && assistantAnswers(second) > 0 && len(second.local.pending) == 0
	})
	flushEditing(t, second)
	snapshot, _, err := preferences.NewStore(path).Load()
	if err != nil || snapshot.Drafts[firstID] != "" || snapshot.Drafts[secondID] != "second conversation draft" {
		t.Fatal("acknowledged delivery changed the wrong saved draft")
	}
	second.Shutdown(10 * time.Second)

	third, updates := open()
	if third.sessionID != firstID || third.model.Draft != "" || assistantAnswers(third) != 1 {
		t.Fatal("sent draft returned or durable history was not restored")
	}
	third.SwitchSession(secondID)
	waitFor(t, third, updates, 30*time.Second, func() bool { return !third.model.SessionBusy && third.sessionID == secondID })
	if third.model.Draft != "second conversation draft" {
		t.Fatal("third process lost the unsent draft")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := third.local.writer.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}
