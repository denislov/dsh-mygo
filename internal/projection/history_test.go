package projection

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestPrependHistoryPairsBoundaryTools(t *testing.T) {
	var earlier, current Transcript
	earlier.Apply(Event{Seq: 7, Type: "tool/call", Data: json.RawMessage(`{"callId":"split","name":"read","arguments":"{}"}`)})
	current.Apply(Event{Seq: 8, Type: "tool/result", Data: json.RawMessage(`{"message":{"toolCallId":"split","content":[{"type":"text","text":"done"}]}}`)})
	current.Prepend(&earlier)
	items := current.Items()
	if len(items) != 1 || items[0].Title != "Tool · read" || items[0].State != "done" || items[0].Tool.Arguments != "{}" || items[0].Tool.Result != "done" {
		t.Fatalf("boundary tool: %+v", items)
	}
	current.Apply(Event{Seq: 8, Type: "tool/result", Data: json.RawMessage(`{}`)})
	if len(current.Items()) != 1 {
		t.Fatal("repeated event duplicated")
	}
}

func TestTrimStartKeepsWholeEvents(t *testing.T) {
	items := []Item{{Seq: 1}, {Seq: 2}, {Seq: 2}, {Seq: 3}, {Seq: 3}, {Seq: 3}}
	if start := trimStart(items, 2, 1<<20); start != 3 {
		t.Fatalf("trim split an event: start = %d", start)
	}
	if start := trimStart(items, 1, 1<<20); start != 3 {
		t.Fatalf("single-row budget split an event: start = %d", start)
	}
	if start := trimStart(items, 6, 1<<20); start != 0 {
		t.Fatalf("untrimmed window moved: start = %d", start)
	}
}

func TestHistoryCacheSlidesAndDedupRangesStaySmall(t *testing.T) {
	var recent, earlier Transcript
	for seq := uint64(0); seq < 25000; seq++ {
		recent.Apply(Event{Seq: seq, Type: "test/event", Data: json.RawMessage(`{}`)})
	}
	if len(recent.seen) != 1 {
		t.Fatal("contiguous hidden events grew the dedup cache")
	}
	for seq := uint64(25000); seq < 25000+MaxHistoryRows; seq++ {
		recent.Apply(Event{Seq: seq, Type: "assistant/message", Data: json.RawMessage(fmt.Sprintf(`{"message":{"content":[{"type":"text","text":"message %d"}]}}`, seq))})
	}
	recent.Apply(Event{Seq: 40000, Type: "assistant/message", Data: json.RawMessage(`{"message":{"content":[{"type":"text","text":"latest"}]}}`)})
	start, trimmed := recent.TrimRecent()
	if !trimmed || start != 25001 || len(recent.Items()) != MaxHistoryRows {
		t.Fatal("live window is not bounded")
	}
	earlier.Apply(Event{Seq: 24999, Type: "assistant/message", Data: json.RawMessage(`{"message":{"content":[{"type":"text","text":"older"}]}}`)})
	if !recent.Prepend(&earlier) || recent.Items()[0].Text != "older" || recent.Items()[len(recent.Items())-1].Text == "latest" {
		t.Fatal("browsing did not slide toward older history")
	}
	var large Transcript
	text := strings.Repeat("中", 7900)
	raw := json.RawMessage(fmt.Sprintf(`{"message":{"content":[{"type":"text","text":%q}]}}`, text))
	for seq := uint64(0); seq < 800; seq++ {
		large.Apply(Event{Seq: seq, Type: "assistant/message", Data: raw})
	}
	if _, trimmed := large.TrimRecent(); !trimmed || len(large.Items()) >= 800 {
		t.Fatal("preview byte budget ignored")
	}
}
