package view

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"dsh-mygo/internal/interaction"
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
	for _, text := range []string{"You", "hello", "Assistant", "pong", "Tool · read_file", "Ready"} {
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

func TestStopButtonPreservesDraftAndDisablesWhilePending(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseRunning
	model.CanCancel = true
	model.Draft = "next prompt"
	stops := 0
	model.Cancel = func() { stops++; model.CancelPending = true }
	tt := tester(model)
	if err := tt.Click("Stop"); err != nil {
		t.Fatal(err)
	}
	if stops != 1 || model.Draft != "next prompt" {
		t.Fatalf("stops = %d, draft = %q", stops, model.Draft)
	}
	if !tt.HasText("Stopping…") {
		t.Fatal("pending cancellation is not shown")
	}
	_ = tt.Click("Stopping…")
	if stops != 1 {
		t.Fatal("duplicate stop was dispatched")
	}
	model.Phase = PhaseIdle
	model.CancelPending = false
	tt.Frame()
	if tt.HasText("Stop") || !tt.HasText("Send") {
		t.Fatal("idle composer did not restore Send")
	}
	model.Phase = PhaseRunning
	tt.Frame()
	if err := tt.Click("Stop"); err != nil {
		t.Fatal(err)
	}
	if stops != 2 {
		t.Fatal("returning Stop button lost its action")
	}
}

func TestStopIsHiddenWithoutCapability(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseRunning
	model.Cancel = func() { t.Fatal("unnegotiated cancellation dispatched") }
	tt := tester(model)
	if tt.HasText("Stop") {
		t.Fatal("Stop shown without capability")
	}
}

func TestApprovalModalShowsParametersAndSendsOnlyOneDecision(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseRunning
	model.CanApprove = true
	model.ApprovalOpen = true
	model.Approvals = []interaction.Approval{{RequestID: "a1", SessionID: "s1", ToolName: "write", Arguments: `{"file_path":"outside.txt"}`, Reason: "Write outside workspace"}}
	decisions := 0
	model.RespondApproval = func(requestID, decision string) {
		if requestID != "a1" || decision != "allow" {
			t.Fatalf("decision = %s %s", requestID, decision)
		}
		decisions++
		model.Approvals[0].Responding = true
	}
	tt := tester(model)
	for _, text := range []string{"Tool approval", "write", "Write outside workspace", `{"file_path":"outside.txt"}`, "Approval 1 of 1"} {
		if !tt.HasText(text) {
			t.Fatalf("approval is missing %q", text)
		}
	}
	if err := tt.Click("Allow once"); err != nil {
		t.Fatal(err)
	}
	_ = tt.Click("Allow once")
	if decisions != 1 || !tt.HasText("Sending decision…") {
		t.Fatal("decision was duplicated or not shown as pending")
	}
}

func TestApprovalEscapeRejectsAndExternalRequestWarns(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseRunning
	model.CanApprove = true
	model.ApprovalOpen = true
	model.Approvals = []interaction.Approval{{RequestID: "a1", SessionID: "s1", ToolName: "write", External: true}}
	denied := false
	model.RespondApproval = func(requestID, decision string) {
		denied = requestID == "a1" && decision == "deny"
		model.Approvals[0].Responding = true
	}
	tt := tester(model)
	if !tt.HasText("Unlinked approval: verify this request before allowing.") {
		t.Fatal("external approval did not warn")
	}
	tt.Key(0, ui.KeyEscape)
	if !denied {
		t.Fatal("Escape did not reject the approval")
	}
	if !model.ApprovalOpen {
		t.Fatal("approval disappeared before host confirmation")
	}
	model.Approvals = nil
	model.ApprovalOpen = false
	tt.Frame()
	model.Approvals = []interaction.Approval{{RequestID: "a2", SessionID: "s1", ToolName: "read", External: true}}
	model.ApprovalOpen = true
	tt.Frame()
	if !tt.HasText("read") || !tt.HasText("Deny") {
		t.Fatal("returning approval lost its controls")
	}
}
