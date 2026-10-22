package view

import (
	"encoding/json"
	"testing"

	"dsh-mygo/internal/interaction"
	"github.com/egoist/mygo/ui"
)

func questionModel(t *testing.T) *Model {
	t.Helper()
	request, err := interaction.DecodeQuestion(json.RawMessage(`{"requestId":"q1","sessionId":"s1","questions":[{"id":"q","question":"Choose mode","detail":"Full detail","options":[{"label":"Safe","description":"Recommended"},{"label":"Fast"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	model.Phase = PhaseRunning
	model.CanQuestion = true
	model.QuestionOpen = true
	model.Questions = []*interaction.QuestionRequest{request}
	return model
}

func TestQuestionModalChoiceSubmissionAndEscape(t *testing.T) {
	model := questionModel(t)
	responses := 0
	model.RespondQuestion = func(requestID string, cancel bool) {
		if requestID != "q1" || cancel {
			t.Fatal("unexpected answer")
		}
		answers, err := model.Questions[0].Answers()
		if err != nil || answers[0].Selected[0] != "Safe" {
			t.Fatal("choice did not reach persistent draft")
		}
		model.Questions[0].Responding = true
		responses++
	}
	tester := tester(model)
	if !tester.HasText("Full detail") || !tester.HasText("Recommended") {
		t.Fatal("question detail missing")
	}
	if err := tester.Click("Safe"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Submit answers"); err != nil {
		t.Fatal(err)
	}
	_ = tester.Click("Submit answers")
	if responses != 1 {
		t.Fatal("answer duplicated")
	}
	model.Questions[0].Responding = false
	cancelled := false
	model.RespondQuestion = func(_ string, cancel bool) { cancelled = cancel; model.Questions[0].Responding = true }
	tester.Frame()
	tester.Key(0, ui.KeyEscape)
	if !cancelled || !model.QuestionOpen {
		t.Fatal("Escape did not cancel with host confirmation pending")
	}
}

func TestQuestionCustomTextReplacesSingleChoiceAndSurvivesFrames(t *testing.T) {
	model := questionModel(t)
	model.RespondQuestion = func(string, bool) {}
	tester := tester(model)
	if err := tester.Click("Safe"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Fast"); err != nil {
		t.Fatal(err)
	}
	if model.Questions[0].Drafts[0].Selected[0] || !model.Questions[0].Drafts[0].Selected[1] {
		t.Fatal("single choice selected twice")
	}
	if err := tester.Click("Answer: q"); err != nil {
		t.Fatal(err)
	}
	tester.Type("Custom answer")
	tester.Frame()
	tester.Frame()
	if model.Questions[0].Drafts[0].Custom != "Custom answer" || model.Questions[0].Drafts[0].Selected[1] {
		t.Fatal("custom text was lost or left a single choice selected")
	}
	model.Questions = nil
	model.QuestionOpen = false
	tester.Frame()
	request, err := interaction.DecodeQuestion(json.RawMessage(`{"requestId":"q2","sessionId":"s1","questions":[{"id":"q","question":"Second question"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	model.Questions = []*interaction.QuestionRequest{request}
	model.QuestionOpen = true
	tester.Frame()
	if !tester.HasText("Second question") || request.Drafts[0].Custom != "" {
		t.Fatal("second form inherited old state")
	}
}

func TestApprovalAndQuestionDoNotOpenTwoModals(t *testing.T) {
	model := questionModel(t)
	model.RespondQuestion = func(string, bool) {}
	model.CanApprove = true
	model.ApprovalOpen = true
	model.Approvals = []interaction.Approval{{RequestID: "a1", SessionID: "s1", ToolName: "write", External: true}}
	tester := tester(model)
	if !tester.HasText("Tool approval") || tester.HasText("User questions") {
		t.Fatal("two interaction modals overlap")
	}
	model.Questions[0].Drafts[0].Custom = "Kept"
	model.Approvals = nil
	model.ApprovalOpen = false
	tester.Frame()
	if !tester.HasText("User questions") || model.Questions[0].Drafts[0].Custom != "Kept" {
		t.Fatal("approval discarded the question draft")
	}
}
