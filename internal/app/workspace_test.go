package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"dsh-mygo/internal/view"
	"dsh-mygo/internal/workspace"
)

func workspaceUpdate(t *testing.T, updates <-chan func()) func() {
	t.Helper()
	select {
	case fn := <-updates:
		return fn
	case <-time.After(5 * time.Second):
		t.Fatal("workspace job timed out")
		return nil
	}
}

func TestWorkspaceUntrackedChangePreviewsWithoutHeadAndCannotEdit(t *testing.T) {
	a, updates := sessionTestApp(&fakeClient{})
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("new file\n"), 0600)
	a.model.SessionCWD = root
	a.syncWorkspace()
	a.model.Workspace.Changes.Files = []workspace.Change{{Path: "new.txt", Status: "??"}}
	a.readWorkspaceDiff("new.txt")
	workspaceUpdate(t, updates)()
	w := &a.model.Workspace
	if w.DiffError != "" || w.Diff != "new file\n" || w.DiffCode == nil || !w.DiffCode.ReadOnly {
		t.Fatal("new file preview failed without a HEAD commit")
	}
	a.readWorkspaceDiff("../outside")
	if w.DiffBusy || w.DiffPath != "new.txt" {
		t.Fatal("accepted a path outside the change catalog")
	}
}

func TestWorkspaceFilePreviewAndProjectSwitchRejectLateResults(t *testing.T) {
	a, updates := sessionTestApp(&fakeClient{})
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "note.md"), []byte("# project one"), 0600)
	a.model.SessionCWD = root
	a.syncWorkspace()
	a.model.ToggleWorkspacePanel(view.PanelFiles)
	workspaceUpdate(t, updates)()
	if len(a.model.Workspace.Directories[""]) != 1 {
		t.Fatal("project directory not loaded")
	}
	a.openWorkspaceFile("note.md")
	late := workspaceUpdate(t, updates)
	a.model.SessionCWD = t.TempDir()
	a.syncWorkspace()
	late()
	if a.model.Workspace.Preview.Path != "" || a.model.Workspace.PreviewCode != nil || a.model.Workspace.PreviewBusy {
		t.Fatal("old project preview leaked into new project")
	}
	workspaceUpdate(t, updates)() // open file panel refresh in the new root
	if len(a.model.Workspace.Directories[""]) != 0 {
		t.Fatal("old directory entries survived")
	}
}

func TestWorkspacePreviewUsesReadonlyNativeCodeAndDoesNotChangeDraft(t *testing.T) {
	a, updates := sessionTestApp(&fakeClient{})
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "hello.go"), []byte("package hello\n"), 0600)
	a.model.SessionCWD = root
	a.syncWorkspace()
	a.model.Draft = "keep draft"
	a.openWorkspaceFile("hello.go")
	workspaceUpdate(t, updates)()
	w := &a.model.Workspace
	if w.Preview.Text != "package hello\n" || w.PreviewCode == nil || !w.PreviewCode.ReadOnly || a.model.Draft != "keep draft" || !w.Open[view.PanelPreview] {
		t.Fatal("preview failed or changed editing state")
	}
	a.openWorkspaceFile("../secret")
	workspaceUpdate(t, updates)()
	if w.PreviewError == "" || w.Preview.Text != "package hello\n" {
		t.Fatal("invalid read was not rejected safely")
	}
}

func TestNativeWorkspaceTerminalSurvivesHideAndClosesOnProjectSwitch(t *testing.T) {
	if os.Getenv("DSH_MYGO_NATIVE_TERMINAL") != "1" {
		t.Skip("set DSH_MYGO_NATIVE_TERMINAL=1 for native resource lifecycle")
	}
	a, updates := sessionTestApp(&fakeClient{})
	t.Cleanup(func() { a.Shutdown(3 * time.Second) })
	a.model.SessionCWD = t.TempDir()
	a.syncWorkspace()
	a.model.ToggleWorkspacePanel(view.PanelTerminal)
	workspaceUpdate(t, updates)()
	first := a.model.Workspace.Terminal
	if first == nil {
		t.Fatalf("terminal did not start: %s", a.model.Workspace.TerminalError)
	}
	a.model.ToggleWorkspacePanel(view.PanelTerminal)
	if a.model.Workspace.Terminal != first {
		t.Fatal("hiding destroyed terminal state")
	}
	a.model.ToggleWorkspacePanel(view.PanelTerminal)
	if a.model.Workspace.Terminal != first {
		t.Fatal("showing restarted the shell")
	}
	a.model.SessionCWD = t.TempDir()
	a.syncWorkspace()
	workspaceUpdate(t, updates)()
	if a.model.Workspace.Terminal == nil || a.model.Workspace.Terminal == first {
		t.Fatal("new project reused old terminal")
	}
	select {
	case <-first.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("old terminal survived project switch")
	}
	second := a.model.Workspace.Terminal
	a.Shutdown(3 * time.Second)
	select {
	case <-second.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("terminal survived app shutdown")
	}
}
