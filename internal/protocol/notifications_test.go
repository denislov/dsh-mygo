package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"dsh-mygo/internal/projection"
)

func TestNotificationPayloadsMatchSharedFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "protocol", "notifications.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SessionEvent         json.RawMessage `json:"sessionEvent"`
		SessionStatus        json.RawMessage `json:"sessionStatus"`
		AssistantStream      json.RawMessage `json:"assistantStream"`
		SessionCancelTimeout json.RawMessage `json:"sessionCancelTimeout"`
		ApprovalStaleRequest json.RawMessage `json:"approvalStaleRequest"`
		QuestionStaleRequest json.RawMessage `json:"questionStaleRequest"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	var event SessionEventNotification
	if err := json.Unmarshal(fixture.SessionEvent, &event); err != nil || event.SessionID != "s1" || event.Seq == nil || *event.Seq != 7 || event.Event == nil {
		t.Fatalf("event envelope = %+v, %v", event, err)
	}
	decoded, sequence, err := projection.DecodeEvent(event.Event)
	if err != nil || decoded.Type != "assistant/message" || decoded.Seq != 7 || sequence == nil || *sequence != 7 || string(decoded.Data) == "" {
		t.Fatalf("event = %+v, %v, %v", decoded, sequence, err)
	}
	var status SessionStatusNotification
	if err := json.Unmarshal(fixture.SessionStatus, &status); err != nil || status.SessionID != "s1" || status.Status != "idle" || status.StatusRevision == nil || *status.StatusRevision != 4 {
		t.Fatalf("status = %+v, %v", status, err)
	}
	var stream AssistantStreamNotification
	if err := json.Unmarshal(fixture.AssistantStream, &stream); err != nil || stream.SessionID != "s1" || stream.Frame == nil {
		t.Fatalf("stream = %+v, %v", stream, err)
	}
	for _, test := range []struct {
		name string
		wire json.RawMessage
		want string
	}{
		{"cancel timeout", fixture.SessionCancelTimeout, ErrorKindSessionCancelTimeout},
		{"approval stale", fixture.ApprovalStaleRequest, ErrorKindApprovalStaleRequest},
		{"question stale", fixture.QuestionStaleRequest, ErrorKindQuestionStaleRequest},
	} {
		if got := ErrorKind(test.wire); got != test.want {
			t.Fatalf("%s kind = %q", test.name, got)
		}
	}
	var missingEvent SessionEventNotification
	if err := json.Unmarshal([]byte(`{"sessionId":"s1","event":{"type":"turn/start","seq":0,"data":{}}}`), &missingEvent); err != nil || missingEvent.Seq != nil {
		t.Fatal("missing outer seq must stay distinguishable")
	}
	var missingStatus SessionStatusNotification
	if err := json.Unmarshal([]byte(`{"sessionId":"s1","status":"running"}`), &missingStatus); err != nil || missingStatus.StatusRevision != nil {
		t.Fatal("missing statusRevision must stay distinguishable")
	}
	if ErrorKind(nil) != "" || ErrorKind(json.RawMessage(`not json`)) != "" || ErrorKind(json.RawMessage(`{"kind":1}`)) != "" {
		t.Fatal("malformed error payloads must not report a kind")
	}
	for name, pair := range map[string][2]string{
		"session event": {NotificationSessionEvent, "session/event"}, "session status": {NotificationSessionStatus, "session/status"},
		"assistant stream": {NotificationSessionAssistantStream, "session/assistant-stream"}, "approval request": {NotificationApprovalRequest, "approval/request"},
		"approval closed": {NotificationApprovalClosed, "approval/closed"}, "question request": {NotificationQuestionRequest, "question/request"},
		"question closed": {NotificationQuestionClosed, "question/closed"}, "subagent started": {NotificationSubagentStarted, "subagent/started"},
		"subagent finished": {NotificationSubagentFinished, "subagent/finished"}, "host fatal": {NotificationHostFatal, "host/fatal"},
		"sdk event": {NotificationSDKSessionEvent, "session.event"}, "sdk status": {NotificationSDKSessionStatus, "session.status"},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("%s = %q", name, pair[0])
		}
	}
}
