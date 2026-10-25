package view

import (
	"strings"

	"dsh-mygo/internal/workspace"
	"github.com/egoist/mygo/plugins/terminal"
	mgo "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
)

type WorkspacePanel string

const (
	PanelRail     WorkspacePanel = "rail"
	PanelTerminal WorkspacePanel = "terminal"
	PanelChanges  WorkspacePanel = "changes"
	PanelPreview  WorkspacePanel = "preview"
	PanelFiles    WorkspacePanel = "files"
)

type WorkspaceState struct {
	FilesList, ChangesList                  mgo.ListState
	PreviewCode, DiffCode                   *editor.State
	Root                                    string
	Open                                    map[WorkspacePanel]bool
	Active, Maximized                       WorkspacePanel
	ChatPercent                             float32
	Splits                                  map[string]*float32
	Directories                             map[string][]workspace.Entry
	Expanded, Loading                       map[string]bool
	Filter, FileError                       string
	Preview                                 workspace.File
	PreviewImage                            *mgo.Bitmap
	PreviewBusy                             bool
	PreviewError, PreviewURL                string
	Changes                                 workspace.Changes
	ChangesBusy                             bool
	ChangesError, DiffPath, Diff, DiffError string
	DiffBusy                                bool
	Terminal                                *terminal.Terminal
	TerminalBusy                            bool
	TerminalError                           string
	Load                                    func(WorkspacePanel)
	ReadDirectory                           func(string)
	OpenFile                                func(string)
	ReadDiff                                func(string)
	RestartTerminal                         func()
}

func (model *Model) ToggleWorkspacePanel(panel WorkspacePanel) {
	w := &model.Workspace
	if w.Open == nil {
		w.Open = make(map[WorkspacePanel]bool)
	}
	w.Open[panel] = !w.Open[panel]
	if w.Open[panel] {
		w.Active = panel
		if len(model.workspacePanels()) > 2 && w.ChatPercent > 45 {
			w.ChatPercent = 40
		}
		if w.Load != nil {
			w.Load(panel)
		}
	} else if w.Maximized == panel {
		w.Maximized = ""
	}
}

func (model *Model) workspacePanels() []WorkspacePanel {
	var panels []WorkspacePanel
	for _, p := range []WorkspacePanel{PanelRail, PanelTerminal, PanelChanges, PanelPreview, PanelFiles} {
		if model.Workspace.Open[p] {
			panels = append(panels, p)
		}
	}
	return panels
}

func workspacePanelLabel(p WorkspacePanel) string {
	switch p {
	case PanelRail:
		return "Session rail"
	case PanelTerminal:
		return "Terminal"
	case PanelChanges:
		return "Changes"
	case PanelPreview:
		return "Preview"
	default:
		return "Files"
	}
}

func workspacePanelIcon(p WorkspacePanel) *mgo.SVG {
	switch p {
	case PanelRail:
		return iconRail
	case PanelTerminal:
		return iconTerminal
	case PanelChanges:
		return iconChanges
	case PanelPreview:
		return iconPreview
	default:
		return iconFolder
	}
}

func workspaceDock(c *mgo.Context, model *Model, panels []WorkspacePanel, narrow bool) {
	w := &model.Workspace
	mgo.Column(c.Key("workspace-dock")).Fill().MinWidth(0).Padding(space6).Gap(space6).Children(func() {
		if narrow {
			mgo.Row(c.Key("workspace-tabs")).FillWidth().Gap(space4).Children(func() {
				iconButton(c.Key("back-chat"), model.T("Back to conversation"), iconBack).OnClick(func() {
					for _, p := range panels {
						w.Open[p] = false
					}
					w.Maximized = ""
				})
				for _, p := range panels {
					iconButton(c.Key(string(p)), model.T(workspacePanelLabel(p)), workspacePanelIcon(p)).OnClick(func() { w.Active = p })
				}
			})
			active := panels[0]
			for _, p := range panels {
				if p == w.Active {
					active = p
				}
			}
			workspacePanel(c, model, active)
		} else if w.Maximized != "" && w.Open[w.Maximized] {
			workspacePanel(c, model, w.Maximized)
		} else {
			workspacePanelGroup(c, model, panels, len(panels) > 2, "root")
		}
	})
}

func workspacePanelGroup(c *mgo.Context, model *Model, panels []WorkspacePanel, horizontal bool, key string) {
	if len(panels) == 1 {
		workspacePanel(c, model, panels[0])
		return
	}
	w := &model.Workspace
	if w.Splits == nil {
		w.Splits = make(map[string]*float32)
	}
	if w.Splits[key] == nil {
		value := float32(50)
		w.Splits[key] = &value
	}
	middle := (len(panels) + 1) / 2
	// Split keeps its draggable divider; inset each side to create a 13 DIP
	// gutter (6 + divider + 6) between the rounded panel surfaces.
	first := func() {
		box := mgo.Column(c).Fill().MinWidth(0).MinHeight(0)
		if horizontal {
			box.Padding(0, space6, 0, 0)
		} else {
			box.Padding(0, 0, space6, 0)
		}
		box.Children(func() { workspacePanelGroup(c, model, panels[:middle], false, key+"a") })
	}
	second := func() {
		box := mgo.Column(c).Fill().MinWidth(0).MinHeight(0)
		if horizontal {
			box.Padding(0, 0, 0, space6)
		} else {
			box.Padding(space6, 0, 0, 0)
		}
		box.Children(func() { workspacePanelGroup(c, model, panels[middle:], false, key+"b") })
	}
	if horizontal {
		mgo.SplitPercent(c.Key("dock-split-"+key), w.Splits[key], first, second).Fill()
	} else {
		mgo.SplitVerticalPercent(c.Key("dock-split-"+key), w.Splits[key], first, second).Fill()
	}
}

