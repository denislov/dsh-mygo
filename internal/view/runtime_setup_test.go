package view

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func runtimeSetupModel() *Model {
	model := NewModel()
	model.Phase = PhaseAwaitingRuntime
	model.Runtime.SetupRequired = true
	model.Runtime.Error = "A DSH runtime is required before the host can start."
	model.Runtime.Candidates = []RuntimeChoice{
		{ID: "external-1", Kind: "external", Label: "Existing DSH installation", DSH: "0.2.1-rc.2", Node: "24.14.0", Path: "C:/bin/dsh.cmd", Compatible: true},
		{ID: "external-2", Kind: "external", Label: "Existing DSH installation", DSH: "0.1.0", Node: "22.0.0", Compatible: false, Reason: "DSH 0.1.0 is installed; this app requires 0.2.1-rc.2"},
		{ID: "legacy-bundled", Kind: "legacy", Label: "Runtime bundled with a previous installation", DSH: "0.2.1-rc.2", Node: "24.14.0", Compatible: true},
	}
	model.Runtime.Available = []RuntimeChoice{{ID: "runtime-1", Kind: "managed", Label: "Runtime bundle", DSH: "0.2.1-rc.2", Node: "24.14.0", HostVersion: "0.1.1", Detail: "212 MiB"}}
	model.Runtime.InstallFile = func() {}
	model.Runtime.CheckUpdates = func() {}
	model.Runtime.BrowseDSH = func() {}
	model.Runtime.UseInstalled = func(string) {}
	model.Runtime.Cancel = func() {}
	model.Quit = func() {}
	return model
}

func TestRuntimeSetupShowsSourcesAndUsesCallbacks(t *testing.T) {
	model := runtimeSetupModel()
	detected, downloaded, used := 0, 0, 0
	model.Runtime.Detect = func() { detected++ }
	model.Runtime.Download = func(id string) {
		downloaded++
		if id != "" {
			t.Fatalf("download id = %q", id)
		}
	}
	model.Runtime.UseExternal = func(command string) {
		used++
		if command != "C:/bin/dsh.cmd" {
			t.Fatalf("external command = %q", command)
		}
	}
	model.Runtime.UseLegacy = func() {}
	tt := tester(model)
	for _, text := range []string{"Set up DeepSeek Harness", "Use an installed DSH", "Install a runtime bundle", "Download latest runtime", "Choose .tgz file…"} {
		if !tt.HasText(text) {
			t.Fatalf("setup screen missing %q in %v", text, tt.Texts())
		}
	}
	if err := tt.Click("Check again"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Download latest runtime"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Use this DSH"); err != nil {
		t.Fatal(err)
	}
	if detected != 1 || downloaded != 1 || used != 1 {
		t.Fatalf("callbacks = detect %d download %d use %d", detected, downloaded, used)
	}
}

func TestRuntimeSetupOffersLegacyRuntime(t *testing.T) {
	model := runtimeSetupModel()
	model.Runtime.Candidates = model.Runtime.Candidates[2:]
	legacy := 0
	model.Runtime.UseLegacy = func() { legacy++ }
	model.Runtime.UseExternal = func(string) { t.Fatal("legacy runtime used the external path") }
	tt := tester(model)
	if !tt.HasText("Runtime bundled with a previous installation") {
		t.Fatalf("legacy candidate missing in %v", tt.Texts())
	}
	if err := tt.Click("Use this DSH"); err != nil {
		t.Fatal(err)
	}
	if legacy != 1 {
		t.Fatal("legacy callback was not called")
	}
}

func TestRuntimeSetupBusyShowsCancelAndMinimumWindow(t *testing.T) {
	model := runtimeSetupModel()
	model.Runtime.Busy = true
	model.Runtime.Status = "Downloading runtime"
	model.Runtime.Progress = 0.5
	cancelled := 0
	model.Runtime.Cancel = func() { cancelled++ }
	tt := ui.NewTester(func(c *ui.Context) { View(c, model) }, 560, 420)
	if !tt.HasText("Downloading runtime") {
		t.Fatalf("busy status missing in %v", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if cancelled != 1 {
		t.Fatal("cancel callback was not called")
	}
}

func TestRuntimeSetupQuitAtMinimumWindow(t *testing.T) {
	model := runtimeSetupModel()
	quits := 0
	model.Quit = func() { quits++ }
	tt := ui.NewTester(func(c *ui.Context) { View(c, model) }, 560, 420)
	if rect, ok := tt.Find("Quit"); !ok || rect.W <= 0 || rect.H <= 0 {
		t.Fatalf("quit is not visible at the minimum window: %+v", rect)
	}
	if err := tt.Click("Quit"); err != nil {
		t.Fatal(err)
	}
	if quits != 1 {
		t.Fatal("quit callback was not called")
	}
	wide := ui.NewTester(func(c *ui.Context) { View(c, model) }, 1280, 820)
	wide.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
	wide.Frame()
	if rect, ok := wide.Find("Quit"); !ok || rect.X+rect.W > 1280-138 {
		t.Fatalf("quit overlaps the window controls: %+v", rect)
	}
}

func TestRuntimeSetupBlocksShellShortcutsAndSending(t *testing.T) {
	model := runtimeSetupModel()
	sent := 0
	model.Send = func(string) { sent++ }
	if model.CanOpenPreferences() {
		t.Fatal("preferences opened during runtime setup")
	}
	if model.CanRefreshSessions() || model.CanChooseSession() {
		t.Fatal("session navigation enabled during runtime setup")
	}
	tt := tester(model)
	_ = tt.Click("Send")
	if sent != 0 {
		t.Fatal("send was enabled during runtime setup")
	}
}
