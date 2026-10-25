package view

import (
	"testing"
	"time"

	"dsh-mygo/internal/projection"
	"github.com/egoist/mygo/ui"
)

func wideTester(model *Model) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) { View(c, model) }, 1280, 800)
}

func TestDetailsPaneOpensOnDemandOnWideWindows(t *testing.T) {
	model := sessionModel()
	model.SessionCWD = "C:/work/project"
	model.Detail = "host-build-fixture"
	model.Context = projection.ContextView{Window: 128000}
	tt := wideTester(model)
	tt.Frame()
	if tt.HasText("host-build-fixture") {
		t.Fatal("details should start collapsed")
	}
	if err := tt.Click("Toggle Session rail"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, text := range []string{"Progress", "Environment", "Context", "C:/work/project", "host-build-fixture", "Context 128k"} {
		if !tt.HasText(text) {
			t.Fatalf("details pane missing %q in %v", text, tt.Texts())
		}
	}
	if err := tt.Click("Toggle Session rail"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if model.Workspace.Open[PanelRail] || tt.HasText("C:/work/project") {
		t.Fatal("details toggle did not hide the pane")
	}
	if err := tt.Click("Toggle Session rail"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !model.Workspace.Open[PanelRail] || !tt.HasText("C:/work/project") {
		t.Fatal("details pane did not return")
	}
}

func TestSessionRailUsesWorkspaceBelowBreakpoint(t *testing.T) {
	model := sessionModel()
	model.SessionCWD = "C:/work/project"
	tt := tester(model)
	if tt.HasText("C:/work/project") {
		t.Fatal("narrow window rendered the wide pane")
	}
	if err := tt.Click("Toggle Session rail"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("C:/work/project") || !model.Workspace.Open[PanelRail] {
		t.Fatal("narrow window did not open the session rail")
	}
}

func TestWideDetailsPaneSurvivesModalWithoutReflow(t *testing.T) {
	model := sessionModel()
	model.SessionCWD = "C:/work/project"
	tt := wideTester(model)
	tt.Frame()
	if err := tt.Click("Toggle Session rail"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := clickToolbarMenu(tt, "Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !model.Workspace.Open[PanelRail] || !tt.HasText("C:/work/project") {
		t.Fatal("wide pane reflowed away when a modal opened")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if !model.Workspace.Open[PanelRail] || !tt.HasText("C:/work/project") {
		t.Fatal("wide pane did not survive the modal")
	}
}

func TestSessionRailAtMinimumWindow(t *testing.T) {
	model := sessionModel()
	model.SessionCWD = "C:/work/project"
	tt := ui.NewTester(func(c *ui.Context) { View(c, model) }, 560, 420)
	tt.Frame()
	if err := tt.Click("Toggle Session rail"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	for _, text := range []string{"Progress", "Environment", "Context"} {
		if !tt.HasText(text) {
			t.Fatalf("compact details missing %q in %v", text, tt.Texts())
		}
	}
}
