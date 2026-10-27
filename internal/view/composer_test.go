package view

import "testing"

func TestComposerKeepsInputDuringSessionLoadButNotSend(t *testing.T) {
	model := sessionModel()
	model.SessionBusy, model.SessionSwitching = true, true
	if composerInputStopped(model) {
		t.Fatal("session loading stopped the composer input")
	}
	if model.CanSendPrompt() {
		t.Fatal("session loading accepted a prompt")
	}
}
