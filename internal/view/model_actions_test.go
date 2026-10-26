package view

import "testing"

func TestRegionActionsAreOptionalAndIndependent(t *testing.T) {
	model := NewModel()
	if model.Send != nil || model.OpenSessions != nil || model.OpenSettings != nil || model.RespondApproval != nil || model.Restart != nil {
		t.Fatal("new model should not imply unavailable actions")
	}

	sent := ""
	model.ComposerActions = ComposerActions{Send: func(text string) { sent = text }}
	model.Send("hello")
	if sent != "hello" {
		t.Fatalf("sent = %q", sent)
	}
	if model.OpenSessions != nil || model.OpenSettings != nil {
		t.Fatal("composer action leaked into another region")
	}

	opened := false
	model.SettingsActions = SettingsActions{OpenSettings: func() { opened = true }}
	model.OpenSettings()
	if !opened || model.Send == nil {
		t.Fatal("settings action was not independent from the composer")
	}

	model.SessionActions = SessionActions{SwitchSession: func(id string) { sent = id }}
	model.SwitchSession("s2")
	if sent != "s2" || model.OpenSettings == nil {
		t.Fatal("session action was not independent from settings")
	}

	// Gating stays in capability fields, not in the presence of a callback.
	model.CanCancel = true
	if model.Cancel != nil {
		t.Fatal("capability implied a callback")
	}
}
