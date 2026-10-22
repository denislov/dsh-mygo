package projection

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestToolDetailsAndHunkDiffs(t *testing.T) {
	var transcript Transcript
	transcript.Apply(event(1, "tool/call", `{"callId":"c1","name":"edit","arguments":"{\"file_path\":\"a.txt\"}"}`))
	previous := transcript.Items()[0]
	transcript.Apply(event(2, "tool/result", `{"message":{"toolCallId":"c1","content":[{"type":"text","text":"edited"}]},"meta":{"diffs":[{"path":"a.txt","oldText":"old","newText":"new"},{"path":"b.txt","oldText":null,"newText":"created"}],"operation":"update"}}`))
	item := transcript.Items()[0]
	if item.Tool == nil || item.Tool.Arguments != `{"file_path":"a.txt"}` || item.Tool.Result != "edited" || len(item.Tool.Diffs) != 2 || !item.Tool.Diffs[1].OldTextMissing {
		t.Fatal("tool details or diff shape lost")
	}
	if previous.State != "running" || previous.Tool.Result != "" {
		t.Fatal("published snapshot mutated")
	}
	if item.Tool.Diffs[0].OldText != "old" || item.Tool.Diffs[0].NewText != "new" {
		t.Fatal("hunk content changed")
	}
}

func TestToolResultBeforeCallPairsByExactID(t *testing.T) {
	var transcript Transcript
	transcript.Apply(event(1, "tool/result", `{"message":{"toolCallId":"c1","content":[{"type":"text","text":"failure output"}]},"error":{"reason":"Denied"}}`))
	identity := transcript.Items()[0].ID
	transcript.Apply(event(2, "tool/call", `{"callId":"c1","name":"edit","arguments":"{}"}`))
	if len(transcript.Items()) != 1 {
		t.Fatal("late call duplicated result")
	}
	item := transcript.Items()[0]
	if item.ID != identity || item.State != "error" || item.Tool.Result != "failure output" || item.Tool.Arguments != "{}" || item.Detail != "Denied" {
		t.Fatal("late call erased result or identity")
	}
}

func TestMalformedAndOversizedDiffsFallBackWithoutPartialPresentation(t *testing.T) {
	for _, metadata := range []string{`{"diffs":[{"path":"a","oldText":3,"newText":"b"}]}`, `{"diffs":[{"path":"a","newText":"b"}]}`, `{"diffs":[{"path":"a","oldText":null,"newText":"b"},{"path":"b","oldText":null,"newText":null}]}`, `{"diffs":{}}`, `null`} {
		var transcript Transcript
		transcript.Apply(event(1, "tool/result", `{"message":{"toolCallId":"c1","content":[{"type":"text","text":"result retained"}]},"meta":`+metadata+`}`))
		if details := transcript.Items()[0].Tool; details == nil || details.Result != "result retained" || len(details.Diffs) != 0 {
			t.Fatal("bad metadata destroyed result or showed partial diff")
		}
	}
	var transcript Transcript
	metadata, _ := json.Marshal(map[string]any{"diffs": []any{map[string]any{"path": "a", "oldText": "before", "newText": strings.Repeat("界", 70000)}}})
	transcript.Apply(event(1, "tool/result", `{"message":{"toolCallId":"c1","content":[]},"meta":`+string(metadata)+`}`))
	if details := transcript.Items()[0].Tool; len(details.Diffs) != 0 || details.DiffNotice == "" {
		t.Fatal("oversized diff was presented as complete")
	}
}

func TestToolDetailLimitsAndEviction(t *testing.T) {
	var transcript Transcript
	for index := 0; index < 80; index++ {
		arguments, _ := json.Marshal(strings.Repeat("界", 30000))
		transcript.Apply(event(uint64(index), "tool/call", fmt.Sprintf(`{"callId":"c%d","name":"read","arguments":%s}`, index, arguments)))
	}
	var retained int
	for _, item := range transcript.Items() {
		if item.Tool != nil {
			retained += item.Tool.Size()
			if !utf8.ValidString(item.Tool.Arguments) || !item.Tool.ArgumentsTruncated {
				t.Fatal("Unicode or truncation marker lost")
			}
		}
	}
	if retained > MaxToolCacheBytes || transcript.Items()[0].Tool != nil || !transcript.Items()[0].ToolEvicted {
		t.Fatal("detail cache not bounded")
	}
}

func TestEmptyDiffCreationIsNotAnUnknownMutation(t *testing.T) {
	var transcript Transcript
	transcript.Apply(event(1, "tool/result", `{"message":{"toolCallId":"c1","content":[]},"meta":{"operation":"create","path":"empty.txt","diffs":[]}}`))
	if details := transcript.Items()[0].Tool; details.DiffNotice != "Created file (no textual hunks)." {
		t.Fatal("empty creation lost meaning")
	}
}
