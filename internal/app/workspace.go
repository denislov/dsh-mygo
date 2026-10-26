package app

import (
	"context"
	"errors"
	"time"

	"dsh-mygo/internal/view"
	"dsh-mygo/internal/workspace"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui/editor"
)

func (a *App) configureWorkspace() {
	w := &a.model.Workspace
	w.Load = a.loadWorkspacePanel
	w.ReadDirectory = a.readWorkspaceDirectory
	w.OpenFile = a.openWorkspaceFile
	w.ReadDiff = a.readWorkspaceDiff
	w.RestartTerminal = a.restartWorkspaceTerminal
}

func (a *App) syncWorkspace() {
	w := &a.model.Workspace
	root := a.model.SessionCWD
	if root == w.Root {
		return
	}
	a.workspace.switchProject()
	w.Root = root
	w.Directories = make(map[string][]workspace.Entry)
	w.Loading = make(map[string]bool)
	w.Expanded = make(map[string]bool)
	w.Preview = workspace.File{}
	w.PreviewImage = nil
	w.PreviewCode = nil
	w.DiffCode = nil
	w.PreviewBusy = false
	w.PreviewError = ""
	w.PreviewURL = ""
	w.Changes = workspace.Changes{}
	w.ChangesBusy = false
	w.ChangesError = ""
	w.DiffPath = ""
	w.Diff = ""
	w.DiffBusy = false
	w.DiffError = ""
	w.Terminal = nil
	w.TerminalBusy = false
	w.TerminalError = ""
	w.Filter = ""
	w.FileError = ""
	for _, panel := range []view.WorkspacePanel{view.PanelRail, view.PanelTerminal, view.PanelChanges, view.PanelFiles} {
		if w.Open[panel] {
			a.loadWorkspacePanel(panel)
		}
	}
}

func (a *App) currentWorkspace(generation uint64) bool {
	return !a.shuttingDown.Load() && a.workspace.current(generation)
}

func (a *App) workspaceContext() context.Context {
	return a.workspace.project.readContext(a.shuttingDown.Load())
}

func (a *App) cancelWorkspaceReads() {
	a.workspace.project.cancelReads()
}

func (a *App) loadWorkspacePanel(panel view.WorkspacePanel) {
	if a.shuttingDown.Load() {
		return
	}
	a.syncWorkspace()
	switch panel {
	case view.PanelTerminal:
		if a.model.Workspace.Terminal == nil && !a.model.Workspace.TerminalBusy {
			a.restartWorkspaceTerminal()
		}
	case view.PanelFiles:
		a.readWorkspaceDirectory("")
	case view.PanelPreview:
		if a.model.Workspace.Preview.Path != "" {
			a.openWorkspaceFile(a.model.Workspace.Preview.Path)
		}
	case view.PanelChanges, view.PanelRail:
		a.readWorkspaceChanges()
	}
}

func (a *App) readWorkspaceDirectory(path string) {
	if a.shuttingDown.Load() {
		return
	}
	w := &a.model.Workspace
	if w.Loading[path] {
		return
	}
	if w.Loading == nil {
		w.Loading = make(map[string]bool)
	}
	if len(w.Directories) >= 64 {
		w.FileError = "Too many open folders"
		return
	}
	w.Loading[path] = true
	w.FileError = ""
	root, generation := w.Root, a.workspace.project.generation
	go func() {
		entries, err := workspace.ReadDir(root, path)
		a.post(func() {
			if !a.currentWorkspace(generation) {
				return
			}
			delete(w.Loading, path)
			if err != nil {
				w.FileError = err.Error()
				return
			}
			if w.Directories == nil {
				w.Directories = make(map[string][]workspace.Entry)
			}
			w.Directories[path] = entries
		})
	}()
}

