package view

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestWideShellShowsSidebarAndTitleBar(t *testing.T) {
	model := sessionModel()
	tt := tester(model)
	if !tt.HasText("Conversations") || !tt.HasText("dsh-mygo") || !tt.HasText("Hide sidebar") {
		t.Fatalf("wide shell missing chrome: %v", tt.Texts())
	}
	if err := tt.Click("Hide sidebar"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("Conversations") || !tt.HasText("Show sidebar") {
		t.Fatal("sidebar collapse did not take effect")
	}
	if err := tt.Click("Show sidebar"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Conversations") {
		t.Fatal("sidebar did not return")
	}
}

func TestCompactShellUsesSessionsModal(t *testing.T) {
	model := sessionModel()
	tt := ui.NewTester(func(c *ui.Context) { View(c, model) }, 560, 420)
	tt.Frame()
	if tt.HasText("Conversations") || tt.HasText("Hide sidebar") {
		t.Fatalf("compact shell kept the sidebar: %v", tt.Texts())
	}
	if err := clickToolbarMenu(tt, "Sessions"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("Conversations") {
		t.Fatal("compact shell did not open the sessions modal")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Advance(300 * time.Millisecond)
	if model.SessionsOpen {
		t.Fatal("Escape did not close the sessions modal")
	}
}

func TestTitleBarKeepsClearOfWindowControls(t *testing.T) {
	model := sessionModel()
	tt := tester(model)
	const systemButtons = float32(138)
	tt.SetTitleBar(ui.TitleBar{Height: 44, Right: systemButtons})
	for _, label := range []string{"Menu", "Toggle Session rail", "Toggle Terminal", "Toggle Changes", "Toggle Preview", "Toggle Files", "More"} {
		bounds, ok := tt.Find(label)
		if !ok {
			t.Fatalf("title bar missing %q", label)
		}
		if bounds.X < 0 || bounds.X+bounds.W > 900-systemButtons {
			t.Fatalf("%s overlaps the window controls: %+v", label, bounds)
		}
		if bounds.Y < 0 || bounds.Y+bounds.H > 44 {
			t.Fatalf("%s is outside the title bar: %+v", label, bounds)
		}
	}
}

func TestHiddenSidebarStillReachesSessions(t *testing.T) {
	model := sessionModel()
	model.Shell.SidebarOpen = false
	tt := tester(model)
	if tt.HasText("Conversations") {
		t.Fatal("hidden sidebar still rendered")
	}
	if err := clickToolbarMenu(tt, "Sessions"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("Conversations") {
		t.Fatal("sessions entry disappeared with the sidebar")
	}
}
