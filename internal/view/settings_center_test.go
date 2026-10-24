package view

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestSettingsCenterGeneralWorksWithoutHostCapabilities(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	tt := tester(model)
	if err := tt.Click("Preferences"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !model.SettingsOpen || model.SettingsSection != SettingsSectionGeneral {
		t.Fatalf("settings center did not open General: open=%v section=%v", model.SettingsOpen, model.SettingsSection)
	}
	for _, text := range []string{"Interface preferences", "Appearance", "Theme", "Language", "Shortcuts", "About"} {
		if !tt.HasText(text) {
			t.Fatalf("general section missing %q in %v", text, tt.Texts())
		}
	}
	for _, text := range []string{"Conversation model", "Permissions", "Connection", "Apply model"} {
		if tt.HasText(text) {
			t.Fatalf("host-only %q appeared without capabilities in %v", text, tt.Texts())
		}
	}
}

func TestSettingsCenterSwitchesSectionsAndRefreshes(t *testing.T) {
	model := settingsModel()
	model.Permission.Available = true
	model.CanConfigure = true
	model.SettingsOpen = true
	model.AppVersion = "v9.9.9"
	model.Detail = "host-build-fixture"
	refreshes := 0
	model.RefreshSettings = func() { refreshes++ }
	tt := tester(model)
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("Model settings") || !tt.HasText("Model ID") {
		t.Fatalf("conversation section missing in %v", tt.Texts())
	}
	if err := tt.Click("General"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if model.SettingsSection != SettingsSectionGeneral || !tt.HasText("Appearance") {
		t.Fatal("nav did not switch to General")
	}
	if err := tt.Click("Conversation model"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if model.SettingsSection != SettingsSectionConversation || refreshes == 0 {
		t.Fatalf("conversation switch did not refresh or bind: section=%v refreshes=%d", model.SettingsSection, refreshes)
	}
	if err := tt.Click("About"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, text := range []string{"Version", "v9.9.9", "Host"} {
		if !tt.HasText(text) {
			t.Fatalf("about section missing %q in %v", text, tt.Texts())
		}
	}
}

func TestApplicationMenuOpensSettingsAndAbout(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	tt := tester(model)
	if err := tt.Click("Menu"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !model.SettingsOpen || model.SettingsSection != SettingsSectionGeneral {
		t.Fatal("menu Settings did not open General")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Advance(300 * time.Millisecond)
	if err := tt.Click("Menu"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("About dsh-mygo"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !model.SettingsOpen || model.SettingsSection != SettingsSectionAbout {
		t.Fatal("menu About did not open the About section")
	}
}

func TestSettingsCenterShowsOnlyAvailableSections(t *testing.T) {
	model := settingsModel()
	model.Permission.Available = true
	model.CanConfigure = false
	model.SettingsOpen = true
	tt := tester(model)
	tt.Frame()
	if !tt.HasText("Conversation model") || !tt.HasText("Permissions") || tt.HasText("Connection") {
		t.Fatalf("section gating wrong in %v", tt.Texts())
	}
}
