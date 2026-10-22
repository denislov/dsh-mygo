package app

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/view"
)

type fakeClient struct {
	mu     sync.Mutex
	calls  []string
	params []any
	result json.RawMessage
	err    error
	closed bool
}

func (f *fakeClient) Call(_ context.Context, method string, params any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	f.params = append(f.params, params)
	return f.result, f.err
}

func (f *fakeClient) OnNotification(channel.NotificationHandler) {}

func (f *fakeClient) Run() error { return nil }

func (f *fakeClient) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
}

func (f *fakeClient) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func newTestApp(client rpcClient) *App {
	application := New(Options{
		CWD:       "C:/work",
		Provider:  "deepseek-official",
		Model:     "deepseek-flash",
		SessionID: "s1",
		Post:      func(fn func()) { fn() },
	})
	application.client = client
	application.model.Phase = view.PhaseIdle
	return application
}

func TestHandleSessionEventProjectsOnlyTheOwnedSession(t *testing.T) {
	application := newTestApp(&fakeClient{})
	application.handleNotification("session.event", json.RawMessage(`{
		"sessionId": "s1",
		"event": {
			"type": "user/message", "seq": 1, "time": 1,
			"data": {"source": {"kind": "user"}, "content": [{"type": "text", "text": "hello"}]}
		}
	}`))
	application.handleNotification("session.event", json.RawMessage(`{
		"sessionId": "s2",
		"event": {
			"type": "user/message", "seq": 2, "time": 1,
			"data": {"source": {"kind": "user"}, "content": [{"type": "text", "text": "other"}]}
		}
	}`))
	items := application.Model().Items
	if len(items) != 1 || items[0].Text != "hello" {
		t.Fatalf("items = %+v", items)
	}
}

func TestIdleBeforeTurnActivityDoesNotEndTheTurn(t *testing.T) {
	application := newTestApp(&fakeClient{})
	application.model.Phase = view.PhaseRunning
	application.awaitingTurn = true
	application.sawActivity = false

	application.applyStatus("idle")
	if application.model.Phase != view.PhaseRunning {
		t.Fatalf("phase = %v after a stale idle", application.model.Phase)
	}

	application.handleNotification("session.event", json.RawMessage(`{
		"sessionId": "s1",
		"event": {
			"type": "user/message", "seq": 1, "time": 1,
			"data": {"source": {"kind": "user"}, "content": [{"type": "text", "text": "hello"}]}
		}
	}`))
	application.applyStatus("idle")
	if application.model.Phase != view.PhaseIdle {
		t.Fatalf("phase = %v after the turn ended", application.model.Phase)
	}
	if application.awaitingTurn {
		t.Fatal("awaitingTurn stayed set after the turn ended")
	}
}

func TestSendIgnoredWhenNotIdle(t *testing.T) {
	client := &fakeClient{}
	application := newTestApp(client)
	application.model.Phase = view.PhaseRunning
	application.model.Draft = "draft"

	application.Send("hello")

	if len(client.called()) != 0 {
		t.Fatalf("calls = %v", client.called())
	}
	if application.model.Draft != "draft" {
		t.Fatalf("draft = %q", application.model.Draft)
	}
}

func TestSendQueuesPromptAndClearsDraft(t *testing.T) {
	client := &fakeClient{result: json.RawMessage(`{"messageId":"m1"}`)}
	application := newTestApp(client)
	application.model.Draft = "hello"

	application.Send("hello")

	deadline := time.Now().Add(2 * time.Second)
	for len(client.called()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	calls := client.called()
	if len(calls) != 1 || calls[0] != "session/prompt" {
		t.Fatalf("calls = %v", calls)
	}
	if application.model.Draft != "" {
		t.Fatalf("draft = %q", application.model.Draft)
	}
	if application.model.Phase != view.PhaseRunning || !application.awaitingTurn {
		t.Fatalf("phase = %v, awaiting = %t", application.model.Phase, application.awaitingTurn)
	}
}

func TestSetFailureReportsHostState(t *testing.T) {
	application := newTestApp(&fakeClient{})
	application.model.Phase = view.PhaseRunning
	application.awaitingTurn = true

	application.setFailure("host exited", errors.New("exit status 1"))

	if application.model.Phase != view.PhaseFailed {
		t.Fatalf("phase = %v", application.model.Phase)
	}
	if application.model.Error != "host exited" || application.model.ErrorDetail != "exit status 1" {
		t.Fatalf("error = %q / %q", application.model.Error, application.model.ErrorDetail)
	}
	if application.awaitingTurn {
		t.Fatal("awaitingTurn stayed set after failure")
	}
}

func TestShutdownCallsHostAndClosesClient(t *testing.T) {
	client := &fakeClient{result: json.RawMessage(`{}`)}
	application := newTestApp(client)

	application.Shutdown(time.Second)

	calls := client.called()
	if len(calls) != 1 || calls[0] != "shutdown" {
		t.Fatalf("calls = %v", calls)
	}
	if !client.closed {
		t.Fatal("client was not closed")
	}
}
