package view

import (
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestSettingsHasSearchCornerCloseAndImmediatePreferences(t *testing.T) {
	m := NewModel()
	m.Phase = PhaseIdle
	m.OpenPreferences()
	tt := tester(m)
	tt.SetTitleBar(ui.TitleBar{Height: 44, Right: 138})
	for _, label := range []string{"Cancel", "Apply preferences", "Reset preferences"} {
		if tt.HasText(label) {
			t.Fatalf("old footer %s survived", label)
		}
	}
	close, ok := tt.Find("Close settings")
	if !ok || close.Y < 44 {
		t.Fatal("corner close missing")
	}
	search, ok := tt.Find("Search settings")
	if !ok || search.X >= close.X || close.Y > search.Y+45 {
		t.Fatal("search / corner close layout is wrong")
	}
	if err := tt.Click("Dark theme"); err != nil {
		t.Fatal(err)
	}
	if m.Prefs.Value.Theme != "dark" || !m.SettingsOpen {
		t.Fatal("theme was not saved in place")
	}
	if err := tt.Click("Search settings"); err != nil {
		t.Fatal(err)
	}
	tt.Type("shortcut")
	if tt.HasText("About") {
		t.Fatal("settings navigation was not filtered")
	}
	if err := tt.Click("Close settings"); err != nil {
		t.Fatal(err)
	}
	if m.SettingsOpen || m.Prefs.Value.Theme != "dark" {
		t.Fatal("closing lost saved preference")
	}
}

func TestModelPageUsesProviderCardsAndInlineEditor(t *testing.T) {
	m := settingsModel()
	m.SettingsOpen = true
	tt := ui.NewTester(func(c *ui.Context) { View(c, m) }, 1100, 760)
	if !tt.HasText("Model providers") || !tt.HasText("Test") {
		t.Fatal("provider cards missing")
	}
	if err := tt.Click("Edit provider · Test"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Model ID") || !tt.HasText("Apply model") {
		t.Fatal("inline editor missing")
	}
}
