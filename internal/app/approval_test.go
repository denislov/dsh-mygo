package app

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"dsh-mygo/internal/view"
)

const approvalRequest = `{"requestId":"a1","sessionId":"s1","toolName":"write","callId":"call-1","arguments":"{\"file_path\":\"outside.txt\"}","reason":"Write outside the workspace","external":false}`

func approvalApp(client *fakeClient) (*App, chan func()) {
	application := newTestApp(client)
	application.options.SDKProtocol = false
	application.model.CanApprove = true
	application.model.Phase = view.PhaseRunning
	updates := make(chan func(), 16)
	application.options.Post = func(fn func()) { updates <- fn }
	return application, updates
}

func takeApprovalUpdate(t *testing.T, updates <-chan func()) {
	t.Helper()
	select {
	case update := <-updates:
		update()
	case <-time.After(2 * time.Second):
		t.Fatal("approval response did not arrive")
	}
}

func TestApprovalRoundTripHasNoSyntheticHistory(t *testing.T) {
	client := &fakeClient{result: json.RawMessage(`{}`)}
	application, updates := approvalApp(client)
	application.handleNotification("approval/request", json.RawMessage(approvalRequest))
	application.RespondApproval("a1", "allow")
	application.RespondApproval("a1", "allow")
	if !application.model.Approvals[0].Responding {
		t.Fatal("duplicate-answer protection was not enabled")
	}
	takeApprovalUpdate(t, updates)
	if len(client.called()) != 1 || client.called()[0] != "approval/respond" {
		t.Fatalf("calls = %v", client.called())
	}
	if len(application.model.Approvals) != 0 || application.model.ApprovalOpen || len(application.model.Items) != 0 {
		t.Fatal("response left an approval or invented history")
	}
	params := client.params[0].(map[string]any)
	if params["requestId"] != "a1" || params["decision"] != "allow" {
		t.Fatalf("response parameters = %+v", params)
	}
}

func TestApprovalFailureRetriesButLateResponseCannotRestoreClosedRequest(t *testing.T) {
	client := &fakeClient{err: errors.New("private runtime error")}
	application, updates := approvalApp(client)
	application.handleNotification("approval/request", json.RawMessage(approvalRequest))
	application.RespondApproval("a1", "deny")
	takeApprovalUpdate(t, updates)
	if application.model.Approvals[0].Responding || application.model.Approvals[0].Error == "" {
		t.Fatal("failed decision cannot be retried")
	}
	application.RespondApproval("a1", "deny")
	application.handleNotification("approval/closed", json.RawMessage(`{"requestId":"a1","sessionId":"s1"}`))
	application.handleNotification("approval/request", json.RawMessage(`{"requestId":"a2","sessionId":"s1","toolName":"read","external":true}`))
	takeApprovalUpdate(t, updates)
	if len(application.model.Approvals) != 1 || application.model.Approvals[0].RequestID != "a2" || application.model.Approvals[0].Error != "" {
		t.Fatal("old answer changed the next approval")
	}
}

func TestApprovalValidationCapabilityAndForeignSession(t *testing.T) {
	application, _ := approvalApp(&fakeClient{})
	application.handleNotification("approval/request", json.RawMessage(`{"requestId":"foreign","sessionId":"other","toolName":"write","external":true}`))
	if len(application.model.Approvals) != 0 {
		t.Fatal("foreign approval was displayed")
	}
	application.model.CanApprove = false
	application.handleNotification("approval/request", json.RawMessage(approvalRequest))
	if application.model.Phase != view.PhaseFailed {
		t.Fatal("unnegotiated approval did not fail closed")
	}
}

func TestApprovalCannotBeAllowedWhileCancellingAndClosesOnFailure(t *testing.T) {
	client := &fakeClient{}
	application, _ := approvalApp(client)
	application.handleNotification("approval/request", json.RawMessage(approvalRequest))
	application.model.CancelPending = true
	application.RespondApproval("a1", "allow")
	if len(client.called()) != 0 {
		t.Fatal("approval was allowed while cancellation was pending")
	}
	application.handleNotification("host/fatal", json.RawMessage(`{}`))
	if len(application.model.Approvals) != 0 || application.model.ApprovalOpen {
		t.Fatal("host failure left an approval visible")
	}
}
