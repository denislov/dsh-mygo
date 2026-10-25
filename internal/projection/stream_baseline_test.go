package projection

import (
	"encoding/json"
	"strings"
	"testing"
)

const textBaseline = `{"revision":3,"activeAttempt":{"attemptId":"s1:1","turn":1,"step":1,"nextIndex":2,"blocks":[{"index":0,"type":"text","text":"你好","ended":false}],"truncated":false}}`

func TestStreamBaselineContinuesAndIgnoresOlderFrames(t *testing.T) {
	var stream AssistantStream
	if err := stream.Restore(json.RawMessage(textBaseline)); err != nil {
		t.Fatal(err)
	}
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":3,"index":1,"time":1,"chunk":{"type":"text-delta","index":0,"text":"duplicate"}}`)
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":4,"index":2,"time":1,"chunk":{"type":"text-delta","index":0,"text":"世界"}}`)
	if item, ok := streamTextItem(t, &stream); !ok || item.Text != "你好世界" {
		t.Fatal("baseline did not continue")
	}
	if stream.NeedsResync() {
		t.Fatal("continuous baseline marked broken")
	}
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":6,"index":3,"time":1,"chunk":{"type":"text-delta","index":0,"text":"gap"}}`)
	if !stream.NeedsResync() {
		t.Fatal("gap not detectable")
	}
	if err := stream.Restore(json.RawMessage(`{"revision":6}`)); err != nil {
		t.Fatal(err)
	}
	if stream.NeedsResync() {
		t.Fatal("baseline did not reset gap")
	}
	if _, ok := streamTextItem(t, &stream); ok {
		t.Fatal("empty baseline retained text")
	}
}

func TestStreamBaselineRejectsMalformedOrOversizedStateAtomically(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `{"revision":9007199254740992}`, `{"revision":0,"activeAttempt":{}}`,
		`{"revision":3,"activeAttempt":null}`,
		`{"revision":3,"activeAttempt":{"attemptId":"a","turn":1,"step":1,"nextIndex":1,"blocks":[{"index":0,"type":"text","ended":false}],"truncated":false}}`,
		`{"revision":3,"activeAttempt":{"attemptId":"a","turn":1,"step":1,"nextIndex":0,"blocks":[],"truncated":null}}`,
		strings.Replace(textBaseline, `你好`, strings.Repeat("x", 8001), 1),
		strings.Replace(textBaseline, `[{"index":0,"type":"text","text":"你好","ended":false}]`, `[{"index":0,"type":"text","text":"a","ended":false},{"index":0,"type":"text","text":"b","ended":false}]`, 1),
		strings.Repeat(" ", 128*1024) + `{"revision":0}`,
	} {
		var stream AssistantStream
		startStream(t, &stream)
		if err := stream.Restore(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid baseline: %s", raw)
		}
		if item, _ := streamTextItem(t, &stream); item.Text != "你好" {
			t.Fatal("failed baseline mutated current state")
		}
	}
}

func TestStreamBaselineRestoresReasoningText(t *testing.T) {
	var stream AssistantStream
	raw := `{"revision":4,"activeAttempt":{"attemptId":"s1:1","turn":1,"step":2,"nextIndex":3,"blocks":[{"index":0,"type":"reasoning","text":"plan","ended":true},{"index":1,"type":"text","text":"answer","ended":false}],"truncated":false}}`
	if err := stream.Restore(json.RawMessage(raw)); err != nil {
		t.Fatal(err)
	}
	items := stream.Items()
	if len(items) != 2 || items[0].Kind != KindReasoning || items[0].Text != "plan" || items[0].ActivityKey != "activity:1:2:r:0" || items[1].Kind != KindAssistant || items[1].Text != "answer" {
		t.Fatalf("restored items = %+v", items)
	}
	oversized := strings.Replace(raw, `"text":"plan"`, `"text":"`+strings.Repeat("想", 8001)+`"`, 1)
	var broken AssistantStream
	if broken.Restore(json.RawMessage(oversized)) == nil {
		t.Fatal("oversized reasoning baseline accepted")
	}
}

func TestStreamBaselineRetainsBlockBoundariesAndTruncation(t *testing.T) {
	var stream AssistantStream
	raw := `{"revision":8,"activeAttempt":{"attemptId":"s1:1","turn":1,"step":1,"nextIndex":7,"blocks":[{"index":0,"type":"reasoning","ended":true},{"index":1,"type":"text","text":"prefix","ended":false}],"truncated":true}}`
	if err := stream.Restore(json.RawMessage(raw)); err != nil {
		t.Fatal(err)
	}
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":9,"index":7,"time":1,"chunk":{"type":"block-end","index":1,"block":{"type":"text","text":"prefix complete"}}}`)
	if item, ok := streamTextItem(t, &stream); !ok || item.Text != "prefix complete…" || !item.Truncated {
		t.Fatal("baseline lost block state or truncation")
	}
	stream.Commit(Event{Type: "assistant/message", Seq: 12, Data: json.RawMessage(`{"turn":1,"step":1}`)})
	if _, ok := streamTextItem(t, &stream); ok {
		t.Fatal("baseline duplicated durable commit")
	}
}