func workspacePanel(c *mgo.Context, model *Model, p WorkspacePanel) {
	w := &model.Workspace
	tokens := tokensFor(c.Theme())
	mgo.Column(c.Key("workspace-panel-"+string(p))).Label(model.T("Workspace panel · ")+model.T(workspacePanelLabel(p))).Fill().MinWidth(0).MinHeight(0).Radius(radiusLg).Background(tokens.Surface).Border(1, tokens.Border).Children(func() {
		mgo.Row(c.Key("panel-header")).FillWidth().Height(40).Padding(space4, space8).Gap(space4).AlignItems(mgo.Center).Children(func() {
			mgo.Text(c, model.T(workspacePanelLabel(p))).FontSize(textCaption).SingleLine().Grow(1).MinWidth(0)
			if p == PanelTerminal && w.RestartTerminal != nil {
				iconButton(c.Key("restart-terminal"), model.T("Restart terminal"), iconRefresh).Disabled(w.TerminalBusy).OnClick(w.RestartTerminal)
			} else if w.Load != nil && (p != PanelPreview || w.Preview.Path != "") {
				iconButton(c.Key("refresh"), model.T("Refresh panel"), iconRefresh).OnClick(func() { w.Load(p) })
			}
			icon := iconMaximize
			label := "Maximize panel"
			if w.Maximized == p {
				icon = iconRestore
				label = "Restore panel"
			}
			iconButton(c.Key("maximize"), model.T(label), icon).OnClick(func() {
				if w.Maximized == p {
					w.Maximized = ""
				} else {
					w.Maximized = p
				}
			})
			iconButton(c.Key("close"), model.T("Close "+workspacePanelLabel(p)), iconClose).OnClick(func() { model.ToggleWorkspacePanel(p) })
		})
		switch p {
		case PanelRail:
			mgo.Scroll(c.Key("rail-scroll")).Grow(1).MinHeight(0).FillWidth().Padding(space8).Children(func() { detailsBody(c, model) })
		case PanelTerminal:
			if w.Terminal != nil {
				terminal.View(c.Key("native-terminal"), w.Terminal).Grow(1).MinHeight(0).FillWidth()
			} else {
				workspaceEmpty(c, model, w.TerminalBusy, w.TerminalError, "Start a terminal in this project")
			}
		case PanelFiles:
			workspaceFiles(c, model)
		case PanelChanges:
			workspaceChanges(c, model)
		case PanelPreview:
			workspacePreview(c, model)
		}
	})
}

func workspaceEmpty(c *mgo.Context, model *Model, busy bool, message, fallback string) {
	mgo.Column(c).Grow(1).MinHeight(0).FillWidth().Padding(space16).Gap(space8).AlignItems(mgo.Center).Justify(mgo.Center).Children(func() {
		if busy {
			mgo.Spinner(c).Size(18, 18)
		} else {
			if message == "" {
				message = fallback
			}
			mgo.Text(c, model.T(message)).FontSize(textCaption).TextAlign(mgo.Center).TextColor(c.Theme().TextMuted)
		}
	})
}

func workspaceFiles(c *mgo.Context, model *Model) {
	w := &model.Workspace
	mgo.SearchField(c.Key("file-filter"), &w.Filter).Label(model.T("Filter files")).Placeholder(model.T("Filter files")).FillWidth().Margin(space8)
	if w.FileError != "" {
		mgo.Text(c, model.T(w.FileError)).FontSize(textMicro).TextColor(c.Theme().Danger)
	}
	rows := visibleFiles(w, "", 0)
	mgo.List(c.Key("file-tree"), &w.FilesList, len(rows)).ItemKey(func(i int) any { return rows[i].entry.Path }).Grow(1).MinHeight(0).FillWidth().Rows(func(row mgo.ListRow) {
		entry, depth := rows[row.Index].entry, rows[row.Index].depth
		icon := iconFile
		if entry.Directory {
			icon = iconChevronRight
			if w.Expanded[entry.Path] {
				icon = iconChevron
			}
		}
		mgo.Button(c.Key("file-"+entry.Path), "").Label(entry.Name).Tooltip(entry.Path).FillWidth().Padding(space6, space8).Background(mgo.Transparent).Border(0, mgo.Transparent).Children(func() {
			mgo.Row(c).FillWidth().Gap(space6).Padding(0, 0, 0, float32(depth)*12).AlignItems(mgo.Center).Children(func() {
				mgo.Icon(c, icon).Size(14, 14)
				mgo.Text(c, entry.Name).FontSize(textCaption).SingleLine().Grow(1).MinWidth(0)
			})
		}).OnClick(func() {
			if entry.Directory {
				if w.Expanded == nil {
					w.Expanded = make(map[string]bool)
				}
				w.Expanded[entry.Path] = !w.Expanded[entry.Path]
				if w.Expanded[entry.Path] && w.ReadDirectory != nil {
					w.ReadDirectory(entry.Path)
				}
			} else if w.OpenFile != nil {
				w.OpenFile(entry.Path)
			}
		})
	})
}

