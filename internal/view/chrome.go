package view

import (
	mgo "github.com/egoist/mygo/ui"
)

func overlayActive(model *Model) bool {
	return model.Runtime.SetupRequired || model.SettingsOpen || model.SessionsOpen || model.Inspection.Open ||
		model.ToolInspector != nil || model.RecoveryOpen || len(model.Approvals) > 0 || len(model.Questions) > 0
}

func sidebarVisible(c *mgo.Context, model *Model) bool {
	width, _ := c.Size()
	return width >= sidebarBreakpoint && model.CanHistory && !model.SessionsOpen && model.Shell.SidebarOpen
}

func titleBarHeight(c *mgo.Context) float32 {
	bar := c.TitleBar()
	if bar.Height > WindowTitleBarHeight {
		return bar.Height
	}
	return WindowTitleBarHeight
}

func activeSessionTitle(model *Model) string {
	for _, session := range model.Sessions {
		if session.ID != model.SessionID {
			continue
		}
		if session.Blank {
			return model.T("New conversation")
		}
		if session.Title != "" {
			return session.Title
		}
	}
	return model.T("Untitled conversation")
}

// titleBar draws the app's own bar under the Windows controls. MyGo keeps the
// system buttons and reports the room they take; everything here stays clear
// of that inset, and the row itself drags the window like a title bar.
func titleBar(c *mgo.Context, model *Model) {
	theme := c.Theme()
	tokens := tokensFor(theme)
	width, _ := c.Size()
	bar := c.TitleBar()

	row := mgo.Row(c.Key("title-bar")).FillWidth().Height(titleBarHeight(c)).Shrink(0).
		Background(tokens.Background).Padding(0, bar.Right+space8, 0, bar.Left+space8).
		Gap(space2).AlignItems(mgo.Center).DragWindow()
	row.Children(func() {
		if !sidebarVisible(c, model) {
			iconButton(c.Key("app-menu"), model.T("Menu"), iconMenu).Menu(func(menu *mgo.Menu) { titleBarMenu(menu, model, width) })
			if width >= sidebarBreakpoint && model.CanHistory {
				label := "Hide sidebar"
				if !model.Shell.SidebarOpen {
					label = "Show sidebar"
				}
				iconButton(c.Key("sidebar-toggle"), model.T(label), iconPanelLeft).OnClick(func() {
					model.Shell.SidebarOpen = !model.Shell.SidebarOpen
				})
			}
		}

		mgo.Column(c.Key("title-bar-title")).Grow(1).MinWidth(0).Padding(space4, space8).Gap(space2).
			DragWindow().Children(func() {
			mgo.Text(c.Key("title-bar-session"), activeSessionTitle(model)).Label(model.T("Conversation title") + " · " + activeSessionTitle(model)).FontSize(textLabel).SingleLine()
		})
		if !sidebarVisible(c, model) {
			mgo.Text(c.Key("phase"), model.T(phaseLabel(model.Phase))).FontSize(textMicro).TextColor(tokens.TextMuted)
		}

		for _, panel := range []WorkspacePanel{PanelRail, PanelTerminal, PanelChanges, PanelPreview, PanelFiles} {
			if panel == PanelTerminal {
				mgo.Box(c.Key("tool-group-separator")).Label(model.T("Workspace tools separator")).Width(1).Height(18).Margin(0, space4).Background(tokens.Border)
			}
			button := iconButton(c.Key("workspace-"+string(panel)), model.T("Toggle "+workspacePanelLabel(panel)), workspacePanelIcon(panel)).
				Disabled(model.RecoveryOpen || model.LocalState.Closing).OnClick(func() { model.ToggleWorkspacePanel(panel) })
			if model.Workspace.Open[panel] {
				button.Background(tokens.AccentSoft).TextColor(tokens.Accent)
				if panel == PanelRail {
					button.Background(tokens.SurfaceHover).TextColor(tokens.Text)
				}
				button.Tooltip(model.T("Hide " + workspacePanelLabel(panel)))
			} else {
				button.Tooltip(model.T("Show " + workspacePanelLabel(panel)))
			}
			if panel == PanelRail {
				button.Bind(&model.Workspace.RailButton)
			}
		}
		iconButton(c.Key("workspace-more"), model.T("More"), iconMoreVertical).
			Menu(func(menu *mgo.Menu) { titleBarMenu(menu, model, width) })

	})
}

func titleBarMenu(menu *mgo.Menu, model *Model, width float32) {
	item := menu.Item(model.T("New conversation")).Disabled(!model.CanChooseSession() || model.NewSession == nil)
	if mods, key := commandKey(model.Prefs.Value.NewSession); key != mgo.KeyUnknown {
		item.Shortcut(mods, key)
	}
	if item.Chosen() {
		model.NewSession()
	}
	if model.CanHistory && model.OpenSessions != nil {
		if item := menu.Item(model.T("Sessions")).Disabled(!model.CanRefreshSessions()); item.Chosen() {
			model.queueAction(model.OpenSessions)
		}
	}
	if width >= sidebarBreakpoint && model.CanHistory {
		label := "Hide sidebar"
		if !model.Shell.SidebarOpen {
			label = "Show sidebar"
		}
		if menu.Item(model.T(label)).Chosen() {
			model.Shell.SidebarOpen = !model.Shell.SidebarOpen
		}
	}
	menu.Separator()
	if model.Inspection.Available && model.Inspection.Show != nil {
		if item := menu.Item(model.T("Subagents")).Disabled(!model.CanInspectSubagents()); item.Chosen() {
			model.Inspection.Show()
		}
	}
	if model.CanOpenPreferences() {
		item := menu.Item(model.T("Settings"))
		if mods, key := commandKey(model.Prefs.Value.Preferences); key != mgo.KeyUnknown {
			item.Shortcut(mods, key)
		}
		if item.Chosen() {
			model.OpenPreferences()
		}
	} else if model.OpenSettings != nil && (model.CanModels || model.Permission.Available || model.CanConfigure) {
		if menu.Item(model.T("Settings")).Chosen() {
			model.OpenSettings()
		}
	}
	if menu.Item(model.T("About dsh-mygo")).Chosen() {
		model.OpenSettingsAt(SettingsSectionAbout)
	}
	if model.Quit != nil {
		menu.Separator()
		if menu.Item(model.T("Quit")).Chosen() {
			model.Quit()
		}
	}
}
