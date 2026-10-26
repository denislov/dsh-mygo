package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInteractionPayloadsMatchSharedFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "protocol", "interaction.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ApprovalRespond      json.RawMessage `json:"approvalRespond"`
		ApprovalClosed       json.RawMessage `json:"approvalClosed"`
		QuestionRespond      json.RawMessage `json:"questionRespond"`
		QuestionCancel       json.RawMessage `json:"questionCancel"`
		QuestionClosed       json.RawMessage `json:"questionClosed"`
		QuestionClosedGlobal json.RawMessage `json:"questionClosedGlobal"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		value any
		want  json.RawMessage
	}{
		{"approval respond", ApprovalRespondParams{RequestID: "a1", Decision: ApprovalDecisionAllow}, fixture.ApprovalRespond},
		{"question respond", QuestionRespondParams{RequestID: "q1", Answers: []QuestionAnswer{{ID: "q", Selected: []string{}, Custom: "hello"}}}, fixture.QuestionRespond},
		{"question cancel", QuestionCancelParams{RequestID: "q1"}, fixture.QuestionCancel},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertJSONEqual(t, test.value, test.want)
		})
	}
	var approvalClosed ApprovalClosedParams
	if err := json.Unmarshal(fixture.ApprovalClosed, &approvalClosed); err != nil || approvalClosed != (ApprovalClosedParams{RequestID: "a1", SessionID: "s1"}) {
		t.Fatalf("approval closed = %+v, %v", approvalClosed, err)
	}
	for _, test := range []struct {
		name string
		wire json.RawMessage
		want string
	}{
		{"question closed", fixture.QuestionClosed, "s1"},
		{"question closed global", fixture.QuestionClosedGlobal, ""},
	} {
		var closed QuestionClosedParams
		if err := json.Unmarshal(test.wire, &closed); err != nil || closed.RequestID != "q1" || closed.SessionID == nil || *closed.SessionID != test.want {
			t.Fatalf("%s = %+v, %v", test.name, closed, err)
		}
	}
	var missing QuestionClosedParams
	if err := json.Unmarshal([]byte(`{"requestId":"q1"}`), &missing); err != nil || missing.SessionID != nil {
		t.Fatal("missing sessionId must stay distinguishable from empty")
	}
	if MethodApprovalRespond != "approval/respond" || MethodQuestionRespond != "question/respond" || MethodQuestionCancel != "question/cancel" {
		t.Fatal("interaction method names changed")
	}
	if ApprovalDecisionAllow != "allow" || ApprovalDecisionDeny != "deny" {
		t.Fatal("approval decision names changed")
	}
}
