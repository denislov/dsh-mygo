package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/view"
)

func resyncEvent(seq uint64, text string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"sessionId": "original", "seq": seq, "event": map[string]any{"seq": seq, "time": 1, "type": "assistant/message", "data": map[string]any{"message": map[string]any{"content": []any{map[string]string{"type": "text", "text": text}}}}}})
	return raw
}

func finishResync(t *testing.T, a *App, updates <-chan func()) {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for a.model.Phase == view.PhaseResyncing && !a.model.RecoveryOpen {
		select {
		case update := <-updates:
			update()
		case <-timeout:
			t.Fatal("resync did not settle")
		}
	}
}

func TestResyncRebuildsDurableHistoryAndRejectsOldIdleWithoutRestart(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method == "session/list" {
			return json.RawMessage(`{"sessions":[]}`), nil
		}
		if method != "session/history" || params.(map[string]any)["live"] != true {
			t.Error("resync did not request live history")
		}
		close(started)
		<-finish
		return json.RawMessage(`{"events":[{"seq":0,"time":1,"type":"assistant/message","data":{"message":{"content":[{"type":"text","text":"recovered"}]}}}],"nextSeq":1,"hasMore":false,"status":"running","statusRevision":5}`), nil
	}}
	a, updates := sessionTestApp(client)
	a.model.CanResync = true
	a.model.Phase = view.PhaseRunning
	a.model.Draft = "keep draft"
	a.handleNotification("session/event", resyncEvent(1, "after snapshot"))
	<-started
	if a.model.Phase != view.PhaseResyncing || a.model.RecoveryOpen {
		t.Fatal("gap did not enter automatic synchronization")
	}
	a.handleNotification("session/status", json.RawMessage(`{"sessionId":"original","status":"idle","statusRevision":4}`))
	a.handleNotification("session/status", json.RawMessage(`{"sessionId":"original","status":"running","statusRevision":6}`))
	close(finish)
	finishResync(t, a, updates)
	if a.model.Phase != view.PhaseRunning || a.nextSeq != 2 || a.model.Draft != "keep draft" || len(a.model.Items) != 2 {
		t.Fatalf("unsafe resync result: phase=%v next=%d items=%v", a.model.Phase, a.nextSeq, a.model.Items)
	}
	a.handleNotification("session/status", json.RawMessage(`{"sessionId":"original","status":"idle","statusRevision":7}`))
	if a.model.Phase != view.PhaseIdle {
		t.Fatal("fresh idle did not end the turn")
	}
}

func TestResyncFailurePreservesDraftAndOffersRecovery(t *testing.T) {
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) { return nil, errors.New("private-token") }}
	a, updates := sessionTestApp(client)
	a.model.CanResync = true
	a.model.Draft = "keep"
	a.handleNotification("session/event", resyncEvent(2, "gap"))
	finishResync(t, a, updates)
	if !a.model.RecoveryOpen || !a.model.Recoverable || a.model.Phase != view.PhaseFailed || a.model.Draft != "keep" || strings.Contains(a.model.ErrorDetail, "private-token") {
		t.Fatal("failed resync did not safely stop")
	}
}

