package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"dsh-mygo/internal/view"
)

const liveTextBaseline = `{"revision":3,"activeAttempt":{"attemptId":"s1:1","turn":1,"step":1,"nextIndex":2,"blocks":[{"index":0,"type":"text","text":"prefix","ended":false}],"truncated":false}}`

func TestResyncRestoresStreamAndReplaysOnlyNewerChunks(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method == "session/list" {
			return json.RawMessage(`{"sessions":[]}`), nil
		}
		if params.(map[string]any)["assistantStream"] != true {
			t.Error("baseline not requested")
		}
		close(started)
		<-finish
		return json.RawMessage(`{"events":[],"nextSeq":0,"hasMore":false,"status":"running","statusRevision":0,"assistantStream":` + liveTextBaseline + `}`), nil
	}}
	a, updates := sessionTestApp(client)
	a.model.CanResync, a.model.CanStream, a.canStreamBaseline = true, true, true
	a.model.Phase = view.PhaseRunning
	// Missing start/block-start is enough to trigger a stream-only resync.
	a.handleNotification("session/assistant-stream", json.RawMessage(`{"sessionId":"original","frame":{"type":"chunk","attemptId":"s1:1","revision":3,"index":1,"time":1,"chunk":{"type":"text-delta","index":0,"text":"prefix"}}}`))
	<-started
	a.handleNotification("session/assistant-stream", json.RawMessage(`{"sessionId":"original","frame":{"type":"chunk","attemptId":"s1:1","revision":4,"index":2,"time":1,"chunk":{"type":"text-delta","index":0,"text":" continued"}}}`))
	close(finish)
	finishResync(t, a, updates)
	if !hasStreamingText(a.model.Items, "prefix continued") || a.nextSeq != 0 || len(a.transcript.Items()) != 0 {
		t.Fatal("baseline did not remain temporary or replayed twice")
	}
}

func TestHistoryStreamBaselineRejectsMissingInvalidOrChangedPages(t *testing.T) {
	for _, second := range []string{liveTextBaseline, `{"revision":4}`, `null`, `{}`} {
		t.Run(fmt.Sprint(second), func(t *testing.T) {
			calls := 0
			client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) {
				calls++
				if calls == 1 {
					return json.RawMessage(`{"events":[{"seq":0,"time":1,"type":"test/event","data":{}}],"nextSeq":2,"hasMore":true,"nextCursor":"next","status":"running","statusRevision":0,"assistantStream":` + liveTextBaseline + `}`), nil
				}
				return json.RawMessage(`{"events":[{"seq":1,"time":1,"type":"test/event","data":{}}],"nextSeq":2,"hasMore":false,"status":"running","statusRevision":0,"assistantStream":` + second + `}`), nil
			}}
			_, err := readHistorySnapshot(context.Background(), client, "original", 0, true, true)
			if (err == nil) != (second == liveTextBaseline) {
				t.Fatalf("unexpected baseline pagination result: %v", err)
			}
		})
	}
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) {
		return json.RawMessage(`{"events":[],"nextSeq":0,"hasMore":false,"status":"idle","statusRevision":0}`), nil
	}}
	if _, err := readHistorySnapshot(context.Background(), client, "original", 0, true, true); err == nil {
		t.Fatal("required baseline missing")
	}
}

func TestResyncStreamBufferDetectsNewGapsAndIgnoresOlderAttempts(t *testing.T) {
	snapshot := historySnapshot{Baseline: json.RawMessage(liveTextBaseline)}
	for _, revision := range []int{2, 4, 5} {
		params := json.RawMessage(fmt.Sprintf(`{"sessionId":"original","frame":{"type":"chunk","attemptId":"s1:1","revision":%d,"index":2,"time":1,"chunk":{"type":"text-delta","index":0,"text":"suffix"}}}`, revision))
		continuous, err := resyncStreamContinuous([]hostNotification{{method: "session/assistant-stream", params: params}}, &snapshot)
		if err != nil || continuous != (revision != 5) {
			t.Fatalf("revision %d: continuous=%v err=%v", revision, continuous, err)
		}
	}
}
