package app

import (
	"encoding/json"
	"errors"
	"testing"

	"dsh-mygo/internal/view"
)

const questionRequest = `{"requestId":"q1","sessionId":"s1","questions":[{"id":"q","question":"Choose mode","options":[{"label":"Safe"}]}]}`

func TestQuestionAnswerValidationRetryAndClose(t *testing.T) {
	client := &fakeClient{err: errors.New("private error")}
	application, updates := approvalApp(client)
	application.model.CanQuestion = true
	application.handleNotification("question/request", json.RawMessage(questionRequest))
	application.RespondQuestion("q1", false)
	if len(client.called()) != 0 || application.model.Questions[0].Error == "" {
		t.Fatal("empty answer was sent")
	}
	application.model.Questions[0].Drafts[0].Selected[0] = true
	application.RespondQuestion("q1", false)
	application.RespondQuestion("q1", false)
	takeApprovalUpdate(t, updates)
	if len(client.called()) != 1 || client.called()[0] != "question/respond" || application.model.Questions[0].Responding || !application.model.Questions[0].Drafts[0].Selected[0] {
		t.Fatal("retry lost draft or submission duplicated")
	}
	application.RespondQuestion("q1", true)
	application.handleNotification("question/closed", json.RawMessage(`{"requestId":"q1","sessionId":"s1"}`))
	application.handleNotification("question/request", json.RawMessage(`{"requestId":"q2","sessionId":"s1","questions":[{"id":"q","question":"Next"}]}`))
	takeApprovalUpdate(t, updates)
	if len(application.model.Questions) != 1 || application.model.Questions[0].RequestID != "q2" || application.model.Questions[0].Error != "" || len(application.model.Items) != 0 {
		t.Fatal("late callback changed next form or invented history")
	}
}

func TestQuestionGlobalScopeAndCapability(t *testing.T) {
	application, _ := approvalApp(&fakeClient{})
	application.model.CanQuestion = true
	application.handleNotification("question/request", json.RawMessage(`{"requestId":"foreign","sessionId":"other","questions":[{"id":"q","question":"Foreign"}]}`))
	if len(application.model.Questions) != 0 {
		t.Fatal("foreign question displayed")
	}
	application.handleNotification("question/request", json.RawMessage(`{"requestId":"global","sessionId":"","questions":[{"id":"q","question":"Sign in?"}]}`))
	application.applyStatus("idle")
	if len(application.model.Questions) != 1 {
		t.Fatal("session idle withdrew a global question")
	}
	application.model.CanQuestion = false
	application.handleNotification("question/request", json.RawMessage(questionRequest))
	if application.model.Phase != view.PhaseFailed || len(application.model.Questions) != 0 {
		t.Fatal("unnegotiated request did not fail closed")
	}
}