func TestResyncRetriesNewGapsBeforePublishingAndBoundsAttempts(t *testing.T) {
	for _, recoverable := range []bool{true, false} {
		t.Run(map[bool]string{true: "newer snapshot", false: "exhausted"}[recoverable], func(t *testing.T) {
			calls := 0
			client := &sessionClient{request: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
				if method == "session/list" {
					return json.RawMessage(`{"sessions":[]}`), nil
				}
				calls++
				if calls > 1 && recoverable {
					return json.RawMessage(`{"events":[{"seq":0,"time":1,"type":"test/event","data":{}},{"seq":1,"time":1,"type":"test/event","data":{}},{"seq":2,"time":1,"type":"assistant/message","data":{"message":{"content":[{"type":"text","text":"final"}]}}}],"nextSeq":3,"hasMore":false,"status":"idle","statusRevision":0}`), nil
				}
				return json.RawMessage(`{"events":[],"nextSeq":0,"hasMore":false,"status":"idle","statusRevision":0}`), nil
			}}
			a, updates := sessionTestApp(client)
			a.model.CanResync = true
			a.handleNotification("session/event", resyncEvent(2, "final"))
			finishResync(t, a, updates)
			if recoverable {
				if calls != 2 || a.nextSeq != 3 || a.model.Phase != view.PhaseIdle || len(a.model.Items) != 1 {
					t.Fatalf("retry did not safely converge: calls=%d phase=%v", calls, a.model.Phase)
				}
			} else if calls != 3 || a.model.Phase != view.PhaseFailed {
				t.Fatal("inconsistent snapshots retried without a bound")
			}
		})
	}
}

func TestResyncBuffersCancelAcknowledgementUntilIdleSnapshot(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) {
		close(started)
		<-finish
		return json.RawMessage(`{"events":[{"seq":0,"time":1,"type":"test/event","data":{}},{"seq":1,"time":1,"type":"test/event","data":{}}],"nextSeq":2,"hasMore":false,"status":"idle","statusRevision":1}`), nil
	}}
	a, updates := sessionTestApp(client)
	a.model.CanResync = true
	a.model.Phase = view.PhaseRunning
	a.model.CancelPending = true
	a.handleNotification("session/event", resyncEvent(1, "covered"))
	<-started
	a.postAfterResync(func() {
		if a.model.Phase != view.PhaseRunning || !a.model.CancelPending {
			t.Error("cancel acknowledgement lost its running barrier")
		}
		a.model.CancelPending = false
		a.model.Phase = view.PhaseIdle
	})
	(<-updates)()
	close(finish)
	finishResync(t, a, updates)
	if a.model.Phase != view.PhaseIdle || a.model.CancelPending {
		t.Fatal("cancel remained stuck after resync")
	}
}

func TestResyncNewRunningStatusCannotBeOverwrittenByOlderCancelCallback(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) {
		close(started)
		<-finish
		return json.RawMessage(`{"events":[{"seq":0,"time":1,"type":"test/event","data":{}},{"seq":1,"time":1,"type":"test/event","data":{}}],"nextSeq":2,"hasMore":false,"status":"running","statusRevision":3}`), nil
	}}
	a, updates := sessionTestApp(client)
	a.model.CanResync = true
	a.model.Phase = view.PhaseRunning
	a.handleNotification("session/event", resyncEvent(1, "covered"))
	<-started
	a.postAfterResync(func() { a.model.Phase = view.PhaseIdle })
	(<-updates)()
	close(finish)
	finishResync(t, a, updates)
	if a.model.Phase != view.PhaseRunning {
		t.Fatal("stale RPC acknowledgement unlocked a running Host")
	}
}

func TestResyncOverflowAndLateResultCannotReviveFailedHost(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) {
		close(started)
		<-finish
		return json.RawMessage(`{"events":[],"nextSeq":0,"hasMore":false,"status":"idle","statusRevision":0}`), nil
	}}
	a, updates := sessionTestApp(client)
	a.model.CanResync = true
	a.model.Draft = "keep"
	a.handleNotification("session/event", resyncEvent(2, "gap"))
	<-started
	for index := 0; index <= maxResyncFrames; index++ {
		a.handleNotification("session/status", json.RawMessage(`{"sessionId":"original","status":"running","statusRevision":1}`))
	}
	close(finish)
	select {
	case update := <-updates:
		update()
	case <-time.After(3 * time.Second):
		t.Fatal("late result never settled")
	}
	if a.model.Phase != view.PhaseFailed || a.model.Draft != "keep" || a.nextSeq != 0 {
		t.Fatal("late resync revived failed state")
	}
}
