package view

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func runtimeSettingsModel() *Model {
	model := NewModel()
	model.Phase = PhaseIdle
	model.SettingsOpen = true
	model.SettingsSection = SettingsSectionRuntime
	model.Runtime.CanManage = true
	model.Runtime.Current = RuntimeChoice{ID: "managed-a", Kind: "managed", Label: "Managed runtime", Node: "24.14.0", DSH: "0.2.1-rc.2", HostVersion: "0.1.1", Path: "C:/runtimes/a"}
	model.Runtime.Installed = []RuntimeChoice{
		model.Runtime.Current,
		{ID: "external-b", Kind: "external", Label: "Existing DSH installation", Node: "24.14.0", DSH: "0.2.1-rc.2", HostVersion: "0.1.1", Detail: "C:/bin/dsh.cmd"},
		{ID: "managed-c", Kind: "managed", Label: "Managed runtime", Node: "24.15.0", DSH: "0.2.1-rc.2", HostVersion: "0.1.1", Path: "C:/runtimes/c"},
	}
	model.Runtime.Detect = func() {}
	model.Runtime.InstallFile = func() {}
	model.Runtime.CheckUpdates = func() {}
	model.Runtime.Cancel = func() {}
	return model
}

func TestRuntimeSettingsShowsCurrentAndInstalled(t *testing.T) {
	model := runtimeSettingsModel()
	activated, removed := "", ""
	model.Runtime.Activate = func(id string) { activated = id }
	model.Runtime.Remove = func(id string) { removed = id }
	tt := tester(model)

	for _, text := range []string{"Runtime", "Current runtime", "Managed runtime", "C:/runtimes/a", "Installed runtimes", "Add runtime", "Check for updates", "Install from file…"} {
		if !tt.HasText(text) {
			t.Fatalf("runtime page missing %q in %v", text, tt.Texts())
		}
	}
	if !tt.HasText("Current") {
		t.Fatal("current runtime is not marked")
	}
	if err := tt.Click("Use"); err != nil {
		t.Fatal(err)
	}
	if activated != "external-b" {
		t.Fatalf("activated = %q", activated)
	}
	if err := tt.Click("Remove"); err != nil {
		t.Fatal(err)
	}
	if model.Runtime.RemoveConfirm != "external-b" {
		t.Fatalf("remove confirmation = %q", model.Runtime.RemoveConfirm)
	}
	tt.Frame()
	if err := tt.Click("Remove"); err != nil {
		t.Fatal(err)
	}
	if removed != "external-b" {
		t.Fatalf("removed = %q", removed)
	}
}

func TestRuntimeSettingsBlocksCloseWhileBusy(t *testing.T) {
	model := runtimeSettingsModel()
	cancelled := 0
	model.Runtime.Busy = true
	model.Runtime.Status = "Downloading runtime"
	model.Runtime.Cancel = func() { cancelled++ }
	tt := tester(model)
	if !tt.HasText("Downloading runtime") {
		t.Fatalf("busy status missing in %v", tt.Texts())
	}
	_ = tt.Click("Close settings")
	if !model.SettingsOpen {
		t.Fatal("settings can be closed during a runtime operation")
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if cancelled != 1 {
		t.Fatal("runtime operation was not cancelled")
	}
}

func TestRuntimeSettingsShowsSwitchingAndDisablesClose(t *testing.T) {
	model := runtimeSettingsModel()
	model.Runtime.Switching = true
	tt := tester(model)
	if !tt.HasText("Switching runtime…") {
		t.Fatalf("switch status missing in %v", tt.Texts())
	}
	_ = tt.Click("Close settings")
	if !model.SettingsOpen {
		t.Fatal("settings can be closed during a runtime switch")
	}
}

func TestSettingsSectionsPlaceRuntimeAfterGeneral(t *testing.T) {
	model := NewModel()
	model.Runtime.CanManage = true
	sections := model.settingsSections()
	if len(sections) < 2 || sections[0] != SettingsSectionGeneral || sections[1] != SettingsSectionRuntime {
		t.Fatalf("sections = %v", sections)
	}
	if !model.settingsSectionAvailable(SettingsSectionRuntime) {
		t.Fatal("runtime section is not always available")
	}
}

func TestRecoveryOffersRuntimeSettings(t *testing.T) {
	model := recoveryModel()
	managed := 0
	model.Runtime.CanManage = true
	model.Runtime.Manage = func() { managed++ }
	tt := tester(model)
	if err := tt.Click("Change runtime…"); err != nil {
		t.Fatal(err)
	}
	if managed != 1 {
		t.Fatal("change runtime callback was not called")
	}
}

func TestRuntimeSettingsFitsMinimumWindow(t *testing.T) {
	model := runtimeSettingsModel()
	model.SettingsSection = SettingsSectionRuntime
	tt := ui.NewTester(func(c *ui.Context) { View(c, model) }, 560, 420)
	if !tt.HasText("Current runtime") {
		t.Fatalf("runtime page missing at minimum size: %v", tt.Texts())
	}
	if err := tt.Click("Close settings"); err != nil {
		t.Fatal(err)
	}
}
