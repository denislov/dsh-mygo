package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

func TestHistoryReadKeepsShellInteractiveAndRejectsSupersededResults(t *testing.T) {
	for _, action := range []string{"send", "settings"} {
		t.Run(action, func(t *testing.T) {
			client := &sessionClient{request: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
				if method == "session/history" {
					return historyWindowJSON(0, 64, 128), nil
				}
				return json.RawMessage(`{}`), nil
			}}
			a, updates := sessionTestApp(client)
			a.canHistoryWindow, a.model.History.Available, a.model.CanModels = true, true, true
			a.model.History.Start, a.nextSeq = 64, 128
			a.model.Draft = "keep editing"
			a.LoadEarlierHistory()
			operation := a.sessionOperation
			a.LoadEarlierHistory()
			if a.sessionOperation != operation {
				t.Fatal("duplicate history request was not rejected")
			}
			if !a.model.History.Busy || a.model.SessionBusy || !a.model.CanChooseSession() || !a.model.CanEditSettings() || a.model.CanLoadHistory() {
				t.Fatal("background read changed global interaction state")
			}
			var late func()
			select {
			case late = <-updates:
			case <-time.After(3 * time.Second):
				t.Fatal("history result did not arrive")
			}
			if action == "send" {
				a.Send(a.model.Draft)
				if a.model.Phase != view.PhaseRunning {
					t.Fatal("background history blocked sending")
				}
			} else {
				a.OpenSettings()
				if !a.model.SettingsOpen {
					t.Fatal("background history blocked settings")
				}
			}
			late()
			if a.model.History.Busy || a.model.History.Start != 64 || len(a.model.Items) != 0 || a.model.History.Error != "" {
				t.Fatal("superseded history result changed current state")
			}
		})
	}
}

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
		request, ok := params.(protocol.SessionHistoryParams)
		if method != "session/history" || !ok || request.Direction != protocol.HistoryDirectionBackward || request.Limit != 64 || request.Cursor != "" || request.BeforeSeq == nil || *request.BeforeSeq != 30000 {
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
		request, ok := params.(protocol.SessionHistoryParams)
		if !ok {
			t.Error("history request was not typed")
		}
		if request.BeforeSeq != nil {
			if *request.BeforeSeq != 64 {
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
			request, ok := params.(protocol.SessionReleaseParams)
			if !ok || request.SessionID != "inactive" {
				t.Errorf("release request = %T %+v", params, params)
			}
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
