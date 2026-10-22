package projection

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAssistantMarkdownPreservesWhitespaceAndMarksTruncation(t *testing.T) {
	source := "    indented code\n\n```go\n  fmt.Println(\"你好\")\n```\n"
	var transcript Transcript
	data, _ := json.Marshal(map[string]any{"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": source}}}})
	transcript.Apply(Event{Type: "assistant/message", Seq: 1, Data: data})
	if item := transcript.Items()[0]; item.Text != source || item.Truncated {
		t.Fatal("Markdown source whitespace changed")
	}
	data, _ = json.Marshal(map[string]any{"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": strings.Repeat("界", 9000)}}}})
	transcript.Apply(Event{Type: "assistant/message", Seq: 2, Data: data})
	if !transcript.Items()[1].Truncated {
		t.Fatal("truncated source presented as complete")
	}
}

func TestStreamMarkdownPreservesOpenFenceWhitespace(t *testing.T) {
	var stream AssistantStream
	streamFrame(t, &stream, `{"type":"start","attemptId":"s1:1","revision":1,"turn":1,"step":1}`)
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":2,"index":0,"time":1,"chunk":{"type":"block-start","index":0,"blockType":"text"}}`)
	source := "```go\n  fmt.Println(\"hello\")\n"
	wire, _ := json.Marshal(map[string]any{"type": "chunk", "attemptId": "s1:1", "revision": 3, "index": 1, "time": 1, "chunk": map[string]any{"type": "text-delta", "index": 0, "text": source}})
	streamFrame(t, &stream, string(wire))
	if item, visible := stream.Item(); !visible || item.Text != source {
		t.Fatal("stream removed code whitespace")
	}
}
