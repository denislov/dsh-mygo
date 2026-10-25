package view

import (
	"dsh-mygo/internal/workspace"
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestWorkspaceToolbarTogglesIndependentNativePanels(t *testing.T) {
	m := sessionModel()
	m.SessionCWD = "C:/work/project"
	m.Workspace.Directories = map[string][]workspace.Entry{"": {{Name: "note.md", Path: "note.md"}}}
	opened := ""
	m.Workspace.OpenFile = func(path string) { opened = path }
	tt := wideTester(m)
	for _, label := range []string{"Toggle Session rail", "Toggle Terminal", "Toggle Changes", "Toggle Preview", "Toggle Files", "More"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("toolbar missing %s", label)
		}
	}
	for _, label := range []string{"Preferences", "Conversation details", "Subagents"} {
		if _, ok := tt.Find(label); ok {
			t.Fatalf("old toolbar entry %s survived", label)
		}
	}
	for _, panel := range []WorkspacePanel{PanelTerminal, PanelFiles, PanelPreview} {
		if err := tt.Click("Toggle " + workspacePanelLabel(panel)); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !m.Workspace.Open[panel] {
			t.Fatalf("panel %s not toggled", panel)
		}
	}
	if err := tt.Click("note.md"); err != nil {
		t.Fatal(err)
	}
	if opened != "note.md" {
		t.Fatal("file did not open in preview")
	}
	if err := tt.Click("Close Files"); err != nil {
		t.Fatal(err)
	}
	if m.Workspace.Open[PanelFiles] || !m.Workspace.Open[PanelTerminal] {
		t.Fatal("close affected other panels")
	}
}

func TestWorkspacePanelsRemainReachableAtMinimumSize(t *testing.T) {
	m := sessionModel()
	tt := ui.NewTester(func(c *ui.Context) { View(c, m) }, 560, 420)
	for _, panel := range []WorkspacePanel{PanelTerminal, PanelPreview, PanelFiles} {
		tt.Click("Toggle " + workspacePanelLabel(panel))
		tt.Frame()
	}
	if _, ok := tt.Find("Back to conversation"); !ok {
		t.Fatal("narrow workspace has no return action")
	}
	if err := tt.Click("Back to conversation"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Message"); !ok {
		t.Fatal("conversation did not return")
	}
}
