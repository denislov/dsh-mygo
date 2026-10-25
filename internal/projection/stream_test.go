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

func streamTextItem(t *testing.T, stream *AssistantStream) (Item, bool) {
	t.Helper()
	for _, item := range stream.Items() {
		if item.Kind == KindAssistant {
			return item, true
		}
	}
	return Item{}, false
}

func TestStreamCommitBeforeAndAfterEnd(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprint(before), func(t *testing.T) {
			var stream AssistantStream
			startStream(t, &stream)
			item, visible := streamTextItem(t, &stream)
			if !visible || item.Text != "你好" || item.State != "streaming" {
				t.Fatal("prefix not visible")
			}
			event := Event{Type: "assistant/message", Seq: 9, Data: json.RawMessage(`{"turn":1,"step":1,"message":{"content":[{"type":"text","text":"你好世界"}]}}`)}
			if before {
				stream.Commit(event)
			}
			streamFrame(t, &stream, `{"type":"end","attemptId":"s1:1","revision":4,"index":2,"outcome":{"kind":"committed","eventType":"assistant/message","seq":9}}`)
			if !before {
				if _, visible := streamTextItem(t, &stream); !visible {
					t.Fatal("end removed prefix before durable commit")
				}
				stream.Commit(event)
			}
			if _, visible := streamTextItem(t, &stream); visible {
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
		if item, _ := streamTextItem(t, &stream); item.Text != "你好" {
			t.Fatal("duplicate appended twice")
		}
		streamFrame(t, &stream, ending)
		if _, visible := streamTextItem(t, &stream); visible {
			t.Fatal("unsafe prefix retained")
		}
		streamFrame(t, &stream, `{"type":"start","attemptId":"s1:2","revision":10,"turn":1,"step":1}`)
		streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:2","revision":11,"index":0,"time":1,"chunk":{"type":"block-start","index":0,"blockType":"text"}}`)
		streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:2","revision":12,"index":1,"time":1,"chunk":{"type":"text-delta","index":0,"text":"retry"}}`)
		if item, _ := streamTextItem(t, &stream); item.Text != "retry" {
			t.Fatal("retry mixed attempts")
		}
	}
}

func TestStreamDisplaysReasoningButNotToolArguments(t *testing.T) {
	var stream AssistantStream
	streamFrame(t, &stream, `{"type":"start","attemptId":"s1:1","revision":1,"turn":1,"step":1}`)
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":2,"index":0,"time":10,"chunk":{"type":"block-start","index":0,"blockType":"reasoning"}}`)
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":3,"index":1,"time":20,"chunk":{"type":"reasoning-delta","index":0,"text":"private reasoning"}}`)
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":4,"index":2,"time":30,"chunk":{"type":"tool-call-delta","index":1,"argumentsDelta":"private arguments"}}`)
	items := stream.Items()
	if len(items) != 1 || items[0].Kind != KindReasoning || items[0].Text != "private reasoning" {
		t.Fatalf("reasoning preview = %+v", items)
	}
	if items[0].ActivityKey != "activity:1:1:r:0" || items[0].Turn != 1 || items[0].Step != 1 {
		t.Fatalf("reasoning identity = %+v", items[0])
	}
	if items[0].Time != 10 || items[0].EndTime != 20 {
		t.Fatalf("reasoning times = %+v", items[0])
	}
	if strings.Contains(items[0].Text, "private arguments") {
		t.Fatal("tool arguments leaked into the preview")
	}
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":5,"index":3,"time":40,"chunk":{"type":"block-start","index":2,"blockType":"text"}}`)
	streamFrame(t, &stream, fmt.Sprintf(`{"type":"chunk","attemptId":"s1:1","revision":6,"index":4,"time":50,"chunk":{"type":"text-delta","index":2,"text":%q}}`, strings.Repeat("界", 10000)))
	item, visible := streamTextItem(t, &stream)
	if !visible || !utf8.ValidString(item.Text) || utf8.RuneCountInString(item.Text) > 8001 {
		t.Fatal("preview limit or Unicode broken")
	}
	if item.Time != 40 || item.EndTime != 50 {
		t.Fatalf("text times = %+v", item)
	}
	stream.Clear()
	if len(stream.Items()) != 0 {
		t.Fatal("clear retained preview")
	}
}

func TestStreamReasoningBlockEndCorrectsPrefixAndSharesNoTextBudget(t *testing.T) {
	var stream AssistantStream
	streamFrame(t, &stream, `{"type":"start","attemptId":"s1:1","revision":1,"turn":1,"step":2}`)
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":2,"index":0,"time":1,"chunk":{"type":"block-start","index":0,"blockType":"reasoning"}}`)
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":3,"index":1,"time":1,"chunk":{"type":"reasoning-delta","index":0,"text":"draft"}}`)
	streamFrame(t, &stream, fmt.Sprintf(`{"type":"chunk","attemptId":"s1:1","revision":4,"index":2,"time":1,"chunk":{"type":"block-end","index":0,"block":{"type":"reasoning","text":%q}}}`, strings.Repeat("想", 9000)))
	items := stream.Items()
	if len(items) != 1 || utf8.RuneCountInString(items[0].Text) != maxReasoningRunes || !items[0].Truncated {
		t.Fatalf("reasoning block-end = %+v", items)
	}
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":5,"index":3,"time":1,"chunk":{"type":"block-start","index":1,"blockType":"text"}}`)
	streamFrame(t, &stream, `{"type":"chunk","attemptId":"s1:1","revision":6,"index":4,"time":1,"chunk":{"type":"text-delta","index":1,"text":"answer"}}`)
	item, visible := streamTextItem(t, &stream)
	if !visible || item.Text != "answer" || item.Truncated {
		t.Fatalf("reasoning starved text: visible=%v item=%+v items=%+v", visible, item, stream.Items())
	}
	reasoning := stream.Items()[0]
	if !reasoning.Truncated || utf8.RuneCountInString(reasoning.Text) != maxReasoningRunes {
		t.Fatalf("reasoning budget = %d/%v", utf8.RuneCountInString(reasoning.Text), reasoning.Truncated)
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
