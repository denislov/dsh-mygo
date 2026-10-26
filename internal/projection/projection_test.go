package projection

import (
	"encoding/json"
	"strings"
	"testing"
)

func event(seq uint64, eventType, data string) Event {
	return Event{Type: eventType, Seq: seq, Data: json.RawMessage(data)}
}

func TestApplySkipsSyntheticUserContext(t *testing.T) {
	var transcript Transcript
	changed := transcript.Apply(event(1, "user/message", `{
		"source": {"kind": "runtime-context"},
		"content": [{"type": "text", "text": "Current runtime context"}]
	}`))
	if changed {
		t.Fatal("synthetic context was projected")
	}
	if len(transcript.Items()) != 0 {
		t.Fatalf("items = %+v", transcript.Items())
	}
}

func TestApplyUserAndAssistantMessages(t *testing.T) {
	var transcript Transcript
	if !transcript.Apply(event(1, "user/message", `{
		"source": {"kind": "user"},
		"content": [{"type": "text", "text": "hello"}]
	}`)) {
		t.Fatal("user message did not change the transcript")
	}
	if !transcript.Apply(event(2, "assistant/message", `{
		"message": {"content": [
			{"type": "reasoning", "text": "private"},
			{"type": "text", "text": "pong"}
		]}
	}`)) {
		t.Fatal("assistant message did not change the transcript")
	}
	items := transcript.Items()
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Kind != KindUser || items[0].Title != "You" || items[0].Text != "hello" {
		t.Fatalf("user item = %+v", items[0])
	}
	if items[1].Kind != KindReasoning || items[1].Title != "Reasoning" || items[1].Text != "private" || items[1].ID != "seq:2:r:0" {
		t.Fatalf("reasoning item = %+v", items[1])
	}
	if items[2].Kind != KindAssistant || items[2].Title != "Assistant" || items[2].Text != "pong" || items[2].ID != "seq:2" {
		t.Fatalf("assistant item = %+v", items[2])
	}
}

func TestAssistantReasoningRunsKeepOrderAndStayOutOfText(t *testing.T) {
	var transcript Transcript
	transcript.Apply(event(1, "assistant/message", `{
		"turn": 2, "step": 1,
		"message": {"content": [
			{"type": "reasoning", "text": "plan A"},
			{"type": "text", "text": "answer A"},
			{"type": "reasoning", "text": "plan B"},
			{"type": "text", "text": "answer B"}
		]}
	}`))
	items := transcript.Items()
	if len(items) != 4 {
		t.Fatalf("items = %+v", items)
	}
	wantKinds := []Kind{KindReasoning, KindAssistant, KindReasoning, KindAssistant}
	wantIDs := []string{"seq:1:r:0", "seq:1", "seq:1:r:1", "seq:1:t:1"}
	wantText := []string{"plan A", "answer A", "plan B", "answer B"}
	for index, item := range items {
		if item.Kind != wantKinds[index] || item.ID != wantIDs[index] || item.Text != wantText[index] {
			t.Fatalf("item %d = %+v", index, item)
		}
		if item.Turn != 2 || item.Step != 1 {
			t.Fatalf("item %d position = %d/%d", index, item.Turn, item.Step)
		}
	}
	if items[0].ActivityKey != "activity:2:1:r:0" || items[2].ActivityKey != "activity:2:1:r:1" {
		t.Fatalf("activity keys = %q, %q", items[0].ActivityKey, items[2].ActivityKey)
	}
}