func (a *App) openWorkspaceFile(path string) {
	w := &a.model.Workspace
	if a.shuttingDown.Load() {
		return
	}
	if w.Open == nil {
		w.Open = make(map[view.WorkspacePanel]bool)
	}
	w.Open[view.PanelPreview] = true
	w.Active = view.PanelPreview
	generation, request := a.workspace.preview.begin(a.workspace.project.generation)
	root := w.Root
	w.PreviewBusy = true
	w.PreviewError = ""
	go func() {
		file, err := workspace.ReadFile(root, path)
		bitmap := attachmentThumbnail(file.Image)
		buffer := editor.NewBuffer(file.Text)
		buffer.SetLanguage(editor.LanguageFor(file.Path))
		code := &editor.State{Buffer: buffer, ReadOnly: true, NoScrollPastEnd: true}
		a.post(func() {
			if !a.currentWorkspace(generation) || !a.workspace.preview.current(generation, request) {
				return
			}
			w.PreviewBusy = false
			if err != nil {
				w.PreviewError = err.Error()
				return
			}
			if len(file.Image) > 0 && bitmap == nil {
				w.PreviewError = "Image cannot be previewed"
				return
			}
			w.Preview = file
			w.PreviewImage = bitmap
			w.PreviewCode = code
		})
	}()
}

func (a *App) readWorkspaceChanges() {
	w := &a.model.Workspace
	if w.ChangesBusy {
		return
	}
	w.ChangesBusy = true
	root, generation := w.Root, a.workspace.project.generation
	base := a.workspaceContext()
	go func() {
		ctx, cancel := context.WithTimeout(base, 10*time.Second)
		defer cancel()
		changes, err := workspace.ReadChanges(ctx, root)
		a.post(func() {
			if !a.currentWorkspace(generation) {
				return
			}
			w.ChangesBusy = false
			if err != nil {
				w.ChangesError = err.Error()
				return
			}
			w.ChangesError = ""
			w.Changes = changes
		})
	}()
}

func (a *App) readWorkspaceDiff(path string) {
	w := &a.model.Workspace
	found, untracked := false, false
	for _, file := range w.Changes.Files {
		if file.Path == path {
			found = true
			untracked = file.Status == "??"
			break
		}
	}
	if !found {
		return
	}
	generation, request := a.workspace.diff.begin(a.workspace.project.generation)
	root := w.Root
	w.DiffPath = path
	w.DiffBusy = true
	w.DiffError = ""
	base := a.workspaceContext()
	go func() {
		ctx, cancel := context.WithTimeout(base, 10*time.Second)
		defer cancel()
		var diff string
		var err error
		if untracked {
			var file workspace.File
			file, err = workspace.ReadFile(root, path)
			diff = file.Text
			if err == nil && len(file.Image) > 0 {
				err = errors.New("Binary file cannot be previewed")
			}
		} else {
			diff, err = workspace.ReadDiff(ctx, root, path)
		}
		buffer := editor.NewBuffer(diff)
		buffer.SetLanguage(editor.LanguageFor("working.diff"))
		code := &editor.State{Buffer: buffer, ReadOnly: true, NoScrollPastEnd: true}
		a.post(func() {
			if !a.currentWorkspace(generation) || !a.workspace.diff.current(generation, request) {
				return
			}
			w.DiffBusy = false
			if err != nil {
				w.DiffError = err.Error()
				return
			}
			w.Diff = diff
			w.DiffCode = code
		})
	}()
}

func (a *App) restartWorkspaceTerminal() {
	w := &a.model.Workspace
	if w.TerminalBusy || a.shuttingDown.Load() {
		return
	}
	if w.Terminal != nil {
		a.discardWorkspaceTerminal(w.Terminal)
		w.Terminal = nil
	}
	w.TerminalBusy = true
	w.TerminalError = ""
	generation, request := a.workspace.terminal.begin(a.workspace.project.generation)
	root := w.Root
	go func() {
		path, err := workspace.Resolve(root, "")
		var term *terminal.Terminal
		if err == nil {
			term, err = terminal.New(terminal.Options{Dir: path, Scrollback: 4 * 1024 * 1024, NoClipboardWrite: true})
		}
		if term != nil && !a.workspace.terminals.track(term, !a.shuttingDown.Load()) {
			return
		}
		a.post(func() {
			if !a.currentWorkspace(generation) || !a.workspace.terminal.current(generation, request) {
				if term != nil {
					a.discardWorkspaceTerminal(term)
				}
				return
			}
			w.TerminalBusy = false
			if err != nil {
				w.TerminalError = "Could not start terminal"
				return
			}
			w.Terminal = term
		})
	}()
}

func (a *App) discardWorkspaceTerminal(term *terminal.Terminal) {
	a.workspace.terminals.discard(term)
}

func (a *App) closeWorkspaceTerminals(wait bool) {
	a.workspace.terminals.closeAll(wait)
}
