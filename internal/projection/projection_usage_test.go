package projection

import (
	"encoding/json"
	"testing"
)

func TestAssistantUsageBecomesSessionMetrics(t *testing.T) {
	var transcript Transcript
	event := Event{Seq: 1, Type: "assistant/message", Data: json.RawMessage(`{
		"message": {"content": [{"type": "text", "text": "answer"}]},
		"usage": {"inputTokens": 1200, "outputTokens": 350, "totalTokens": 1550, "cacheReadTokens": 200, "reasoningTokens": 40}
	}`)}
	if !transcript.Apply(event) {
		t.Fatal("assistant message with usage was not projected")
	}
	usage := transcript.Usage
	if !usage.HasUsage || usage.InputTokens != 1200 || usage.OutputTokens != 350 || usage.TotalTokens != 1550 || usage.CacheReadTokens != 200 || usage.ReasoningTokens != 40 {
		t.Fatalf("usage = %+v", usage)
	}
	if len(transcript.Items()) != 1 {
		t.Fatalf("items = %+v", transcript.Items())
	}
}

func TestToolOnlyStepsUpdateUsageWithoutARow(t *testing.T) {
	var transcript Transcript
	event := Event{Seq: 1, Type: "assistant/message", Data: json.RawMessage(`{
		"message": {"content": []},
		"usage": {"inputTokens": 10, "outputTokens": 2}
	}`)}
	if !transcript.Apply(event) {
		t.Fatal("tool-only usage change was not reported")
	}
	if len(transcript.Items()) != 0 {
		t.Fatalf("items = %+v", transcript.Items())
	}
	if transcript.Usage.InputTokens != 10 || transcript.Usage.OutputTokens != 2 || transcript.Usage.TotalTokens != 12 {
		t.Fatalf("usage = %+v", transcript.Usage)
	}
}

func TestImplausibleUsageIsDroppedButTextSurvives(t *testing.T) {
	var transcript Transcript
	event := Event{Seq: 1, Type: "assistant/message", Data: json.RawMessage(`{
		"message": {"content": [{"type": "text", "text": "keep"}]},
		"usage": {"inputTokens": -5, "outputTokens": 999999999999999}
	}`)}
	if !transcript.Apply(event) {
		t.Fatal("message was dropped")
	}
	if len(transcript.Items()) != 1 || transcript.Items()[0].Text != "keep" {
		t.Fatalf("items = %+v", transcript.Items())
	}
	if transcript.Usage.InputTokens != 0 || transcript.Usage.OutputTokens != 0 || transcript.Usage.TotalTokens != 0 {
		t.Fatalf("usage = %+v", transcript.Usage)
	}
}

func TestRequestContextProvidesRouteCapacity(t *testing.T) {
	var transcript Transcript
	event := Event{Seq: 1, Type: "request/context", Data: json.RawMessage(`{
		"provider": "deepseek-official", "model": "deepseek-flash", "contextWindow": 128000,
		"systemPromptUpdate": {"kind": "replace"}
	}`)}
	if !transcript.Apply(event) {
		t.Fatal("request context was not projected")
	}
	if transcript.Context.Provider != "deepseek-official" || transcript.Context.Model != "deepseek-flash" || transcript.Context.Window != 128000 {
		t.Fatalf("context = %+v", transcript.Context)
	}
	if len(transcript.Items()) != 0 {
		t.Fatalf("request context added a row: %+v", transcript.Items())
	}
	if transcript.Apply(Event{Seq: 2, Type: "request/context", Data: json.RawMessage(`{"provider": "deepseek-official", "model": "deepseek-flash", "contextWindow": 128000}`)}) {
		t.Fatal("duplicate context reported a change")
	}
}

func TestMalformedRequestContextIsBounded(t *testing.T) {
	var transcript Transcript
	transcript.Apply(Event{Seq: 1, Type: "request/context", Data: json.RawMessage(`{
		"provider": "bad\nprovider", "model": "model", "contextWindow": -1
	}`)})
	if transcript.Context.Provider != "" || transcript.Context.Model != "model" || transcript.Context.Window != 0 {
		t.Fatalf("context = %+v", transcript.Context)
	}
	transcript.Apply(Event{Seq: 2, Type: "request/context", Data: json.RawMessage(`{"contextWindow": 1e300}`)})
	if transcript.Context.Window != 0 {
		t.Fatalf("context = %+v", transcript.Context)
	}
}
