package interaction

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDecodeApproval(t *testing.T) {
	for _, test := range []struct {
		name  string
		wire  string
		valid bool
	}{
		{"anchored", `{"requestId":"a1","sessionId":"s1","toolName":"write","callId":"call-1","arguments":"{}","external":false}`, true},
		{"external", `{"requestId":"a1","sessionId":"s1","toolName":"write","external":true}`, true},
		{"null", `null`, false},
		{"unsafe id", `{"requestId":"../a1","sessionId":"s1","toolName":"write","external":true}`, false},
		{"missing external", `{"requestId":"a1","sessionId":"s1","toolName":"write"}`, false},
		{"false anchor", `{"requestId":"a1","sessionId":"s1","toolName":"write","external":false}`, false},
		{"wrong type", `{"requestId":"a1","sessionId":"s1","toolName":3,"external":true}`, false},
		{"null optional field", `{"requestId":"a1","sessionId":"s1","toolName":"write","reason":null,"external":true}`, false},
		{"oversized tool", fmt.Sprintf(`{"requestId":"a1","sessionId":"s1","toolName":%q,"external":true}`, strings.Repeat("x", 257)), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeApproval(json.RawMessage(test.wire))
			if (err == nil) != test.valid {
				t.Fatalf("decode error = %v", err)
			}
		})
	}
}

func TestApprovalFIFOAndResponses(t *testing.T) {
	var approvals ApprovalQueue
	first := Approval{RequestID: "a1", SessionID: "s1", ToolName: "write", External: true}
	second := Approval{RequestID: "a2", SessionID: "s1", ToolName: "read", External: true}
	if err := approvals.Add(first); err != nil {
		t.Fatal(err)
	}
	if err := approvals.Add(second); err != nil {
		t.Fatal(err)
	}
	if err := approvals.Add(first); err != nil {
		t.Fatal(err)
	}
	if len(approvals.Items()) != 2 {
		t.Fatal("duplicate approval was queued twice")
	}
	if approvals.Begin("a2") || !approvals.Begin("a1") || approvals.Begin("a1") {
		t.Fatal("FIFO or duplicate-answer protection failed")
	}
	if !approvals.Items()[0].Responding {
		t.Fatal("answer was not marked pending")
	}
	approvals.Fail("a1", "Try again")
	if approvals.Items()[0].Responding || approvals.Items()[0].Error != "Try again" {
		t.Fatal("failed answer cannot be retried")
	}
	if approvals.Remove("a1", "foreign") {
		t.Fatal("foreign session removed an approval")
	}
	if !approvals.Remove("a1", "s1") || approvals.Items()[0].RequestID != "a2" {
		t.Fatal("next approval was lost")
	}
	if err := approvals.Add(first); err == nil {
		t.Fatal("closed request identity was reused")
	}
	approvals.Clear()
	if len(approvals.Items()) != 0 {
		t.Fatal("shutdown left approvals")
	}
}

func TestApprovalCannotChangeWhilePending(t *testing.T) {
	var approvals ApprovalQueue
	request := Approval{RequestID: "a1", SessionID: "s1", ToolName: "write", External: true}
	if err := approvals.Add(request); err != nil {
		t.Fatal(err)
	}
	request.Reason = "different operation"
	if err := approvals.Add(request); err == nil {
		t.Fatal("request identity was reused with different content")
	}
}

func TestApprovalQueueIsBounded(t *testing.T) {
	var approvals ApprovalQueue
	for index := 0; index < MaxPendingApprovals; index++ {
		if err := approvals.Add(Approval{RequestID: fmt.Sprintf("a%d", index), SessionID: "s1", ToolName: "write", External: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := approvals.Add(Approval{RequestID: "overflow", SessionID: "s1", ToolName: "write", External: true}); err == nil {
		t.Fatal("approval queue exceeded its bound")
	}
}
