package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/view"
)

func TestAgainstMygoLargeHistoryAndReleasedSessions(t *testing.T) {
	launch, root := realMygoLaunch(t, true)
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "mygo-large-history.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	patch, err := json.Marshal([]any{map[string]any{"insert": []any{map[string]any{"id": "mygo-large-history", "name": filepath.ToSlash(fixture)}}}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "large.patch.yml")
	if err := os.WriteFile(path, patch, 0600); err != nil {
		t.Fatal(err)
	}
	launch.Args = append(launch.Args, "--patch", path)
	open := func() (*App, chan func()) {
		updates := make(chan func(), 512)
		a := New(Options{Launch: launch, CWD: root, Provider: "mygo-mock", Model: "echo", Post: func(fn func()) { updates <- fn }})
		t.Cleanup(func() { a.Shutdown(10 * time.Second) })
		go a.Start()
		waitFor(t, a, updates, 90*time.Second, func() bool { return a.model.Phase == view.PhaseIdle && !a.model.SessionPanel.Refreshing })
		if !a.canRelease || !a.canHistoryWindow {
			t.Fatal("new capabilities not negotiated")
		}
		return a, updates
	}
	a, updates := open()
	missing := false
	for _, s := range a.model.Sessions {
		if s.ID == "large-history" {
			missing = s.MetadataIncomplete
		}
	}
	if !missing {
		t.Fatal("missing cold index was not exposed without scanning")
	}
	a.SwitchSession("large-history")
	waitFor(t, a, updates, 60*time.Second, func() bool { return !a.model.SessionBusy })
	if a.sessionID != "large-history" || a.nextSeq < 35001 || a.model.History.Start < 34900 {
		t.Fatalf("large window failed: start=%d next=%d error=%s", a.model.History.Start, a.nextSeq, a.model.SessionsError)
	}
	originalSeq := a.nextSeq
	start := a.model.History.Start
	a.model.Draft = "large draft"
	a.LoadEarlierHistory()
	completeSessionJob(t, a, updates)
	if a.model.History.Start != start-64 || a.nextSeq != originalSeq || a.model.Draft != "large draft" {
		t.Fatal("older page changed boundary/draft")
	}
	a.ReturnToLatest()
	completeSessionJob(t, a, updates)
	if a.model.History.Start != start {
		t.Fatal("return to latest failed")
	}
	for index := 0; index < 12; index++ {
		previous := a.sessionID
		a.NewSession()
		waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SessionBusy })
		if a.sessionID == previous || a.model.SessionsError != "" {
			t.Fatalf("navigation %d exhausted handles: %s", index, a.model.SessionsError)
		}
	}
	a.SwitchSession("large-history")
	completeSessionJob(t, a, updates)
	if a.sessionID != "large-history" || a.model.Draft != "large draft" {
		t.Fatal("released session failed to reopen")
	}
	a.Send("continue the large log")
	waitFor(t, a, updates, 30*time.Second, func() bool { return a.model.Phase == view.PhaseIdle && hasAssistantText(a.model.Items, "pong") })
	a.Shutdown(10 * time.Second)
	second, secondUpdates := open()
	found := false
	for _, s := range second.model.Sessions {
		if s.ID == "large-history" {
			found = s.MetadataCached && !s.MetadataIncomplete && strings.HasPrefix(s.Title, "Large history 35000 ")
		}
	}
	if !found {
		t.Fatal("large catalog index did not survive restart")
	}
	second.SwitchSession("large-history")
	completeSessionJob(t, second, secondUpdates)
	if second.sessionID != "large-history" || !hasAssistantText(second.model.Items, "pong") {
		t.Fatal("large durable tail did not survive restart")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	snapshot, err := readHistoryWindow(ctx, second.currentClient(), "large-history", second.nextSeq, nil, true, second.canStreamBaseline)
	if err != nil || snapshot.Status != "idle" {
		t.Fatalf("large live baseline: %v", err)
	}
}

func TestAgainstMygoCompleteTurnHistory(t *testing.T) {
	launch, root := realMygoLaunch(t, true)
	updates := make(chan func(), 512)
	a := New(Options{Launch: launch, CWD: root, Provider: "mygo-mock", Model: "history-probe", Post: func(fn func()) { updates <- fn }})
	t.Cleanup(func() { a.Shutdown(10 * time.Second) })
	go a.Start()
	waitFor(t, a, updates, 90*time.Second, func() bool { return a.model.Phase == view.PhaseIdle && !a.model.SessionPanel.Refreshing })
	original := a.sessionID
	a.Send("Older complete prompt")
	waitFor(t, a, updates, 30*time.Second, func() bool { return a.model.Phase == view.PhaseIdle && hasAssistantText(a.model.Items, "pong") })
	a.Send("Tool-heavy complete prompt")
	longAnswer := strings.Repeat("A complete long response. ", 500)
	waitFor(t, a, updates, 60*time.Second, func() bool { return a.model.Phase == view.PhaseIdle && hasAssistantText(a.model.Items, longAnswer) })
	for range 2 {
		previous := a.sessionID
		a.NewSession()
		waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SessionBusy && a.sessionID != previous })
	}
	a.SwitchSession(original)
	waitFor(t, a, updates, 30*time.Second, func() bool { return !a.model.SessionBusy })
	start := a.model.History.Start
	if a.sessionID != original || start == 0 || a.nextSeq-start <= 64 || len(a.model.Items) != 72 {
		t.Fatalf("incomplete tool-heavy turn: start=%d next=%d rows=%d error=%s", start, a.nextSeq, len(a.model.Items), a.model.SessionsError)
	}
	if !hasAssistantText(a.model.Items, longAnswer) {
		t.Fatal("long assistant response was truncated")
	}
	a.model.Draft = "keep while reading"
	a.LoadEarlierHistory()
	completeSessionJob(t, a, updates)
	if a.model.History.Start >= start || len(a.model.Items) != 74 || a.model.Draft != "keep while reading" {
		t.Fatal("older complete round lost history or draft")
	}
}
