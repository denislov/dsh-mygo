package view

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"dsh-mygo/internal/projection"
)

func tester(model *Model) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) { View(c, model) }, 900, 700)
}

func TestViewShowsTranscriptAndStatus(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	model.ModelLabel = "deepseek-official · deepseek-flash"
	model.Items = []projection.Item{
		{ID: "1", Kind: projection.KindUser, Title: "You", Text: "hello"},
		{ID: "2", Kind: projection.KindAssistant, Title: "Assistant", Text: "pong"},
		{ID: "3", Kind: projection.KindTool, Title: "Tool · read_file", Text: `{"path": "a.txt"}`, Detail: "file contents", State: "done"},
	}
	tt := tester(model)
	for _, text := range []string{"You", "hello", "Assistant", "pong", "Tool · read_file", "file contents", "Ready"} {
		if !tt.HasText(text) {
			t.Errorf("missing %q in %v", text, tt.Texts())
		}
	}
}

func TestComposerSendsOnEnter(t *testing.T) {
	var sent []string
	model := NewModel()
	model.Phase = PhaseIdle
	model.Send = func(text string) { sent = append(sent, text) }
	tt := tester(model)

	tt.Type("hello")
	tt.Key(0, ui.KeyEnter)

	if len(sent) != 1 || sent[0] != "hello" {
		t.Fatalf("sent = %v", sent)
	}
	if model.Draft != "" {
		t.Fatalf("draft = %q", model.Draft)
	}
}

func TestComposerShiftEnterAddsALine(t *testing.T) {
	var sent []string
	model := NewModel()
	model.Phase = PhaseIdle
	model.Send = func(text string) { sent = append(sent, text) }
	tt := tester(model)

	tt.Type("first")
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Type("second")

	if len(sent) != 0 {
		t.Fatalf("sent = %v", sent)
	}
	if model.Draft != "first\nsecond" {
		t.Fatalf("draft = %q", model.Draft)
	}
}

func TestComposerDoesNotSendWhileRunning(t *testing.T) {
	var sent []string
	model := NewModel()
	model.Phase = PhaseRunning
	model.Send = func(text string) { sent = append(sent, text) }
	tt := tester(model)

	tt.Type("draft")
	tt.Key(0, ui.KeyEnter)

	if len(sent) != 0 {
		t.Fatalf("sent = %v", sent)
	}
	if model.Draft != "draft\n" {
		t.Fatalf("draft = %q", model.Draft)
	}
	if !tt.HasText("Working…") {
		t.Fatalf("working status missing: %v", tt.Texts())
	}
}

func TestDraftCanBeRestoredAfterAFailedSend(t *testing.T) {
	var sent []string
	model := NewModel()
	model.Phase = PhaseIdle
	model.Send = func(text string) {
		sent = append(sent, text)
		model.Draft = ""
		model.Phase = PhaseRunning
	}
	tt := tester(model)

	tt.Type("hello")
	tt.Key(0, ui.KeyEnter)
	if len(sent) != 1 || model.Draft != "" {
		t.Fatalf("sent = %v, draft = %q", sent, model.Draft)
	}

	model.Draft = "hello"
	model.Phase = PhaseIdle
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[1] != "hello" {
		t.Fatalf("sent after restoring the draft = %v", sent)
	}
}

func TestSendButtonSendsTheDraft(t *testing.T) {
	var sent []string
	model := NewModel()
	model.Phase = PhaseIdle
	model.Send = func(text string) { sent = append(sent, text) }
	tt := tester(model)

	tt.Type("hello")
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != "hello" {
		t.Fatalf("sent = %v", sent)
	}
}

func TestErrorBannerCanBeDismissed(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseFailed
	model.Error = "The host exited"
	model.ErrorDetail = "exit status 1"
	model.Dismiss = func() {
		model.Error = ""
		model.ErrorDetail = ""
	}
	tt := tester(model)

	if !tt.HasText("The host exited") || !tt.HasText("exit status 1") {
		t.Fatalf("error banner missing: %v", tt.Texts())
	}
	if err := tt.Click("Dismiss"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("The host exited") {
		t.Fatalf("error banner stayed: %v", tt.Texts())
	}
	if !tt.HasText("Host error") {
		t.Fatalf("phase label missing: %v", tt.Texts())
	}
}
