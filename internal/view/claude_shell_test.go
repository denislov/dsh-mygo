package view

import (
	"testing"
	"time"

	"dsh-mygo/internal/projection"
	"github.com/egoist/mygo/ui"
)

func TestProjectGroupsKeepHostOrderAndDistinctPaths(t *testing.T) {
	m := sessionModel()
	m.Sessions = []SessionSummary{
		{ID: "a", Title: "A", CWD: `D:\work\app`},
		{ID: "b", Title: "B", CWD: `D:\other\app`},
		{ID: "c", Title: "C", CWD: `d:/work/app/`},
		{ID: "d", Title: "D"},
	}
	entries := m.projectSessionEntries()
	want := []string{"project:d:/work/app", "session:a", "session:c", "project:d:/other/app", "session:b", "project:", "session:d"}
	if len(entries) != len(want) {
		t.Fatalf("entries = %+v", entries)
	}
	for i := range want {
		if entries[i].Key != want[i] {
			t.Fatalf("entry %d = %q, want %q", i, entries[i].Key, want[i])
		}
	}
	m.SessionPanel.Search = "C"
	entries = m.projectSessionEntries()
	if len(entries) != 2 || entries[1].Session.ID != "c" {
		t.Fatalf("search leaked empty project groups: %+v", entries)
	}
}

func TestTwoColumnShellOwnsSeparateHeaders(t *testing.T) {
	m := sessionModel()
	tt := tester(m)
	menu, _ := tt.Find("Menu")
	title, ok := tt.Find("Conversation title · " + activeSessionTitle(m))
	if !ok || menu.X >= sidebarWidth || title.X < sidebarWidth || title.Y > WindowTitleBarHeight {
		t.Fatalf("headers cross columns: menu=%+v title=%+v", menu, title)
	}
	if m.Workspace.Open[PanelRail] {
		t.Fatal("third column should require disclosure")
	}
}

func TestActivityAndAnswerShareReadingBoundary(t *testing.T) {
	m := sessionModel()
	m.Items = []projection.Item{{ID: "r", Kind: projection.KindReasoning, Text: "thinking"}, {ID: "a", Kind: projection.KindAssistant, Title: "Assistant", Text: "Aligned answer"}}
	tt := wideTester(m)
	activity, ok := tt.Find("Activity")
	answer, found := tt.Find("Aligned answer")
	if !ok || !found || activity.X < answer.X || activity.X > answer.X+24 {
		t.Fatalf("activity and answer use different reading columns: %+v / %+v", activity, answer)
	}
}

func TestMessageActionsFollowRoleAndStayOutsideUserBubble(t *testing.T) {
	m := sessionModel()
	m.Items = []projection.Item{
		{ID: "u", Kind: projection.KindUser, Title: "You", Text: "Short user message"},
		{ID: "a", Kind: projection.KindAssistant, Title: "Assistant", Text: "Assistant answer"},
	}
	tt := tester(m)
	u, _ := tt.Find("Short user message")
	a, _ := tt.Find("Assistant answer")
	uc, _ := tt.Find("Copy message")
	ac, _ := tt.Find("Copy Markdown")
	if u.X <= a.X || uc.Y < u.Y+u.H || ac.X > a.X+40 {
		t.Fatalf("message alignment: user=%+v assistant=%+v userCopy=%+v assistantCopy=%+v", u, a, uc, ac)
	}
}

