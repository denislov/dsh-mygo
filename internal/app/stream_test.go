package app

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"dsh-mygo/internal/view"
)

func appStreamFrame(application *App, frame string) {
	application.handleNotification("session/assistant-stream", json.RawMessage(`{"sessionId":"s1","frame":`+frame+`}`))
}

func openAppStream(application *App) {
	appStreamFrame(application, `{"type":"start","attemptId":"s1:1","revision":1,"turn":1,"step":1}`)
	appStreamFrame(application, `{"type":"chunk","attemptId":"s1:1","revision":2,"index":0,"time":1,"chunk":{"type":"block-start","index":0,"blockType":"text"}}`)
	appStreamFrame(application, `{"type":"chunk","attemptId":"s1:1","revision":3,"index":1,"time":1,"chunk":{"type":"text-delta","index":0,"text":"partial"}}`)
}

func TestStreamPreviewReplacedByDurableMessageAndClearedOnIdle(t *testing.T) {
	application := newTestApp(&fakeClient{})
	application.options.SDKProtocol = false
	application.model.CanStream = true
	application.model.Phase = view.PhaseRunning
	openAppStream(application)
	if len(application.transcript.Items()) != 0 || len(application.model.Items) != 1 || application.model.Items[0].State != "streaming" {
		t.Fatal("stream became durable or was not visible")
	}
	application.handleNotification("session/event", json.RawMessage(`{"sessionId":"s1","seq":0,"event":{"type":"assistant/message","seq":0,"data":{"turn":1,"step":1,"message":{"content":[{"type":"text","text":"final"}]}}}}`))
	appStreamFrame(application, `{"type":"end","attemptId":"s1:1","revision":4,"index":2,"outcome":{"kind":"committed","eventType":"assistant/message","seq":0}}`)
	if len(application.model.Items) != 1 || application.model.Items[0].Text != "final" || application.model.Items[0].State == "streaming" {
		t.Fatal("final answer duplicated")
	}
	appStreamFrame(application, `{"type":"start","attemptId":"s1:2","revision":5,"turn":2,"step":1}`)
	appStreamFrame(application, `{"type":"chunk","attemptId":"s1:2","revision":6,"index":0,"time":1,"chunk":{"type":"block-start","index":0,"blockType":"text"}}`)
	appStreamFrame(application, `{"type":"chunk","attemptId":"s1:2","revision":7,"index":1,"time":1,"chunk":{"type":"text-delta","index":0,"text":"temporary"}}`)
	application.applyStatus("idle")
	if len(application.model.Items) != 1 || application.model.Items[0].Text != "final" {
		t.Fatal("idle retained preview or lost history")
	}
}

func TestStreamCapabilityAndForeignSession(t *testing.T) {
	application := newTestApp(&fakeClient{})
	application.options.SDKProtocol = false
	application.model.Phase = view.PhaseRunning
	application.handleNotification("session/assistant-stream", json.RawMessage(`{"sessionId":"foreign","frame":{"type":"start","attemptId":"other:1","revision":1,"turn":1,"step":1}}`))
	if application.model.Phase != view.PhaseRunning {
		t.Fatal("foreign stream changed session")
	}
	openAppStream(application)
	if application.model.Phase != view.PhaseFailed {
		t.Fatal("unnegotiated stream accepted")
	}
}

func TestNotificationBatchesAreOrderedBoundedAndStopWithoutUI(t *testing.T) {
	updates := make(chan func(), 64)
	application := New(Options{Post: func(fn func()) { updates <- fn }})
	application.sessionID = "s1"
	application.model.CanStream = true
	application.model.Phase = view.PhaseRunning
	incoming := make(chan hostNotification, 512)
	for index := 0; index < 200; index++ {
		incoming <- hostNotification{method: "session/event", params: json.RawMessage(fmt.Sprintf(`{"sessionId":"s1","seq":%d,"event":{"type":"user/message","seq":%d,"data":{"source":{"kind":"user"},"content":[{"type":"text","text":"%d"}]}}}`, index, index, index))}
	}
	close(incoming)
	done := make(chan struct{})
	go func() { application.runNotifications(incoming); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("batch worker did not drain")
	}
	if len(updates) > 4 || len(updates) == 0 {
		t.Fatalf("unbounded redraws: %d", len(updates))
	}
	for len(updates) > 0 {
		(<-updates)()
	}
	if len(application.model.Items) != 200 || application.model.Items[199].Text != "199" {
		t.Fatal("batch lost/reordered durable events")
	}
	blocked := New(Options{Post: func(func()) {}})
	input := make(chan hostNotification, 4096)
	for index := 0; index < cap(input); index++ {
		input <- hostNotification{method: "ignored"}
	}
	finished := make(chan struct{})
	go func() { blocked.runNotifications(input); close(finished) }()
	deadline := time.Now().Add(2 * time.Second)
	for len(blocked.pendingUI) < maxPendingUIUpdates && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(blocked.pendingUI) != maxPendingUIUpdates {
		t.Fatal("UI backpressure limit was not reached")
	}
	close(blocked.stopped)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("backpressured worker ignored shutdown")
	}
}
