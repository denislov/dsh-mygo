package view

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func settingsModel() *Model {
	model := NewModel()
	model.Phase = PhaseIdle
	model.CanModels = true
	model.SetActiveRoute(ModelRoute{Provider: "test", Model: "echo"})
	model.PrepareSettings(model.ActiveRoute)
	model.Catalog = ModelCatalog{Providers: []CatalogName{{ID: "test", Name: "Test"}}, Provider: "test", Models: []CatalogModel{{ID: "echo", Name: "Echo", Available: true, Efforts: []CatalogName{{ID: "high", Name: "High"}}}}}
	model.OpenSettings = func() { model.SettingsOpen = true }
	model.CloseSettings = func() { model.SettingsOpen = false }
	return model
}

func TestSettingsOpensAppliesAndDoesNotChangeDraft(t *testing.T) {
	model := settingsModel()
	model.Draft = "draft"
	applied := 0
	model.ApplySettings = func() { applied++; model.SettingsBusy = true; model.SettingsApplying = true }
	tester := tester(model)
	if err := tester.Click("Choose model"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(300 * time.Millisecond)
	if err := tester.Click("More settings…"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(300 * time.Millisecond)
	if !tester.HasText("Model settings") || !tester.HasText("Model ID") {
		t.Fatal("settings form missing")
	}
	if err := tester.Click("Apply model"); err != nil {
		t.Fatal(err)
	}
	_ = tester.Click("Apply model")
	tester.Key(0, ui.KeyEscape)
	if applied != 1 || !model.SettingsOpen || model.Draft != "draft" {
		t.Fatal("apply was duplicated, dismissed or lost draft")
	}
	model.SettingsBusy = false
	model.SettingsApplying = false
	model.SettingsError = "Could not apply model. Refresh to retry."
	tester.Frame()
	tester.Advance(300 * time.Millisecond)
	if !tester.HasText(model.SettingsError) {
		t.Fatal("settings error missing")
	}
	tester.Key(0, ui.KeyEscape)
	if model.SettingsOpen {
		t.Fatal("Escape did not close settings")
	}
	model.CanModels = false
	tester.Frame()
	if tester.HasText("Settings") {
		t.Fatal("old host showed settings")
	}
}

func TestSettingsEffortSelectionAndDefaultAreUnambiguous(t *testing.T) {
	model := settingsModel()
	model.SettingsOpen = true
	tester := tester(model)
	if err := tester.Click("Provider default"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Effort: high"); err != nil {
		t.Fatal(err)
	}
	if model.SelectedEffort() != "high" {
		t.Fatal("selected effort did not reach persistent state")
	}
	model.PrepareSettings(ModelRoute{Provider: "test", Model: "echo"})
	tester.Frame()
	if model.SelectedEffort() != "" {
		t.Fatal("default effort did not clear the override")
	}
}

func TestSettingsFitsMinimumWindowAndLoadingCanClose(t *testing.T) {
	model := settingsModel()
	model.SettingsOpen = true
	model.SettingsBusy = true
	model.CanHistory = true
	model.OpenSessions = func() {}
	model.Detail = "dsh-mygo-host 0.1.0 · DSH 0.2.1-rc.2"
	tester := ui.NewTester(func(context *ui.Context) { View(context, model) }, 560, 420)
	if err := tester.Click("Close"); err != nil {
		t.Fatal(err)
	}
	if model.SettingsOpen {
		t.Fatal("catalog load could not be dismissed")
	}
	model.SettingsBusy = false
	model.ModelUncertain = true
	model.Draft = "draft"
	model.Send = func(string) { t.Fatal("sent with an unconfirmed model") }
	tester.Frame()
	_ = tester.Click("Send")
	if !tester.HasText("Model selection unconfirmed · open Settings to refresh") {
		t.Fatal("uncertain-model guidance missing")
	}
}