func TestMalformedPositionsDoNotDropReasoning(t *testing.T) {
	var transcript Transcript
	transcript.Apply(event(1, "assistant/message", `{
		"turn": "two", "step": -1,
		"message": {"content": [
			{"type": "reasoning", "text": "still here"},
			{"type": "text", "text": "visible"}
		]}
	}`))
	items := transcript.Items()
	if len(items) != 2 || items[0].Text != "still here" || items[1].Text != "visible" {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Turn != 0 || items[0].Step != 0 {
		t.Fatalf("malformed position accepted: %+v", items[0])
	}
}

func TestToolRowsCarryTurnAndStep(t *testing.T) {
	var transcript Transcript
	transcript.Apply(event(1, "turn/start", `{"turn":3}`))
	transcript.Apply(event(2, "tool/call", `{"callId":"c1","name":"read","arguments":"{}"}`))
	transcript.Apply(Event{Seq: 3, Time: 2000, Type: "tool/result", Data: json.RawMessage(`{
		"turn": 3, "step": 2,
		"message": {"toolCallId":"c1","content":[{"type":"text","text":"done"}]}
	}`)})
	items := transcript.Items()
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Turn != 3 || items[0].Step != 2 || items[0].EndTime != 2000 {
		t.Fatalf("tool position = %+v", items[0])
	}
}

func TestApplyToolCallAndResultPair(t *testing.T) {
	var transcript Transcript
	transcript.Apply(event(1, "tool/call", `{
		"callId": "c1", "name": "read_file", "arguments": "{\"path\":\"a.txt\"}"
	}`))
	if items := transcript.Items(); len(items) != 1 || items[0].State != "running" {
		t.Fatalf("after call: %+v", items)
	}
	transcript.Apply(event(2, "tool/result", `{
		"message": {
			"toolCallId": "c1",
			"content": [{"type": "text", "text": "file contents"}],
			"isError": false
		}
	}`))
	items := transcript.Items()
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].State != "done" || items[0].Detail != "file contents" || items[0].IsError {
		t.Fatalf("result item = %+v", items[0])
	}
	if !strings.Contains(items[0].Text, `"path": "a.txt"`) {
		t.Fatalf("arguments = %q", items[0].Text)
	}
}

func TestApplyToolResultErrorUsesReason(t *testing.T) {
	var transcript Transcript
	transcript.Apply(event(1, "tool/call", `{
		"callId": "c1", "name": "shell", "arguments": "{}"
	}`))
	transcript.Apply(event(2, "tool/result", `{
		"message": {
			"toolCallId": "c1",
			"content": [{"type": "text", "text": "raw"}],
			"isError": true
		},
		"error": {"code": "DENIED", "reason": "permission denied"}
	}`))
	item := transcript.Items()[0]
	if item.State != "error" || !item.IsError || item.Detail != "permission denied" {
		t.Fatalf("item = %+v", item)
	}
}

func TestApplyTurnEndErrorAddsNotice(t *testing.T) {
	var transcript Transcript
	transcript.Apply(event(1, "turn/end", `{
		"turn": 1,
		"reason": {"kind": "error", "error": {"message": "network down", "code": "UNKNOWN"}}
	}`))
	items := transcript.Items()
	if len(items) != 1 || items[0].Kind != KindNotice || !items[0].IsError {
		t.Fatalf("items = %+v", items)
	}
	if !strings.Contains(items[0].Text, "network down") {
		t.Fatalf("text = %q", items[0].Text)
	}
}

func TestApplyIgnoresCompletedTurnAndDuplicateSeq(t *testing.T) {
	var transcript Transcript
	completed := event(1, "turn/end", `{"reason": {"kind": "completed"}}`)
	if transcript.Apply(completed) {
		t.Fatal("completed turn changed the transcript")
	}
	user := event(2, "user/message", `{
		"source": {"kind": "user"},
		"content": [{"type": "text", "text": "hi"}]
	}`)
	if !transcript.Apply(user) {
		t.Fatal("first user message was skipped")
	}
	if transcript.Apply(user) {
		t.Fatal("duplicate event changed the transcript")
	}
	if len(transcript.Items()) != 1 {
		t.Fatalf("items = %+v", transcript.Items())
	}
}

func TestDecodeEventValidatesTypeAndReportsSequencePresence(t *testing.T) {
	event, sequence, err := DecodeEvent(json.RawMessage(`{"type":"turn/start","seq":7,"time":1,"data":{"turn":1}}`))
	if err != nil || event.Type != "turn/start" || event.Seq != 7 || sequence == nil || *sequence != 7 {
		t.Fatalf("event = %+v, sequence = %v, err = %v", event, sequence, err)
	}
	event, sequence, err = DecodeEvent(json.RawMessage(`{"type":"turn/start","data":{}}`))
	if err != nil || sequence != nil || event.Seq != 0 {
		t.Fatalf("missing seq = %+v, %v, %v", event, sequence, err)
	}
	for _, raw := range []string{`{}`, `{"seq":1,"time":1,"data":{}}`, `not json`} {
		if _, _, err := DecodeEvent(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid event %s", raw)
		}
	}
}

func TestClipBoundsLongText(t *testing.T) {
	text := clip(strings.Repeat("a", 20), 10)
	if text != strings.Repeat("a", 10)+"…" {
		t.Fatalf("clip = %q", text)
	}
}
