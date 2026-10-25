package view

import (
	"github.com/egoist/mygo/ui"
	"testing"
	"time"
)

func TestSessionSidebarSearchResizeAndArchivedFilter(t *testing.T) {
	model := sessionModel()
	model.Sessions = []SessionSummary{{ID: "one", Title: "Alpha", Status: "idle"}, {ID: "two", Title: "Beta", Status: "stored", Archived: true}}
	model.SessionPanel.CanArchive = true
	tt := tester(model)
	if !tt.HasText("Conversations") || !tt.HasText("Current · Alpha") || tt.HasText("Beta") {
		t.Fatalf("sidebar: %v", tt.Texts())
	}
	model.SessionPanel.ShowArchived = true
	model.SessionPanel.Search = "beTA"
	tt.Frame()
	if !tt.HasText("Beta") || tt.HasText("Current · Alpha") {
		t.Fatalf("filter: %v", tt.Texts())
	}
	tt.SetSize(560, 420)
	tt.Frame()
	if tt.HasText("Conversations") {
		t.Fatal("narrow window kept the sidebar")
	}
	if err := clickToolbarMenu(tt, "Sessions"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("Beta") {
		t.Fatalf("drawer lost filter: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.SetSize(1000, 760)
	tt.Frame()
	tt.Frame()
	if !tt.HasText("Beta") {
		t.Fatal("resizing lost sidebar state")
	}
}

func TestSessionManagementFormIdentityAndLegacyCapabilities(t *testing.T) {
	model := sessionModel()
	model.SessionPanel.CanRename = true
	model.SessionPanel.EditingID = "one"
	model.SessionPanel.Title = "First title"
	model.SessionPanel.SaveRename = func() {}
	model.SessionPanel.CancelEdit = func() { model.SessionPanel.EditingID = "" }
	tt := tester(model)
	if !tt.HasText("Rename conversation") {
		t.Fatal("rename form missing")
	}
	model.SessionPanel.Writing = true
	model.SessionBusy = true
	tt.Frame()
	_ = tt.Click("Cancel rename")
	if model.SessionPanel.EditingID == "" {
		t.Fatal("dismissed a pending write")
	}
	model.SessionPanel.Writing = false
	model.SessionBusy = false
	model.SessionPanel.EditingID = "two"
	model.SessionPanel.Title = "Second title"
	tt.Frame()
	tt.Frame()
	if err := tt.Click("Conversation title"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Type("Updated second title")
	if model.SessionPanel.Title != "Updated second title" {
		t.Fatalf("stale input identity: %q", model.SessionPanel.Title)
	}
	model.SessionPanel.EditingID = ""
	model.SessionPanel.CanRename = false
	tt.Frame()
	if tt.HasText("Rename") {
		t.Fatal("old host exposes rename")
	}
}
