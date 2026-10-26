package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/preferences"
	"dsh-mygo/internal/view"
)

func editingTestApp(t *testing.T, client rpcClient) (*App, chan func(), string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "editing.json")
	updates := make(chan func(), 256)
	a := New(Options{PreferencesPath: path, Provider: "test", Model: "echo", Post: func(fn func()) { updates <- fn }})
	a.installHost(&hostInstance{client: client})
	a.loadLocalState()
	(<-updates)()
	a.sessionID = "original"
	a.model.SessionID = "original"
	a.model.Phase = view.PhaseIdle
	t.Cleanup(func() { a.Shutdown(time.Second) })
	return a, updates, path
}
func flushEditing(t *testing.T, a *App) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.local.writer.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}
func pumpEditing(t *testing.T, updates <-chan func(), done func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !done() {
		select {
		case fn := <-updates:
			fn()
		case <-deadline:
			t.Fatal("editing operation did not settle")
		}
	}
}

func TestLocalDraftDeliveryKeepsOriginalUntilAcknowledgedAndPreservesNewEdits(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "failed"}[failed], func(t *testing.T) {
			release, started := make(chan struct{}), make(chan struct{})
			client := &sessionClient{request: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
				if method == "shutdown" {
					return json.RawMessage(`{}`), nil
				}
				close(started)
				<-release
				if failed {
					return nil, errors.New("delivery failed")
				}
				return json.RawMessage(`{"messageId":"m"}`), nil
			}}
			a, updates, path := editingTestApp(t, client)
			a.model.Draft = "  original draft\n"
			a.DraftChanged()
			a.Send(a.model.Draft)
			<-started
			a.model.Draft = "next draft"
			a.DraftChanged()
			flushEditing(t, a)
			snapshot, _, err := preferences.NewStore(path).Load()
			if err != nil || snapshot.Drafts["original"] != "  original draft\n\n\nnext draft" {
				t.Fatal("in-flight submission lost its original text")
			}
			close(release)
			pumpEditing(t, updates, func() bool { return len(a.local.pending) == 0 })
			flushEditing(t, a)
			snapshot, _, err = preferences.NewStore(path).Load()
			expected := "next draft"
			if failed {
				expected = "  original draft\n\n\nnext draft"
			}
			if err != nil || snapshot.Drafts["original"] != expected || a.model.Draft != expected {
				t.Fatal("delivery confirmation erased new edits or failed text")
			}
		})
	}
}

func TestQuitFlushesFinalEditAndBudgetFailureKeepsWindowOpen(t *testing.T) {
	client := &fakeClient{result: json.RawMessage(`{}`)}
	a, updates, path := editingTestApp(t, client)
	a.model.Draft = strings.Repeat("x", preferences.MaxDraftBytes+1)
	a.RequestQuit(false)
	pumpEditing(t, updates, func() bool { return !a.model.LocalState.Closing })
	if a.ReadyToQuit() || a.shuttingDown.Load() || a.model.LocalState.Error == "" || len(a.model.Draft) != preferences.MaxDraftBytes+1 {
		t.Fatal("failed save discarded the draft or closed the window")
	}
	a.model.Draft = "last edit before closing"
	a.RequestQuit(false)
	pumpEditing(t, updates, a.ReadyToQuit)
	snapshot, _, err := preferences.NewStore(path).Load()
	if err != nil || snapshot.Drafts["original"] != "last edit before closing" {
		t.Fatal("quit missed the final edit")
	}
}

