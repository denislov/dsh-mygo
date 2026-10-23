package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/view"
)

func historyWindowJSON(start, end, next uint64) json.RawMessage {
	events := make([]map[string]any, 0, end-start)
	for seq := start; seq < end; seq++ {
		events = append(events, map[string]any{"seq": seq, "time": 1, "type": "assistant/message", "data": map[string]any{"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": fmt.Sprint("answer ", seq)}}}}})
	}
	page := map[string]any{"events": events, "startSeq": start, "nextSeq": next, "hasMore": start > 0}
	if start > 0 {
		page["nextCursor"] = "unused-opaque"
	}
	raw, _ := json.Marshal(page)
	return raw
}

func TestHistoryWindowUsesLargeOffsetsAndRejectsMalformedBoundaries(t *testing.T) {
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		p := params.(map[string]any)
		if method != "session/history" || p["direction"] != "backward" || p["beforeSeq"] != uint64(30000) {
			t.Error("wrong history parameters")
		}
		return historyWindowJSON(29998, 30000, 31000), nil
	}}
	before := uint64(30000)
	got, err := readHistoryWindow(context.Background(), client, "saved", 31000, &before, false, false)
	if err != nil || got.StartSeq != 29998 || got.NextSeq != 31000 || len(got.Transcript.Items()) != 2 {
		t.Fatalf("window=%+v err=%v", got, err)
	}
	for _, raw := range []string{
		`{"events":[],"startSeq":30000,"nextSeq":30000,"hasMore":true,"nextCursor":"x"}`,
		`{"events":[],"startSeq":null,"nextSeq":30000,"hasMore":false}`,
		`{"events":[],"startSeq":0,"nextSeq":9007199254740992,"hasMore":false}`,
		`{"events":[{"seq":4,"time":1,"type":"test","data":{}}],"startSeq":0,"nextSeq":1,"hasMore":false}`,
	} {
		client.request = func(context.Context, string, any) (json.RawMessage, error) { return json.RawMessage(raw), nil }
		if _, err := readHistoryWindow(context.Background(), client, "saved", 0, nil, false, false); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestLoadEarlierPreservesDraftRealtimeBoundaryAndRetryState(t *testing.T) {
	fail := false
	client := &sessionClient{request: func(_ context.Context, _ string, params any) (json.RawMessage, error) {
		if fail {
			return nil, errors.New("private-token")
		}
		before, older := params.(map[string]any)["beforeSeq"]
		if older {
			if before != uint64(64) {
				t.Error("wrong older anchor")
			}
			return historyWindowJSON(0, 64, 128), nil
		}
		return historyWindowJSON(64, 128, 128), nil
	}}
	a, updates := sessionTestApp(client)
	a.canHistoryWindow = true
	a.model.History.Available = true
	a.model.History.Start = 64
	a.nextSeq = 128
	a.model.Draft = "keep draft"
	for seq := uint64(64); seq < 128; seq++ {
		a.transcript.Apply(projection.Event{Seq: seq, Type: "assistant/message", Data: json.RawMessage(fmt.Sprintf(`{"message":{"content":[{"type":"text","text":"answer %d"}]}}`, seq))})
	}
	a.publishTranscript()
	a.LoadEarlierHistory()
	a.Send("blocked during read")
	completeSessionJob(t, a, updates)
	if len(a.model.Items) != 128 || a.model.History.Start != 0 || a.nextSeq != 128 || a.model.Draft != "keep draft" {
		t.Fatal("history prepend changed current state")
	}
	fail = true
	a.model.History.Start = 64
	a.LoadEarlierHistory()
	completeSessionJob(t, a, updates)
	if len(a.model.Items) != 128 || !strings.Contains(a.model.History.Error, "Could not load") || strings.Contains(a.model.History.Error, "private-token") || a.model.History.Start != 64 {
		t.Fatal("failed older read discarded state")
	}
	fail = false
	a.model.History.Detached = true
	a.Send("blocked in old window")
	if a.model.Phase != view.PhaseIdle || a.model.Draft != "keep draft" {
		t.Fatal("sent from old window")
	}
	a.ReturnToLatest()
	completeSessionJob(t, a, updates)
	if a.model.History.Detached || len(a.model.Items) != 64 || a.model.History.Start != 64 {
		t.Fatal("latest window did not replace old cache")
	}
}

func TestInactiveReleaseFailureKeepsCurrentSessionAndRetries(t *testing.T) {
	releases := 0
	fail := true
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method == "session/release" {
			releases++
			if fail {
				return nil, errors.New("private-token")
			}
			return json.RawMessage(`{}`), nil
		}
		if method == "session/resume" {
			return json.RawMessage(`{"session":{"id":"target","title":"","cwd":"C:/work","provider":"test","model":"echo","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"nextSeq":0}}`), nil
		}
		return json.RawMessage(`{"events":[],"nextSeq":0,"hasMore":false}`), nil
	}}
	a, updates := sessionTestApp(client)
	a.canRelease = true
	a.retiredSessions = map[string]bool{"inactive": true}
	a.model.Draft = "current"
	a.SwitchSession("target")
	completeSessionJob(t, a, updates)
	if a.sessionID != "original" || a.model.Draft != "current" || !a.retiredSessions["inactive"] {
		t.Fatal("failed cleanup lost current state or retry identity")
	}
	fail = false
	a.SwitchSession("target")
	completeSessionJob(t, a, updates)
	if a.sessionID != "target" || releases != 2 || a.retiredSessions["inactive"] || !a.retiredSessions["original"] {
		t.Fatal("inactive cleanup was not retried")
	}
}
