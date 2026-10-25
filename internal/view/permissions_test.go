package view

import (
	"github.com/egoist/mygo/ui"
	"testing"
	"time"
)

func permissionModel() *Model {
	model := settingsModel()
	model.Permission.Available = true
	model.Permission.Accept(PermissionSnapshot{SessionID: "s", Current: "workspace-write", Sandbox: "workspace-write", Approval: "ask", Options: []PermissionOption{
		{ID: "workspace-write", Name: "Workspace", Sandbox: "workspace-write", Approval: "ask"},
		{ID: "danger-full-access", Name: "Full", Sandbox: "danger-full-access", Approval: "never", RequiresConfirmation: true},
	}})
	return model
}
func TestPermissionControlsRequireConfirmationAndResetOnSelection(t *testing.T) {
	model := permissionModel()
	model.Draft = "draft"
	applied := 0
	model.Permission.Apply = func() { applied++ }
	tester := ui.NewTester(func(c *ui.Context) { permissionControls(c, model, false) }, 560, 420)
	if err := tester.Click("workspace-write"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("danger-full-access"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	_ = tester.Click("Apply permissions")
	if applied != 0 {
		t.Fatal("dangerous preset applied without confirmation")
	}
	if err := tester.Click("I understand this permission change"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Apply permissions"); err != nil {
		t.Fatal(err)
	}
	if applied != 1 || model.Draft != "draft" {
		t.Fatal("confirmation did not apply or draft lost")
	}
	if err := tester.Click("danger-full-access"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("workspace-write"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if model.Permission.Confirm {
		t.Fatal("confirmation carried over to another preset")
	}
	model.Permission.Available = false
	tester.Frame()
	model.Permission.Available = true
	tester.Frame()
}
func TestPermissionOnlySettingsFitsMinimumWindowAndUncertaintyBlocksSend(t *testing.T) {
	model := permissionModel()
	model.CanModels = false
	model.SettingsOpen = true
	tester := ui.NewTester(func(c *ui.Context) { View(c, model) }, 560, 420)
	tester.Advance(300 * time.Millisecond)
	if !tester.HasText("Session permissions") || tester.HasText("Apply model") {
		t.Fatal("permissions depend on model capability")
	}
	if err := tester.Click("Close settings"); err != nil {
		t.Fatal(err)
	}
	model.Permission.Uncertain = true
	model.Draft = "draft"
	model.Send = func(string) { t.Fatal("sent with uncertain permissions") }
	tester.Frame()
	_ = tester.Click("Send")
	if !tester.HasText("Permissions unconfirmed · open Settings to refresh") {
		t.Fatal("permission recovery hint missing")
	}
}
