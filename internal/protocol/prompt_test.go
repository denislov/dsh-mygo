package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPromptPayloadsMatchSharedFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "protocol", "prompt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Text          json.RawMessage `json:"text"`
		TextWithImage json.RawMessage `json:"textWithImage"`
		Image         json.RawMessage `json:"image"`
		SDKText       json.RawMessage `json:"sdkText"`
		Accepted      json.RawMessage `json:"accepted"`
		Cancel        json.RawMessage `json:"cancel"`
		CancelActive  json.RawMessage `json:"cancelActive"`
		CancelIdle    json.RawMessage `json:"cancelIdle"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	text := NewTextBlock("hello")
	for _, test := range []struct {
		name  string
		value any
		want  json.RawMessage
	}{
		{"custom text", SessionPromptParams{SessionID: "s1", Content: []ContentBlock{text}}, fixture.Text},
		{"custom text and image", SessionPromptParams{SessionID: "s1", Content: []ContentBlock{NewTextBlock("look"), NewImageBlock("image/png", "cG5n", "shot.png")}}, fixture.TextWithImage},
		{"custom image only", SessionPromptParams{SessionID: "s1", Content: []ContentBlock{NewImageBlock("image/png", "cG5n", "")}}, fixture.Image},
		{"SDK text", SDKPromptParams{SessionID: "s1", ContentBlocks: []ContentBlock{text}}, fixture.SDKText},
		{"cancel", SessionCancelParams{SessionID: "s1"}, fixture.Cancel},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertJSONEqual(t, test.value, test.want)
		})
	}
	var accepted PromptResult
	if err := json.Unmarshal(fixture.Accepted, &accepted); err != nil || accepted.MessageID != "message-1" {
		t.Fatalf("accepted = %+v, %v", accepted, err)
	}
	for _, test := range []struct {
		name      string
		wire      json.RawMessage
		cancelled bool
	}{
		{"active", fixture.CancelActive, true},
		{"idle", fixture.CancelIdle, false},
	} {
		var result SessionCancelResult
		if err := json.Unmarshal(test.wire, &result); err != nil || result.Cancelled == nil || *result.Cancelled != test.cancelled || result.Status != "idle" {
			t.Fatalf("%s cancel result = %+v, %v", test.name, result, err)
		}
	}
	var missing SessionCancelResult
	if err := json.Unmarshal([]byte(`{"status":"idle"}`), &missing); err != nil || missing.Cancelled != nil {
		t.Fatal("missing cancelled must stay distinguishable from false")
	}
	if MethodSessionPrompt != "session/prompt" || MethodSessionCancel != "session/cancel" {
		t.Fatal("prompt/cancel method names changed")
	}
}

func TestContentBlocksKeepIndependentWireShapes(t *testing.T) {
	assertJSONEqual(t, NewTextBlock(""), json.RawMessage(`{"type":"text","text":""}`))
	assertJSONEqual(t, NewImageBlock("image/jpeg", "cG5n", ""), json.RawMessage(`{"type":"image","mimeType":"image/jpeg","data":"cG5n"}`))
	assertJSONEqual(t, NewImageBlock("image/jpeg", "cG5n", "shot.jpg"), json.RawMessage(`{"type":"image","mimeType":"image/jpeg","data":"cG5n","name":"shot.jpg"}`))
}
