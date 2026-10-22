package projection

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func streamFrame(t *testing.T, stream *AssistantStream, wire string) {
	t.Helper()
	if err := stream.Apply(json.RawMessage(wire)); err != nil {
		t.Fatal(err)
	}
}

func startStream(t *testing.T, stream *AssistantStream) {
	t.Helper()
	streamFrame(t, stream, `{"type":"start","attemptId":"s1:1","revision":1,"turn":1,"step":1}`)
	streamFrame(t, stream, `{"type":"chunk","attemptId":"s1:1","revision":2,"index":0,"time":1,"chunk":{"type":"block-start","index":0,"blockType":"text"}}`)
	streamFrame(t, stream, `{"type":"chunk","attemptId":"s1:1","revision":3,"index":1,"time":1,"chunk":{"type":"text-delta","index":0,"text":"你好"}}`)
}

func TestStreamCommitBeforeAndAfterEnd(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprint(before), func(t *testing.T) {
			var stream AssistantStream
			startStream(t, &stream)
			item, visible := stream.Item()
			if !visible || item.Text != "你好" || item.State != "streaming" {
				t.Fatal("prefix not visible")
			}
			event := Event{Type: "assistant/message", Seq: 9, Data: json.RawMessage(`{"turn":1,"step":1,"message":{"content":[{"type":"text","text":"你好世界"}]}}`)}
			if before {
				stream.Commit(event)
			}
			streamFrame(t, &stream, `{"type":"end","attemptId":"s1:1","revision":4,"index":2,"outcome":{"kind":"committed","eventType":"assistant/message","seq":9}}`)
			if !before {
				if _, visible := stream.Item(); !visible {
					t.Fatal("end removed prefix before durable commit")
				}
				stream.Commit(event)
			}
			if _, visible := stream.Item(); visible {
				t.Fatal("commit left duplicate preview")
			}
		})
	}
}

func TestStreamGapsDuplicatesAbandonmentAndRetry(t *testing.T) {
	for _, ending := range []string{
		`{"type":"chunk","attemptId":"s1:1","revision":5,"index":2,"time":1,"chunk":{"type":"text-delta","index":0,"text":"wrong"}}`,
		`{"type":"chunk","attemptId":"s1:1","revision":4,"index":3,"time":1,"chunk":{"type":"text-delta","index":0,"text":"wrong"}}`,
		`{"type":"end","attemptId":"s1:1","revision":4,"index":2,"outcome":{"kind":"abandoned"}}`,
	} {
		var stream AssistantStream
		startStream(t, &stream)
		streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":3,"index":1,"time":1,"chunk":{"type":"text-delta","index":0,"text":"duplicate"}}`)
		if item, _ := stream.Item(); item.Text != "你好" {
			t.Fatal("duplicate appended twice")
		}
		streamFrame(t, &stream, ending)
		if _, visible := stream.Item(); visible {
			t.Fatal("unsafe prefix retained")
		}
		streamFrame(t, &stream, `{"type":"start","attemptId":"s1:2","revision":10,"turn":1,"step":1}`)
		streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:2","revision":11,"index":0,"time":1,"chunk":{"type":"block-start","index":0,"blockType":"text"}}`)
		streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:2","revision":12,"index":1,"time":1,"chunk":{"type":"text-delta","index":0,"text":"retry"}}`)
		if item, _ := stream.Item(); item.Text != "retry" {
			t.Fatal("retry mixed attempts")
		}
	}
}

func TestStreamNeverDisplaysReasoningOrToolArgumentsAndBoundsText(t *testing.T) {
	var stream AssistantStream
	streamFrame(t, &stream, `{"type":"start","attemptId":"s1:1","revision":1,"turn":1,"step":1}`)
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":2,"index":0,"time":1,"chunk":{"type":"block-start","index":0,"blockType":"reasoning"}}`)
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":3,"index":1,"time":1,"chunk":{"type":"reasoning-delta","index":0,"text":"private reasoning"}}`)
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":4,"index":2,"time":1,"chunk":{"type":"tool-call-delta","index":1,"argumentsDelta":"private arguments"}}`)
	if _, visible := stream.Item(); visible {
		t.Fatal("non-text content became visible")
	}
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":5,"index":3,"time":1,"chunk":{"type":"block-start","index":2,"blockType":"text"}}`)
	streamFrame(t, &stream, fmt.Sprintf(`{"type":"chunk","attemptId":"s1:1","revision":6,"index":4,"time":1,"chunk":{"type":"text-delta","index":2,"text":%q}}`, strings.Repeat("界", 10000)))
	item, visible := stream.Item()
	if !visible || !utf8.ValidString(item.Text) || utf8.RuneCountInString(item.Text) > 8001 {
		t.Fatal("preview limit or Unicode broken")
	}
	stream.Clear()
	if _, visible := stream.Item(); visible {
		t.Fatal("clear retained preview")
	}
}

func TestStreamRejectsMalformedFrames(t *testing.T) {
	for _, wire := range []string{`null`, `{}`, `{"type":"start","attemptId":"x","revision":null,"turn":1,"step":1}`, `{"type":"start","attemptId":"x","revision":-1,"turn":1,"step":1}`, `{"type":"chunk","attemptId":"x","revision":2,"index":0,"time":1,"chunk":{"type":"text-delta","index":0,"text":null}}`} {
		var stream AssistantStream
		if err := stream.Apply(json.RawMessage(wire)); err == nil {
			t.Fatal("invalid stream accepted")
		}
	}
}
