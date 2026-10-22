package interaction

import (
	"encoding/json"
	"fmt"
	"testing"
)

const questionWire = `{"requestId":"q1","sessionId":"s1","questions":[{"id":"mode","question":"Choose mode","options":[{"label":"Safe"},{"label":"Fast"}]},{"id":"features","question":"Choose features","multiSelect":true,"options":[{"label":"Logs"},{"label":"Tests"}]},{"id":"note","question":"Add a note"}]}`

func TestQuestionFormValidatesBatchAndKeepsDraft(t *testing.T) {
	request, err := DecodeQuestion(json.RawMessage(questionWire))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := request.Answers(); err == nil {
		t.Fatal("empty form was accepted")
	}
	request.Drafts[0].Selected[0] = true
	request.Drafts[1].Selected[0] = true
	request.Drafts[1].Selected[1] = true
	request.Drafts[1].Custom = "Extra"
	request.Drafts[2].Custom = "Hello"
	answers, err := request.Answers()
	if err != nil || len(answers) != 3 || len(answers[1].Selected) != 2 || answers[2].Custom != "Hello" {
		t.Fatalf("invalid batch: %v", err)
	}
	request.Drafts[0].Custom = "Other"
	if _, err := request.Answers(); err == nil {
		t.Fatal("single choice accepted option and custom together")
	}
	request.Drafts[0].Custom = ""
	var queue QuestionQueue
	if err := queue.Add(request); err != nil {
		t.Fatal(err)
	}
	if !queue.Begin("q1") || queue.Begin("q1") {
		t.Fatal("duplicate submission allowed")
	}
	queue.Fail("q1", "Retry")
	if queue.Items()[0].Drafts[2].Custom != "Hello" || queue.Items()[0].Responding {
		t.Fatal("retry lost form draft")
	}
	queue.Remove("q1", "s1")
	if err := queue.Add(request); err == nil {
		t.Fatal("closed identity reused")
	}
}

func TestQuestionPayloadValidationAndGlobalScope(t *testing.T) {
	for _, wire := range []string{`null`, `{"requestId":"q1","sessionId":"s1","questions":[]}`, `{"requestId":"q1","sessionId":"s1","questions":[{"id":"x","question":"Q","options":null}]}`, `{"requestId":"q1","sessionId":"s1","questions":[{"id":"x","question":"Q","multiSelect":null}]}`, `{"requestId":"q1","sessionId":"s1","questions":[{"id":"x","question":"Q"},{"id":"x","question":"Q"}]}`} {
		if _, err := DecodeQuestion(json.RawMessage(wire)); err == nil {
			t.Fatal("invalid question accepted")
		}
	}
	request, err := DecodeQuestion(json.RawMessage(`{"requestId":"global","sessionId":"","questions":[{"id":"q","question":"Sign in?"}]}`))
	if err != nil || request.SessionID != "" {
		t.Fatal("agentless request rejected")
	}
}

func TestQuestionQueueFIFOAndLimits(t *testing.T) {
	var queue QuestionQueue
	for index := 0; index < 64; index++ {
		request, err := DecodeQuestion(json.RawMessage(fmt.Sprintf(`{"requestId":"q%d","sessionId":"s1","questions":[{"id":"q","question":"Question"}]}`, index)))
		if err != nil {
			t.Fatal(err)
		}
		if err := queue.Add(request); err != nil {
			t.Fatal(err)
		}
	}
	if queue.Begin("q1") || !queue.Begin("q0") {
		t.Fatal("FIFO was bypassed")
	}
	if err := queue.Add(&QuestionRequest{RequestID: "overflow"}); err == nil {
		t.Fatal("queue bound exceeded")
	}
	queue.Remove("q0", "foreign")
	if len(queue.Items()) != 64 {
		t.Fatal("foreign close removed question")
	}
	queue.RemoveSession("s1")
	if len(queue.Items()) != 0 {
		t.Fatal("session cleanup left questions")
	}
}