func TestStartupResumeFailureCreatesConversationAndOffersPreviousDraft(t *testing.T) {
	client := &sessionClient{request: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
		switch method {
		case "initialize":
			return json.RawMessage(`{"protocolVersion":1,"server":{"name":"host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":["session.history"]}`), nil
		case "session/resume":
			return nil, errors.New("unavailable")
		case "session/create":
			return json.RawMessage(`{"session":{"id":"new","title":"","cwd":"C:/work","provider":"","model":"","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"nextSeq":0}}`), nil
		default:
			return json.RawMessage(`{}`), nil
		}
	}}
	a, _, _ := editingTestApp(t, client)
	initialized, err := a.initializeSession(context.Background(), client, sessionInitialization{resumeID: "missing", allowFallback: true})
	if err != nil || initialized.sessionID != "new" || initialized.restoreNotice == "" {
		t.Fatal("unavailable recent session blocked new work")
	}
	a.drafts = map[string]string{"missing": "recover me"}
	a.model.Draft = "new text"
	a.offerPreviousDraft("missing")
	a.model.LocalState.RecoverDraft()
	if a.model.Draft != "recover me\n\nnew text" || a.drafts["missing"] != "recover me" {
		t.Fatal("recovery replaced existing or retained text")
	}
}

func TestQuitWithoutPersistenceKeepsStorageDisabled(t *testing.T) {
	updates := make(chan func(), 32)
	a := New(Options{Post: func(fn func()) { updates <- fn }})
	a.RequestQuit(false)
	pumpEditing(t, updates, a.ReadyToQuit)
	if a.local != nil {
		t.Fatal("quit changed the persistence configuration")
	}
}

func TestUnavailablePreferencePathRequiresExplicitDiscard(t *testing.T) {
	updates := make(chan func(), 32)
	a := New(Options{PreferencesUnavailable: true, Post: func(fn func()) { updates <- fn }})
	a.model.Draft = "unsaved text"
	a.RequestQuit(false)
	pumpEditing(t, updates, func() bool { return !a.model.LocalState.Closing })
	if a.ReadyToQuit() || a.model.Draft != "unsaved text" {
		t.Fatal("unavailable storage silently discarded text")
	}
	a.RequestQuit(true)
	pumpEditing(t, updates, a.ReadyToQuit)
}

func TestRecentSessionUsesCurrentHostDefaultInsteadOfFallbackModel(t *testing.T) {
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		switch method {
		case "initialize":
			return json.RawMessage(`{"protocolVersion":1,"server":{"name":"host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":["session.history","settings.configuration"]}`), nil
		case "session/resume":
			fields := requestFields(t, params)
			if fields["useDefaultModel"] != true || fields["provider"] != nil {
				t.Error("cold resume ignored Host defaults")
			}
			return json.RawMessage(`{"session":{"id":"original","title":"","cwd":"C:/work","provider":"test","model":"host-default","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"nextSeq":0}}`), nil
		case "session/history":
			return json.RawMessage(`{"events":[],"nextSeq":0,"hasMore":false}`), nil
		default:
			return nil, errors.New("unexpected request")
		}
	}}
	a := New(Options{Provider: "fallback", Model: "fallback", UseHostDefaults: true})
	initialized, err := a.initializeSession(context.Background(), client, sessionInitialization{resumeID: "original", allowFallback: true})
	if err != nil || initialized.route.Model != "host-default" {
		t.Fatal("recent conversation did not use current Host defaults")
	}
}

func TestMalformedDeliveryAcknowledgementDoesNotDiscardDraft(t *testing.T) {
	for _, test := range []struct{ name, result string }{
		{"missing", `{}`},
		{"empty", `{"messageId":""}`},
		{"null", `{"messageId":null}`},
		{"wrong type", `{"messageId":1}`},
		{"blank", `{"messageId":"  "}`},
		{"control", `{"messageId":"a\u0000b"}`},
		{"oversized", `{"messageId":"` + strings.Repeat("x", 257) + `"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeClient{result: json.RawMessage(test.result)}
			a, updates, _ := editingTestApp(t, client)
			a.model.Draft = "preserve until valid acknowledgement"
			a.Send(a.model.Draft)
			pumpEditing(t, updates, func() bool { return len(a.local.pending) == 0 })
			if a.model.Draft != "preserve until valid acknowledgement" || a.model.Error == "" {
				t.Fatal("malformed success discarded the draft")
			}
		})
	}
}
