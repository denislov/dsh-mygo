package view

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestRailFloatsWithoutChangingToolPanelLayout(t *testing.T) {
	m := sessionModel()
	m.Workspace.Open = map[WorkspacePanel]bool{PanelTerminal: true, PanelChanges: true}
	tt := ui.NewTester(func(c *ui.Context) { View(c, m) }, 1356, 900)
	before, _ := tt.Find("Workspace panel · Terminal")
	if before.W < 100 || before.H < 100 {
		t.Fatalf("terminal pane did not fill its slot: %+v", before)
	}
	percent := m.Workspace.ChatPercent
	if err := tt.Click("Toggle Session rail"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	after, _ := tt.Find("Workspace panel · Terminal")
	floating, ok := tt.Find("Floating Session rail")
	if !ok {
		t.Fatal("rail should float at this width")
	}
	if floating.X < sidebarWidth || floating.X+floating.W > after.X {
		t.Fatalf("rail should float over chat, not tools: rail=%+v tool=%+v", floating, after)
	}
	if before != after || m.Workspace.ChatPercent != percent {
		t.Fatalf("rail changed tool layout: %+v => %+v", before, after)
	}
	if _, ok := tt.Find("Message"); !ok {
		t.Fatal("rail replaced conversation")
	}
	message, _ := tt.Find("Message")
	tt.ClickAt(message.X+10, message.Y+10)
	tt.Frame()
	if m.Workspace.Open[PanelRail] {
		t.Fatal("clicking chat outside the rail did not dismiss it")
	}
	if err := tt.Click("Toggle Session rail"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if m.Workspace.Open[PanelRail] || !m.Workspace.Open[PanelTerminal] {
		t.Fatal("dismissal did not isolate rail")
	}
}

func TestRailDocksSeparatelyOnWideWindow(t *testing.T) {
	m := sessionModel()
	m.Workspace.Open = map[WorkspacePanel]bool{PanelRail: true, PanelTerminal: true, PanelChanges: true}
	tt := ui.NewTester(func(c *ui.Context) { View(c, m) }, 1920, 1000)
	tt.Frame()
	rail, ok := tt.Find("Docked Session rail")
	terminal, found := tt.Find("Workspace panel · Terminal")
	message, _ := tt.Find("Message")
	if !ok || !found || rail.X < message.X+message.W || rail.X+rail.W > terminal.X {
		t.Fatalf("rail should sit between chat and tools: %+v %+v %+v", message, rail, terminal)
	}
	if _, ok := tt.Find("Floating Session rail"); ok {
		t.Fatal("wide rail should not float")
	}
	if _, ok := tt.Find("Maximize Session rail"); ok {
		t.Fatal("rail should not be a tile")
	}
	separator, ok := tt.Find("Workspace tools separator")
	railButton, _ := tt.Find("Toggle Session rail")
	terminalButton, _ := tt.Find("Toggle Terminal")
	if !ok || separator.X < railButton.X+railButton.W || separator.X+separator.W > terminalButton.X {
		t.Fatal("rail toolbar group is not separated")
	}
}

func TestRailAloneFloatsAtMinimumWindowWithoutHidingComposer(t *testing.T) {
	m := sessionModel()
	m.Workspace.Open = map[WorkspacePanel]bool{PanelRail: true}
	tt := ui.NewTester(func(c *ui.Context) { View(c, m) }, 560, 420)
	tt.Frame()
	r, ok := tt.Find("Floating Session rail")
	if !ok || r.X < 0 || r.Y < WindowTitleBarHeight || r.X+r.W > 560 || r.Y+r.H > 420 {
		t.Fatalf("rail outside window: %+v", r)
	}
	if _, ok := tt.Find("Message"); !ok {
		t.Fatal("rail must leave the conversation mounted")
	}
	if _, ok := tt.Find("Back to conversation"); ok {
		t.Fatal("rail must not use tool tabs")
	}
}

func TestWorkspaceResizeHandlesReplaceDividers(t *testing.T) {
	m := sessionModel()
	m.Workspace.Open = map[WorkspacePanel]bool{PanelTerminal: true, PanelChanges: true}
	tt := wideTester(m)
	for _, label := range []string{"Resize tools", "Resize panels · Terminal / Changes"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("missing resize handle %s", label)
		}
	}
	if _, ok := tt.Find("Divider"); ok {
		t.Fatal("permanent native divider survived")
	}
}

func TestWorkspaceGuttersShowGripOnHoverAndResizeByMouseAndKeyboard(t *testing.T) {
	m := sessionModel()
	m.Workspace.Open = map[WorkspacePanel]bool{PanelTerminal: true, PanelChanges: true}
	tt := wideTester(m)
	for _, check := range []struct {
		label    string
		vertical bool
		cursor   ui.Cursor
	}{
		{"Resize tools", false, ui.CursorResizeEW}, {"Resize panels · Terminal / Changes", true, ui.CursorResizeNS},
	} {
		tt.Move(0, 0)
		r, ok := tt.Find(check.label)
		if !ok || r.W < 3 || r.H < 3 {
			t.Fatalf("missing gutter bounds: %+v", r)
		}
		x, y := r.X+r.W/2, r.Y+r.H/2
		before := tt.Image().RGBAAt(int(x), int(y))
		tt.Move(x, y)
		if tt.Cursor() != check.cursor {
			t.Fatalf("wrong cursor for %s", check.label)
		}
		if after := tt.Image().RGBAAt(int(x), int(y)); before == after {
			t.Fatalf("hover grip not painted for %s", check.label)
		}
		percent := &m.Workspace.ChatPercent
		if check.vertical {
			percent = m.Workspace.Splits["root"]
		}
		old := *percent
		tt.Press(x, y)
		if check.vertical {
			tt.Move(x, y+30)
			tt.Release(x, y+30)
		} else {
			tt.Move(x+30, y)
			tt.Release(x+30, y)
		}
		if *percent <= old || *percent-old > 10 {
			t.Fatalf("drag delta lost or replayed: %f => %f", old, *percent)
		}
		old = *percent
		key := ui.KeyRight
		if check.vertical {
			key = ui.KeyDown
		}
		tt.Key(0, key)
		if *percent <= old {
			t.Fatal("keyboard did not resize focused gutter")
		}
	}
}

func TestRailRespondsToResizeAndToolSplitWithoutLosingState(t *testing.T) {
	m := sessionModel()
	m.Workspace.Open = map[WorkspacePanel]bool{PanelRail: true, PanelTerminal: true, PanelChanges: true}
	m.Shell.ContextOpen = false
	tt := ui.NewTester(func(c *ui.Context) { View(c, m) }, 1920, 1000)
	for _, size := range []int{1356, 1920, 560, 1920} {
		tt.SetSize(size, 900)
		tt.Advance(300 * time.Millisecond)
		label := "Floating Session rail"
		if size == 1920 {
			label = "Docked Session rail"
		}
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("wrong rail mode at width %d", size)
		}
		if !m.Workspace.Open[PanelRail] || m.Shell.ContextOpen {
			t.Fatal("responsive mode reset rail state")
		}
	}
	m.Workspace.ChatPercent = 40
	tt.Frame()
	if _, ok := tt.Find("Floating Session rail"); !ok {
		t.Fatal("tool resize should allow rail to float")
	}
	m.Workspace.Open[PanelPreview] = true
	m.Workspace.ChatPercent = 60
	m.ToggleWorkspacePanel(PanelRail)
	m.ToggleWorkspacePanel(PanelRail)
	if math.Abs(float64(m.Workspace.ChatPercent-60)) > 0.01 {
		t.Fatal("rail changed tool proportions")
	}
}
