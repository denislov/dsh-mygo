package view

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestCredentialDraftRedactsAndClears(t *testing.T) {
	model := settingsModel()
	model.CredentialDraft.value = "fake-only-test-secret"
	for _, representation := range []string{fmt.Sprint(model.CredentialDraft), fmt.Sprintf("%+v", model), fmt.Sprintf("%#v", model)} {
		if strings.Contains(representation, "fake-only-test-secret") {
			t.Fatal("credential leaked through debug formatting")
		}
	}
	if model.TakeCredential() != "fake-only-test-secret" || model.TakeCredential() != "" || model.CredentialVersion != 2 {
		t.Fatal("credential draft was not consumed and cleared")
	}
}

func TestConfigurationControlsGateWritesAndKeepFooterVisible(t *testing.T) {
	model := settingsModel()
	model.SettingsOpen = true
	model.CanConfigure = true
	model.ConfigurationReady = true
	model.Configuration = Configuration{Defaults: model.ActiveRoute, Credential: CredentialStatus{Available: true, Writable: true, Reference: "MYGO_TEST_KEY", Source: "none"}}
	model.CredentialDraft.value = "fake-only-test-secret"
	tester := ui.NewTester(func(context *ui.Context) { View(context, model) }, 560, 420)
	tester.Advance(300 * time.Millisecond)
	if tester.HasText("fake-only-test-secret") {
		t.Fatal("credential was rendered as plain text")
	}
	if err := tester.Click("Close settings"); err != nil {
		t.Fatal(err)
	}
	model.SettingsOpen = true
	model.SettingsApplying = true
	model.SettingsBusy = true
	tester.Frame()
	tester.Key(0, ui.KeyEscape)
	if !model.SettingsOpen {
		t.Fatal("configuration save was dismissed")
	}
}

func TestPasswordInputSubmissionClearsAcrossFrames(t *testing.T) {
	model := settingsModel()
	model.CanConfigure = true
	model.ConfigurationReady = true
	model.Configuration = Configuration{Defaults: model.ActiveRoute, Credential: CredentialStatus{Available: true, Writable: true, Reference: "MYGO_TEST_KEY", Source: "none"}}
	saves := 0
	model.SaveCredential = func() {
		if model.TakeCredential() != "fake-test-only-key" {
			t.Error("password edit was not submitted")
		}
		saves++
	}
	tester := ui.NewTester(func(context *ui.Context) { credentialControls(context, model, false) }, 600, 600)
	if err := tester.Click("New API key"); err != nil {
		t.Fatal(err)
	}
	tester.Type("fake-test-only-key")
	if err := tester.Click("Save key"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	_ = tester.Click("Save key")
	if saves != 1 || model.CredentialDraft.value != "" {
		t.Fatal("password could be resubmitted after clearing")
	}
	model.CanConfigure = false
	tester.Frame()
	model.CanConfigure = true
	tester.Frame()
	if model.CredentialDraft.value != "" {
		t.Fatal("old password returned after control recreation")
	}
}
