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
	mu         sync.Mutex
	calls      []string
	params     []any
	result     json.RawMessage
	results    map[string]json.RawMessage
	err        error
	closed     bool
	beforeCall func(method string)
}

func (f *fakeClient) Call(_ context.Context, method string, params any) (json.RawMessage, error) {
	if f.beforeCall != nil {
		f.beforeCall(method)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	f.params = append(f.params, params)
	if result, ok := f.results[method]; ok {
		return result, f.err
	}
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
		CWD:         "C:/work",
		Provider:    "deepseek-official",
		Model:       "deepseek-flash",
		SessionID:   "s1",
		SDKProtocol: true,
		Post:        func(fn func()) { fn() },
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

func TestMygoInitializeCreatesSessionAfterValidHandshake(t *testing.T) {
	for _, test := range []struct {
		name      string
		handshake string
		session   string
		wantCalls int
		valid     bool
	}{
		{"valid", `{"protocolVersion":1,"server":{"name":"dsh-mygo-host","version":"0.1.0","dshVersion":"0.2.1-rc.2"},"capabilities":["approval"]}`, `{"session":{"id":"host-session","status":"idle","nextSeq":7}}`, 2, true},
		{"wrong protocol", `{"protocolVersion":2}`, `{}`, 1, false},
		{"wrong release", `{"protocolVersion":1,"server":{"name":"host","version":"9","dshVersion":"0.2.1-rc.2"},"capabilities":[]}`, `{}`, 1, false},
		{"invalid session id", `{"protocolVersion":1,"server":{"name":"host","version":"0.1.0","dshVersion":"0.2.1-rc.2"},"capabilities":[]}`, `{"session":{"id":"../unsafe","status":"idle","nextSeq":0}}`, 2, false},
		{"missing baseline", `{"protocolVersion":1,"server":{"name":"host","version":"0.1.0","dshVersion":"0.2.1-rc.2"},"capabilities":[]}`, `{"session":{"id":"s1","status":"idle"}}`, 2, false},
		{"unsafe baseline", `{"protocolVersion":1,"server":{"name":"host","version":"0.1.0","dshVersion":"0.2.1-rc.2"},"capabilities":[]}`, `{"session":{"id":"s1","status":"idle","nextSeq":9007199254740992}}`, 2, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeClient{results: map[string]json.RawMessage{"initialize": json.RawMessage(test.handshake), "session/create": json.RawMessage(test.session)}}
			application := newTestApp(client)
			application.options.SDKProtocol = false
			initialized, err := application.initialize(context.Background(), client)
			if (err == nil) != test.valid {
				t.Fatalf("initialize error = %v", err)
			}
			if len(client.called()) != test.wantCalls {
				t.Fatalf("calls = %v", client.called())
			}
			if test.valid && (initialized.sessionID != "host-session" || initialized.nextSeq != 7) {
				t.Fatalf("initialized = %+v", initialized)
			}
			if test.valid {
				params := client.params[0].(map[string]any)
				capabilities := params["capabilities"].([]string)
				if len(capabilities) != 7 || capabilities[0] != "session.cancel" || capabilities[1] != "approval" || capabilities[2] != "question" || capabilities[3] != "session.assistant-stream" || capabilities[4] != "session.history" || capabilities[5] != "model.selection" || capabilities[6] != "settings.configuration" {
					t.Fatal("unimplemented capabilities advertised")
				}
			}
		})
	}
}

func TestMygoEventsUseBaselineAndRejectGaps(t *testing.T) {
	application := newTestApp(&fakeClient{})
	application.options.SDKProtocol = false
	application.nextSeq = 7
	application.handleNotification("session/event", json.RawMessage(`{"sessionId":"s1","seq":7,"event":{"seq":7,"type":"user/message","data":{"source":{"kind":"user"},"content":[{"type":"text","text":"hello"}]}}}`))
	if len(application.Model().Items) != 1 || application.nextSeq != 8 {
		t.Fatalf("items = %+v, next seq = %d", application.Model().Items, application.nextSeq)
	}
	application.handleNotification("session/event", json.RawMessage(`{"sessionId":"s1","seq":9,"event":{"seq":9,"type":"turn/end","data":{}}}`))
	if application.Model().Phase != view.PhaseFailed {
		t.Fatal("event gap was silently accepted")
	}
}

func TestCancelWaitsForHostPreservesDraftAndRejectsDuplicate(t *testing.T) {
	client := &fakeClient{result: json.RawMessage(`{"cancelled":true,"status":"idle"}`)}
	application := newTestApp(client)
	application.options.SDKProtocol = false
	application.model.CanCancel = true
	application.model.Phase = view.PhaseRunning
	application.model.Draft = "next prompt"
	application.awaitingTurn = true
	updates := make(chan func(), 8)
	application.options.Post = func(fn func()) { updates <- fn }
	application.Cancel()
	application.Cancel()
	if !application.model.CancelPending || application.model.Phase != view.PhaseRunning {
		t.Fatal("cancel prematurely ended the turn")
	}
	select {
	case update := <-updates:
		update()
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not respond")
	}
	if len(client.called()) != 1 || client.called()[0] != "session/cancel" {
		t.Fatalf("calls = %v", client.called())
	}
	if application.model.Phase != view.PhaseIdle || application.model.CancelPending || application.model.Draft != "next prompt" {
		t.Fatalf("model = %+v", application.model)
	}
	application.Send(application.model.Draft)
	if application.model.Phase != view.PhaseRunning {
		t.Fatal("could not start another turn")
	}
}

func TestCancelUnavailableAndSDKDoNotCallHost(t *testing.T) {
	for _, sdk := range []bool{false, true} {
		client := &fakeClient{}
		application := newTestApp(client)
		application.options.SDKProtocol = sdk
		application.model.Phase = view.PhaseRunning
		application.model.CanCancel = sdk
		application.Cancel()
		if application.model.CancelPending || len(client.called()) != 0 {
			t.Fatal("unavailable cancellation sent")
		}
	}
}

func TestCancelFailureAndStaleResponseDoNotEndANewerTurn(t *testing.T) {
	client := &fakeClient{err: errors.New("cancel failed")}
	application := newTestApp(client)
	application.options.SDKProtocol = false
	application.model.CanCancel = true
	application.model.Phase = view.PhaseRunning
	updates := make(chan func(), 8)
	application.options.Post = func(fn func()) { updates <- fn }
	application.Cancel()
	select {
	case update := <-updates:
		update()
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not respond")
	}
	if application.model.Phase != view.PhaseRunning || application.model.CancelPending || application.model.Error == "" {
		t.Fatal("cancel failure was hidden or ended the turn")
	}
	application.Cancel()
	var stale func()
	select {
	case stale = <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("retry did not respond")
	}
	application.applyStatus("idle")
	stale()
	application.Send("another turn")
	stale()
	if application.model.Phase != view.PhaseRunning || application.model.Error != "" || application.model.CancelPending {
		t.Fatal("old cancellation changed a newer turn")
	}
}

func TestCancelBlocksANewTurnEvenAfterHostBecomesIdle(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	client := &fakeClient{result: json.RawMessage(`{"cancelled":false,"status":"idle"}`), beforeCall: func(method string) {
		if method == "session/cancel" {
			close(started)
			<-release
		}
	}}
	application := newTestApp(client)
	application.options.SDKProtocol = false
	application.model.CanCancel = true
	application.model.Phase = view.PhaseRunning
	application.awaitingTurn = true
	application.sawActivity = true
	updates := make(chan func(), 8)
	application.options.Post = func(fn func()) { updates <- fn }
	application.Cancel()
	<-started
	application.applyStatus("idle")
	application.Send("must wait for cancel")
	blocked := application.model.Phase == view.PhaseRunning && application.model.CancelPending
	close(release)
	if !blocked {
		t.Fatal("a new turn can overtake an outstanding cancel")
	}
	select {
	case update := <-updates:
		update()
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not respond")
	}
	if application.model.Phase != view.PhaseIdle || application.model.CancelPending {
		t.Fatal("idle was not restored after cancel settled")
	}
	application.Send("next turn")
	if application.model.Phase != view.PhaseRunning {
		t.Fatal("next turn remained blocked")
	}
}

func TestCancelAfterSendWaitsForPromptAcceptance(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	client := &fakeClient{results: map[string]json.RawMessage{
		"session/prompt": json.RawMessage(`{"messageId":"m1"}`),
		"session/cancel": json.RawMessage(`{"cancelled":true,"status":"idle"}`),
	}, beforeCall: func(method string) {
		if method == "session/prompt" {
			close(started)
			<-release
		}
	}}
	application := newTestApp(client)
	application.options.SDKProtocol = false
	application.model.CanCancel = true
	updates := make(chan func(), 8)
	application.options.Post = func(fn func()) { updates <- fn }
	application.Send("hello")
	<-started
	application.model.Draft = "next"
	application.Cancel()
	close(release)
	waitFor(t, application, updates, 2*time.Second, func() bool { return application.model.Phase == view.PhaseIdle })
	calls := client.called()
	if len(calls) != 2 || calls[0] != "session/prompt" || calls[1] != "session/cancel" {
		t.Fatalf("request order = %v", calls)
	}
	if application.model.Draft != "next" {
		t.Fatal("cancel discarded the next draft")
	}
}

func TestCancelInvalidResponsesCannotConfirmIdle(t *testing.T) {
	for _, result := range []string{`{}`, `null`, `{"cancelled":true,"status":"running"}`, `{"cancelled":"yes","status":"idle"}`} {
		client := &fakeClient{result: json.RawMessage(result)}
		application := newTestApp(client)
		application.options.SDKProtocol = false
		application.model.CanCancel = true
		application.model.Phase = view.PhaseRunning
		updates := make(chan func(), 1)
		application.options.Post = func(fn func()) { updates <- fn }
		application.Cancel()
		select {
		case update := <-updates:
			update()
		case <-time.After(2 * time.Second):
			t.Fatal("cancel did not respond")
		}
		if application.model.Phase != view.PhaseRunning || application.model.Error == "" {
			t.Fatal("invalid response confirmed idle")
		}
	}
}

func TestCancelTimeoutStillAcceptsLaterHostIdle(t *testing.T) {
	client := &fakeClient{err: &channel.RPCError{Code: -32000, Message: "Cancellation is still pending", Data: json.RawMessage(`{"kind":"session/cancel-timeout"}`)}}
	application := newTestApp(client)
	application.options.SDKProtocol = false
	application.model.CanCancel = true
	application.model.Phase = view.PhaseRunning
	application.awaitingTurn = true
	updates := make(chan func(), 1)
	application.options.Post = func(fn func()) { updates <- fn }
	application.Cancel()
	select {
	case update := <-updates:
		update()
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not respond")
	}
	if application.model.Phase != view.PhaseRunning || application.model.CancelPending {
		t.Fatal("timeout confirmed idle or blocked retry")
	}
	application.applyStatus("idle")
	if application.model.Phase != view.PhaseIdle {
		t.Fatal("late idle was ignored after cancellation timeout")
	}
}
