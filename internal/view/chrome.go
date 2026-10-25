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
		if session.ID == model.SessionID && session.Title != "" {
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
		Background(tokens.Sidebar).Padding(0, bar.Right+space8, 0, bar.Left+space8).
		BorderWidth(0, 0, 1, 0).BorderColor(tokens.Border).
		Gap(space2).AlignItems(mgo.Center).DragWindow()
	row.Children(func() {
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
		if width >= 760 && model.NewSession != nil {
			iconButton(c.Key("new-chat"), model.T("New chat"), iconPlus).
				Disabled(!model.CanChooseSession()).OnClick(model.NewSession)
		}

		mgo.Column(c.Key("title-bar-title")).Grow(1).MinWidth(0).Padding(space4, space8).Gap(space2).
			DragWindow().Children(func() {
			mgo.Text(c.Key("title-bar-session"), activeSessionTitle(model)).FontSize(textLabel).Bold().SingleLine()
			mgo.Row(c).Gap(space6).AlignItems(mgo.Center).Children(func() {
				if model.Phase == PhaseRunning || model.Phase == PhaseStarting || model.Phase == PhaseResyncing {
					mgo.Spinner(c.Key("activity-spinner")).Size(10, 10)
				} else {
					mgo.Box(c.Key("activity-dot")).Size(5, 5).Radius(3).Background(phaseColor(model.Phase, theme)).Transition(quietTransition)
				}
				mgo.Text(c.Key("phase"), model.T(phaseLabel(model.Phase))).FontSize(textMicro).
					TextColor(tokens.TextMuted).ContentTransition(mgo.ContentTransition{})
			})
		})

		if model.CanConfigure && model.Setup.Connect != nil {
			iconButton(c.Key("connection"), model.T("Connect DeepSeek"), iconKey).
				Disabled(!model.CanEditSettings()).OnClick(model.Setup.Connect)
		}
		if model.Inspection.Available && width >= 720 {
			iconButton(c.Key("subagents"), model.T("Subagents"), iconAgents).
				Disabled(!model.CanInspectSubagents()).OnClick(model.Inspection.Show)
		}
		if model.CanHistory && model.OpenSessions != nil {
			iconButton(c.Key("sessions"), model.T("Sessions"), iconSessions).
				Disabled(!model.CanRefreshSessions()).OnClick(model.OpenSessions)
		}
		iconButton(c.Key("ui-preferences"), model.T("Preferences"), iconSettings).
			Disabled(!model.CanOpenPreferences()).OnClick(model.OpenPreferences)
		anchor := iconButton(c.Key("details"), model.T("Conversation details"), iconInfo).Disabled(model.RecoveryOpen)
		if anchor.Clicked() {
			if width >= detailsBreakpoint {
				model.Shell.DetailsOpen = !model.Shell.DetailsOpen
			} else {
				model.Shell.DetailsPopoverOpen = !model.Shell.DetailsPopoverOpen
			}
		}
		if width >= detailsBreakpoint {
			model.Shell.DetailsPopoverOpen = false
		} else if !overlayActive(model) {
			mgo.Popover(c.Key("conversation-details"), anchor, &model.Shell.DetailsPopoverOpen, func() {
				conversationDetails(c, model)
			})
		}
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
			model.OpenSessions()
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
	if model.CanConfigure && model.Setup.Connect != nil {
		if item := menu.Item(model.T("Connect DeepSeek")).Disabled(!model.CanEditSettings()); item.Chosen() {
			model.Setup.Connect()
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
