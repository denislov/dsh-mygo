package projection

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAssistantMarkdownPreservesWhitespaceAndMarksTruncation(t *testing.T) {
	source := "    indented code\n\n```go\n  fmt.Println(\"你好\")\n```\n"
	var transcript Transcript
	data, _ := json.Marshal(map[string]any{"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": source}}}})
	transcript.Apply(Event{Type: "assistant/message", Seq: 1, Data: data})
	if item := transcript.Items()[0]; item.Text != source || item.Truncated {
		t.Fatal("Markdown source whitespace changed")
	}
	data, _ = json.Marshal(map[string]any{"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": strings.Repeat("界", maxMessageRunes+1)}}}})
	transcript.Apply(Event{Type: "assistant/message", Seq: 2, Data: data})
	if !transcript.Items()[1].Truncated {
		t.Fatal("truncated source presented as complete")
	}
}

func TestLongAssistantReplySurvivesHistoryProjection(t *testing.T) {
	source := strings.Repeat("一轮完整回复的内容。", 1500)
	data, _ := json.Marshal(map[string]any{"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": source}}}})
	var transcript Transcript
	transcript.Apply(Event{Type: "assistant/message", Seq: 1, Data: data})
	item := transcript.Items()[0]
	if item.Truncated || item.Text != source {
		t.Fatal("ordinary long reply was cut off")
	}
}

func TestAssistantReasoningHasItsOwnBudget(t *testing.T) {
	var transcript Transcript
	data, _ := json.Marshal(map[string]any{"message": map[string]any{"content": []any{
		map[string]any{"type": "reasoning", "text": strings.Repeat("想", 9000)},
		map[string]any{"type": "text", "text": "answer"},
	}}})
	transcript.Apply(Event{Type: "assistant/message", Seq: 1, Data: data})
	items := transcript.Items()
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Kind != KindReasoning || !items[0].Truncated || utf8.RuneCountInString(items[0].Text) != 8000 {
		t.Fatalf("reasoning truncation = %+v", items[0])
	}
	if items[1].Kind != KindAssistant || items[1].Text != "answer" || items[1].Truncated {
		t.Fatalf("text starved by reasoning: %+v", items[1])
	}
}

func TestAssistantRunCountIsBounded(t *testing.T) {
	blocks := make([]any, 0, 20)
	for index := 0; index < 10; index++ {
		blocks = append(blocks, map[string]any{"type": "reasoning", "text": fmt.Sprintf("thought %d", index)})
		blocks = append(blocks, map[string]any{"type": "text", "text": fmt.Sprintf("answer %d", index)})
	}
	data, _ := json.Marshal(map[string]any{"message": map[string]any{"content": blocks}})
	var transcript Transcript
	transcript.Apply(Event{Type: "assistant/message", Seq: 1, Data: data})
	items := transcript.Items()
	if len(items) != maxAssistantParts {
		t.Fatalf("items = %d", len(items))
	}
	if !items[len(items)-1].Truncated {
		t.Fatal("dropped runs were not marked truncated")
	}
}

func TestStreamMarkdownPreservesOpenFenceWhitespace(t *testing.T) {
	var stream AssistantStream
	streamFrame(t, &stream, `{"type":"start","attemptId":"s1:1","revision":1,"turn":1,"step":1}`)
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":2,"index":0,"time":1,"chunk":{"type":"block-start","index":0,"blockType":"text"}}`)
	source := "```go\n  fmt.Println(\"hello\")\n"
	wire, _ := json.Marshal(map[string]any{"type": "chunk", "attemptId": "s1:1", "revision": 3, "index": 1, "time": 1, "chunk": map[string]any{"type": "text-delta", "index": 0, "text": source}})
	streamFrame(t, &stream, string(wire))
	if item, visible := streamTextItem(t, &stream); !visible || item.Text != source {
		t.Fatal("stream removed code whitespace")
	}
}