type workspaceFileRow struct {
	entry workspace.Entry
	depth int
}

func visibleFiles(w *WorkspaceState, path string, depth int) []workspaceFileRow {
	if depth > 24 {
		return nil
	}
	var rows []workspaceFileRow
	for _, entry := range w.Directories[path] {
		if w.Filter != "" && !entry.Directory && !strings.Contains(strings.ToLower(entry.Name), strings.ToLower(w.Filter)) {
			continue
		}
		rows = append(rows, workspaceFileRow{entry, depth})
		if entry.Directory && w.Expanded[entry.Path] {
			rows = append(rows, visibleFiles(w, entry.Path, depth+1)...)
		}
	}
	return rows
}

func workspaceChanges(c *mgo.Context, model *Model) {
	w := &model.Workspace
	if w.ChangesBusy || w.ChangesError != "" {
		workspaceEmpty(c, model, w.ChangesBusy, w.ChangesError, "")
		return
	}
	if len(w.Changes.Files) == 0 {
		workspaceEmpty(c, model, false, "", "Working tree is clean")
		return
	}
	list := mgo.List(c.Key("changes-files"), &w.ChangesList, len(w.Changes.Files)).ItemKey(func(i int) any { return w.Changes.Files[i].Path }).FillWidth().MinHeight(0)
	if w.DiffPath == "" {
		list.Grow(1)
	} else {
		list.Height(140)
	}
	list.Rows(func(row mgo.ListRow) {
		file := w.Changes.Files[row.Index]
		mgo.Button(c.Key("change-"+file.Path), file.Status+"  "+file.Path).FillWidth().FontSize(textCaption).Background(mgo.Transparent).Border(0, mgo.Transparent).OnClick(func() {
			if w.ReadDiff != nil {
				w.ReadDiff(file.Path)
			}
		})
	})
	if w.DiffPath != "" {
		mgo.Text(c, w.DiffPath).FontSize(textMicro).SingleLine().Margin(space8)
		if w.DiffBusy || w.DiffError != "" {
			workspaceEmpty(c, model, w.DiffBusy, w.DiffError, "")
		} else if w.DiffCode != nil {
			editor.View(c.Key("working-diff"), w.DiffCode).Grow(1).MinHeight(0).FillWidth()
		}
	}
}

func workspacePreview(c *mgo.Context, model *Model) {
	w := &model.Workspace
	services := c.Services()
	mgo.Row(c).FillWidth().Padding(space8).Gap(space4).Children(func() {
		mgo.TextInput(c.Key("preview-url"), &w.PreviewURL).Label(model.T("Preview URL")).Placeholder("http://localhost:3000").Grow(1).MinWidth(0)
		iconButton(c.Key("open-url"), model.T("Open in browser"), iconExternal).Disabled(!safeMarkdownLink(w.PreviewURL)).OnClick(func() {
			if safeMarkdownLink(w.PreviewURL) {
				services.OpenURL(w.PreviewURL)
			}
		})
	})
	if w.PreviewBusy || w.PreviewError != "" {
		workspaceEmpty(c, model, w.PreviewBusy, w.PreviewError, "")
		return
	}
	if w.Preview.Path == "" {
		workspaceEmpty(c, model, false, "", "Select a file to preview, or open a development URL in your browser.")
		return
	}
	mgo.Text(c, w.Preview.Path).FontSize(textMicro).TextColor(c.Theme().TextMuted).Margin(space8)
	if w.PreviewImage == nil && !strings.HasSuffix(strings.ToLower(w.Preview.Path), ".md") && w.PreviewCode != nil {
		editor.View(c.Key("source-preview"), w.PreviewCode).Grow(1).MinHeight(0).FillWidth()
		return
	}
	mgo.Scroll(c.Key("file-preview")).Grow(1).MinHeight(0).FillWidth().Padding(space12).Children(func() {
		if w.PreviewImage != nil {
			mgo.Image(c, w.PreviewImage).FillWidth().Fit(mgo.Contain)
		} else if strings.HasSuffix(strings.ToLower(w.Preview.Path), ".md") {
			markdown.View(c.Key("preview-markdown"), w.Preview.Text, &model.Markdown).FillWidth()
		} else {
			if w.PreviewCode != nil {
				editor.View(c.Key("source-preview"), w.PreviewCode).Height(420).FillWidth()
			}
		}
	})
}