func TestComposerQuickModelSelectionKeepsDraftAndAvoidsSettingsCenter(t *testing.T) {
	m := settingsModel()
	m.Draft = "keep my draft"
	applied := 0
	m.ApplySettings = func() {
		applied++
		m.SetActiveRoute(ModelRoute{Provider: m.SettingsProvider, Model: m.SettingsModel})
		m.SettingsOpen = false
	}
	tt := tester(m)
	if err := tt.Click("Choose model"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if tt.HasText("Model ID") || tt.HasText("Interface preferences") || !tt.HasText("Echo") {
		t.Fatalf("expected quick catalog, got %v", tt.Texts())
	}
	if err := tt.Click("Use model · Echo"); err != nil {
		t.Fatal(err)
	}
	if applied != 1 || m.Draft != "keep my draft" {
		t.Fatal("quick model selection lost draft or did not apply")
	}
}

func TestComposerQuickPermissionsExplicitModeChoiceAppliesOnce(t *testing.T) {
	m := settingsModel()
	m.Permission.Available = true
	m.Permission.Accept(PermissionSnapshot{Current: "safe", Options: []PermissionOption{
		{ID: "safe", Name: "Ask before changes", Sandbox: "workspace-write", Approval: "ask"},
		{ID: "danger", Name: "Full access", Sandbox: "danger-full-access", Approval: "never", RequiresConfirmation: true},
	}})
	applied := 0
	m.Permission.Apply = func() {
		if !m.Permission.Confirm || m.Permission.Selected != "danger" {
			t.Fatal("mode choice did not confirm its actual policy")
		}
		applied++
	}
	tt := tester(m)
	if err := tt.Click("Choose permissions"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if err := tt.Click("Permission option · Full access"); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatal("explicit mode choice was not applied once")
	}
	for _, label := range []string{"Apply permissions", "I understand this permission change", "More settings…"} {
		if _, ok := tt.Find(label); ok {
			t.Fatalf("complex control survived in quick picker: %s", label)
		}
	}
	tt.Key(0, ui.KeyEscape)
	tt.Advance(300 * time.Millisecond)
	if m.SettingsOpen || m.Permission.Confirm {
		t.Fatal("dismissed quick selector retained modal state")
	}
}

func TestQuickSelectorMinimumWindowAndRepeatedDismissal(t *testing.T) {
	m := settingsModel()
	m.Draft = "keep"
	m.CloseSettings = func() { m.SettingsOpen = false; m.SettingsBusy = false }
	for _, size := range [][2]float32{{560, 380}, {900, 600}} {
		tt := ui.NewTester(func(c *ui.Context) { View(c, m) }, int(size[0]), int(size[1]))
		for range 3 {
			if err := tt.Click("Choose model"); err != nil {
				t.Fatal(err)
			}
			tt.Advance(300 * time.Millisecond)
			for _, label := range []string{"Close selector", "Use model · Echo", "More settings…"} {
				r, ok := tt.Find(label)
				if !ok || r.Y < 0 || r.Y+r.H > size[1] || r.X < 0 || r.X+r.W > size[0] {
					t.Fatalf("%s outside window: %+v", label, r)
				}
			}
			tt.Key(0, ui.KeyEscape)
			tt.Advance(300 * time.Millisecond)
			if m.SettingsOpen || m.QuickSelector != "" || m.Draft != "keep" {
				t.Fatal("dismissal retained stale state")
			}
		}
	}
}

func TestPermissionModePickerFitsMinimumClientArea(t *testing.T) {
	m := settingsModel()
	m.Permission.Available = true
	m.Permission.Accept(PermissionSnapshot{Current: "safe", Options: []PermissionOption{{ID: "danger", Name: "Full access", Sandbox: "danger-full-access", Approval: "never", RequiresConfirmation: true}}})
	m.Permission.Apply = func() {}
	tt := ui.NewTester(func(c *ui.Context) { View(c, m) }, 560, 380)
	if err := tt.Click("Choose permissions"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if err := tt.Click("Permission option · Full access"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Mode", "Permission option · Full access", "Full file access without approval prompts."} {
		r, ok := tt.Find(label)
		if !ok || r.Y < 0 || r.Y+r.H > 380 {
			t.Fatalf("%s outside client area: %+v", label, r)
		}
	}
}

func TestQuickPermissionPickerBlocksUncertainAndInFlightChoices(t *testing.T) {
	m := permissionModel()
	applied := 0
	m.Permission.Apply = func() { applied++; m.SettingsBusy = true; m.SettingsApplying = true }
	tt := tester(m)
	if err := tt.Click("Choose permissions"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	m.Permission.Uncertain = true
	tt.Frame()
	_ = tt.Click("Permission option · Full")
	if applied != 0 {
		t.Fatal("uncertain policy was editable")
	}
	m.Permission.Uncertain = false
	tt.Frame()
	if err := tt.Click("Permission option · Full"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	_ = tt.Click("Permission option · Full")
	if applied != 1 || m.Permission.Snapshot.Current != "workspace-write" || m.Permission.Confirm {
		t.Fatal("duplicate write, optimistic policy, or stale confirmation")
	}
}
