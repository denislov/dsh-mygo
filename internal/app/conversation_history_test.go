package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"dsh-mygo/internal/protocol"
)

func TestConversationHistoryCompletesLongTurnFromFrozenCursor(t *testing.T) {
	events := make([]map[string]any, 210)
	for i := range events {
		events[i] = map[string]any{"seq": i, "time": 1, "type": "request/metadata", "data": map[string]any{}}
	}
	events[1]["type"], events[1]["data"] = "turn/start", map[string]any{"turn": 1}
	events[7]["type"], events[7]["data"] = "turn/start", map[string]any{"turn": 2}
	events[8]["type"], events[8]["data"] = "user/message", map[string]any{"source": map[string]any{"kind": "user"}, "content": []any{map[string]any{"type": "text", "text": "complete prompt"}}}
	events[208]["type"], events[208]["data"] = "assistant/message", map[string]any{"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "complete answer"}}}}
	events[209]["type"], events[209]["data"] = "turn/end", map[string]any{"reason": map[string]any{"kind": "completed"}}
	calls := 0
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		request, ok := params.(protocol.SessionHistoryParams)
		if method != "session/history" || !ok || request.Direction != protocol.HistoryDirectionBackward || request.Limit != 64 || !request.Live || !request.AssistantStream {
			t.Fatalf("history request = %T %+v", params, params)
		}
		end := 210
		if calls > 0 {
			if request.BeforeSeq != nil {
				t.Fatal("continuation started a new snapshot")
			}
			if _, err := fmt.Sscanf(request.Cursor, "at-%d", &end); err != nil {
				t.Fatal(err)
			}
		}
		start := max(0, end-64)
		page := map[string]any{"events": events[start:end], "startSeq": start, "nextSeq": 210, "hasMore": start > 0, "status": "running", "statusRevision": 4, "assistantStream": map[string]any{"revision": 0}}
		if start > 0 {
			page["nextCursor"] = fmt.Sprintf("at-%d", start)
		}
		calls++
		return json.Marshal(page)
	}}
	snapshot, err := readConversationHistory(context.Background(), client, "saved", 210, nil, true, true)
	if err != nil {
		t.Fatal(err)
	}
	items := snapshot.Transcript.Items()
	if calls != 4 || snapshot.StartSeq != 7 || snapshot.NextSeq != 210 || snapshot.StatusRevision != 4 || len(items) != 2 || items[0].Text != "complete prompt" || items[1].Text != "complete answer" {
		t.Fatalf("incomplete turn: calls=%d snapshot=%+v items=%+v", calls, snapshot, items)
	}
}

func TestConversationHistoryRejectsChangedSnapshotBeforePublishing(t *testing.T) {
	calls := 0
	client := &sessionClient{request: func(_ context.Context, _ string, _ any) (json.RawMessage, error) {
		calls++
		if calls == 1 {
			return json.RawMessage(`{"events":[{"seq":99,"time":1,"type":"turn/end","data":{}}],"startSeq":99,"nextSeq":100,"hasMore":true,"nextCursor":"older"}`), nil
		}
		return json.RawMessage(`{"events":[{"seq":98,"time":1,"type":"turn/start","data":{}}],"startSeq":98,"nextSeq":101,"hasMore":true,"nextCursor":"changed"}`), nil
	}}
	if _, err := readConversationHistory(context.Background(), client, "saved", 100, nil, false, false); err == nil {
		t.Fatal("combined pages from different snapshots")
	}
}
