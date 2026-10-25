package view

import (
	"fmt"
	"strings"
	"time"

	"dsh-mygo/internal/projection"
	mgo "github.com/egoist/mygo/ui"
)

type SessionSummary struct {
	MetadataIncomplete, MetadataCached bool
	ID                                 string
	Title                              string
	CWD                                string
	CreatedAt                          int64
	UpdatedAt                          int64
	Archived                           bool
	Status                             string
}

// SessionPanelState groups transient navigation and management form state.
type SessionPanelState struct {
	CanRename, CanArchive                          bool
	Search                                         string
	ShowArchived                                   bool
	Refreshing, Writing, Uncertain, ActiveArchived bool
	EditingID, Title                               string
	ArchiveID                                      string
	ArchiveValue                                   bool
	OpenRename                                     func(string)
	SaveRename, CancelEdit                         func()
	SetArchived                                    func(string, bool)
}

func (panel *SessionPanelState) BlocksInput() bool {
	return panel.Writing || panel.Uncertain || panel.ActiveArchived || panel.EditingID != "" || panel.ArchiveID != ""
}

func (model *Model) ActiveSessionLabel() string {
	for _, session := range model.Sessions {
		if session.ID == model.SessionID {
			return model.sessionLabel(session)
		}
	}
	return model.sessionLabel(SessionSummary{ID: model.SessionID})
}

func (model *Model) FilteredSessions() []SessionSummary {
	query := strings.ToLower(strings.TrimSpace(model.SessionPanel.Search))
	var sessions []SessionSummary
	for _, session := range model.Sessions {
		if session.Archived && !model.SessionPanel.ShowArchived {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(model.sessionLabel(session)), query) {
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions
}

func (model *Model) CanChooseSession() bool {
	return model.CanRefreshSessions() && !model.SessionPanel.Uncertain && model.SessionPanel.EditingID == "" && model.SessionPanel.ArchiveID == ""
}

func (model *Model) CanRefreshSessions() bool {
	if model.Runtime.SetupRequired || model.Inspection.Open || model.Setup.Picking {
		return false
	}
	return model.CanHistory && model.Phase == PhaseIdle && !model.LocalState.Closing && !model.SessionBusy && !model.SessionPanel.Writing && !model.SettingsOpen && !model.SettingsBusy && !model.ModelUncertain && !model.ConfigurationUncertain && !model.Permission.Uncertain && len(model.Approvals) == 0 && len(model.Questions) == 0
}

func sessionSidebar(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	mgo.Column(c.Key("session-sidebar")).Width(sidebarWidth).FillHeight().Shrink(0).Background(tokens.Sidebar).Children(func() {
		mgo.Row(c.Key("sidebar-header")).FillWidth().Height(titleBarHeight(c)).Padding(0, space8).Gap(space4).AlignItems(mgo.Center).DragWindow().Children(func() {
			width, _ := c.Size()
			iconButton(c.Key("sidebar-menu"), model.T("Menu"), iconMenu).Background(mgo.Transparent).
				Menu(func(menu *mgo.Menu) { titleBarMenu(menu, model, width) })
			iconButton(c.Key("sidebar-toggle"), model.T("Hide sidebar"), iconPanelLeft).Background(mgo.Transparent).
				OnClick(func() { model.Shell.SidebarOpen = false })
			mgo.Spacer(c)
			mgo.Text(c, "dsh-mygo").FontSize(textMicro).TextColor(tokens.TextFaint).DragWindow()
		})
		mgo.Column(c).Padding(space8, space8, space4, space8).Children(func() {
			button := mgo.Button(c.Key("new"), "").Label(model.T("New conversation")).FillWidth().
				Justify(mgo.Start).
				Padding(space8, space12).Radius(radiusMd).Background(mgo.Transparent).Border(0, mgo.Transparent).
				Transition(quietTransition).Disabled(!model.CanChooseSession() || model.NewSession == nil).OnClick(model.NewSession)
			button.Children(func() {
				mgo.Row(c).Gap(space8).AlignItems(mgo.Center).Children(func() {
					mgo.Icon(c, iconPlus).Size(16, 16).TextColor(tokens.Accent)
					mgo.Text(c, model.T("New conversation")).FontSize(textLabel)
				})
			})
			if button.Pressed() {
				button.Background(tokens.SurfacePressed)
			} else if button.Hovered() {
				button.Background(tokens.SurfaceHover)
			}
		})
		mgo.Row(c.Key("sidebar-search")).Padding(space4, space8).Gap(space4).AlignItems(mgo.Center).Children(func() {
			mgo.SearchField(c.Key("session-search"), &model.SessionPanel.Search).
				Placeholder(model.T("Search titles")).Label(model.T("Search titles")).FillWidth()
			if model.SessionPanel.CanArchive {
				iconButton(c.Key("conversation-filter"), model.T("Filter conversations"), iconFilter).
					Menu(func(menu *mgo.Menu) { sessionFilterMenu(menu, model) })
			}
			iconButton(c.Key("refresh"), model.T("Refresh sessions"), iconRefresh).
				Disabled(!model.CanRefreshSessions() || model.RefreshSessions == nil).OnClick(model.RefreshSessions)
		})
		mgo.Row(c.Key("sidebar-section")).Padding(space12, space12, space4, space12).AlignItems(mgo.Center).Children(func() {
			sectionLabel(c, model.T("Conversations"))
		})
		sessionPanel(c, model, false)
		mgo.Row(c.Key("sidebar-footer")).Padding(space8, space12, space12, space12).Gap(space8).AlignItems(mgo.Center).
			BorderWidth(1, 0, 0, 0).BorderColor(tokens.Border).Children(func() {
			mgo.Box(c.Key("sidebar-status-dot")).Size(6, 6).Radius(3).Background(phaseColor(model.Phase, c.Theme()))
			mgo.Text(c, model.T(phaseLabel(model.Phase))).FontSize(textMicro).TextColor(tokens.TextMuted).SingleLine()
			mgo.Spacer(c)
			iconButton(c.Key("open-settings"), model.T("Open settings"), iconSettings).
				Disabled(!model.CanOpenPreferences()).OnClick(model.OpenPreferences)
		})
	})
}

func sessionFilterMenu(menu *mgo.Menu, model *Model) {
	panel := &model.SessionPanel
	if menu.Item(model.T("Show archived")).Checked(panel.ShowArchived).Chosen() {
		panel.ShowArchived = !panel.ShowArchived
	}
}

func sessionSearch(c *mgo.Context, model *Model) {
	if model.SessionPanel.CanArchive {
		mgo.Row(c.Key("session-filter")).Gap(space8).AlignItems(mgo.Center).Children(func() {
			mgo.Checkbox(c.Key("archived-filter"), &model.SessionPanel.ShowArchived, model.T("Show archived"))
		})
	}
}

func sessionPanel(c *mgo.Context, model *Model, modal bool) {
	panel := &model.SessionPanel
	tokens := tokensFor(c.Theme())
	disabled := !model.CanChooseSession()
	if modal && (model.SessionBusy || panel.Refreshing) {
		label := "Loading sessions…"
		if model.SessionSwitching {
			label = "Loading session history…"
		}
		if panel.Writing {
			label = "Saving conversation…"
		}
		mgo.Text(c, model.T(label)).FontSize(textCaption).TextColor(tokens.TextMuted)
	}
	if model.SessionsError != "" {
		mgo.Text(c, model.T(model.SessionsError)).FontSize(textCaption).TextColor(tokens.Danger)
	}
	if panel.ActiveArchived {
		mgo.Text(c, model.T("This conversation is archived. Restore it or choose another.")).FontSize(textCaption)
	}
	entries := model.projectSessionEntries()
	if len(entries) == 0 && !model.SessionBusy && !panel.Refreshing && model.SessionsError == "" {
		mgo.Text(c, model.T("No matching conversations.")).FontSize(textCaption).TextColor(tokens.TextMuted)
	}
	model.SessionList.Key = func(row int) any { return entries[row].Key }
	model.SessionList.Header = func(row int) bool { return entries[row].Header }
	model.SessionList.Label = func(row int) string {
		if entries[row].Header {
			if entries[row].Project == "" {
				return model.T("Other conversations")
			}
			return entries[row].Project
		}
		return model.sessionLabel(entries[row].Session)
	}
	list := mgo.List(c.Key("session-list"), &model.SessionList, len(entries)).FillWidth()
	if modal {
		list = list.Height(150)
	} else {
		list = list.Grow(1).MinHeight(60)
	}
	list.Rows(func(row mgo.ListRow) {
		entry := entries[row.Index]
		if entry.Header {
			background := tokens.Sidebar
			if modal {
				background = c.Theme().Background
			}
			mgo.Row(c.Key(entry.Key)).FillWidth().Background(background).Padding(space12, space12, space4, space12).Gap(space4).AlignItems(mgo.Center).Children(func() {
				label := projectName(entry.Project)
				if label == "" {
					label = model.T("Other conversations")
				}
				mgo.Text(c, label).FontSize(textCaption).TextColor(tokens.TextMuted).SingleLine().Grow(1).Tooltip(entry.Project)
				if entry.Project != "" && model.NewProjectSession != nil {
					iconButton(c.Key("new-project"), model.T("New conversation in project")+" · "+entry.Project, iconPlus).
						Size(24, 24).Padding(4).Background(mgo.Transparent).Disabled(disabled).
						OnClick(func() { model.NewProjectSession(entry.Project) })
				}
			})
			return
		}
		session := entry.Session
		current := session.ID == model.SessionID
		mgo.Column(c.Key(session.ID)).Gap(space2).Padding(space2, 0).Children(func() {
			label := model.sessionLabel(session)
			if current {
				label = model.T("Current · ") + label
			}
			row := mgo.Row(c).FillWidth().Gap(0).AlignItems(mgo.Center).Radius(radiusSm)
			if current {
				row.Background(tokens.SurfaceHover)
			}
			row.Children(func() {
				background := mgo.Transparent
				if current {
					background = tokens.SurfaceHover
				}
				selectButton := mgo.Button(c.Key("select"), "").Label(label).Tooltip(sessionTooltip(model, session)).Grow(1).MinWidth(0).
					Padding(space8, space12).Background(background).Border(0, mgo.Transparent).
					Radius(radiusMd).Transition(quietTransition).Disabled(disabled || session.Archived || model.SwitchSession == nil).
					OnClick(func() { model.SwitchSession(session.ID) })
				if !disabled && !session.Archived && model.SwitchSession != nil {
					if selectButton.Pressed() {
						selectButton.Background(tokens.SurfacePressed)
					} else if selectButton.Hovered() && !current {
						selectButton.Background(tokens.SurfaceHover)
					}
				}
				selectButton.Children(func() {
					mgo.Column(c).FillWidth().Gap(space2).Children(func() {
						mgo.Text(c, model.sessionLabel(session)).FontSize(textLabel).SingleLine().TextColor(tokens.Text)
						updated := session.UpdatedAt
						if updated == 0 {
							updated = session.CreatedAt
						}
						status := session.Status
						if session.Archived {
							status = "archived"
						}
						meta := relativeTimeLabel(model, time.Now(), updated)
						if status != "" {
							if meta != "" {
								meta += " · "
							}
							meta += model.T(status)
						}
						if modal && meta != "" {
							mgo.Text(c, meta).FontSize(textMicro).TextColor(tokens.TextMuted).SingleLine()
						}
						if modal && session.MetadataIncomplete {
							mgo.Text(c, model.T("Open to rebuild metadata")).FontSize(textMicro).SingleLine().TextColor(tokens.TextMuted)
						}
						if modal && session.CWD != "" {
							mgo.Text(c, session.CWD).FontSize(textCaption).SingleLine().TextColor(tokens.TextMuted)
						}
					})
				})
				if panel.CanRename || panel.CanArchive {
					opacity := float32(0)
					if row.Hovered() || row.FocusWithin() {
						opacity = 1
					}
					iconButton(c.Key("actions"), model.T("Conversation actions · ")+model.sessionLabel(session), iconMore).
						Size(26, 26).Padding(4).Background(mgo.Transparent).Border(0, mgo.Transparent).Disabled(disabled).Opacity(opacity).
						Menu(func(menu *mgo.Menu) { sessionActions(menu, model, session) })
				}
			})
		})
	})
	if panel.EditingID != "" {
		mgo.Column(c.Key("rename-" + panel.EditingID)).Gap(space6).Children(func() {
			mgo.Text(c, model.T("Rename conversation")).Bold().FontSize(textCaption)
			mgo.TextInput(c.Key("title"), &panel.Title).Label(model.T("Conversation title")).FillWidth().Disabled(panel.Writing).Placeholder(model.T("Conversation title"))
			mgo.Row(c).Gap(space6).Children(func() {
				mgo.Button(c, model.T("Cancel rename")).Disabled(panel.Writing || panel.CancelEdit == nil).OnClick(panel.CancelEdit)
				mgo.PrimaryButton(c, model.T("Save title")).Disabled(panel.Writing || panel.Uncertain || strings.TrimSpace(panel.Title) == "" || panel.SaveRename == nil).OnClick(panel.SaveRename)
			})
		})
	}
	if panel.ArchiveID != "" {
		label := "Archive conversation"
		if !panel.ArchiveValue {
			label = "Restore conversation"
		}
		mgo.Text(c, model.T(label)+"?").FontSize(textCaption).Bold()
		mgo.Row(c).Gap(space6).Children(func() {
			mgo.Button(c, model.T("Cancel archive")).Disabled(panel.Writing).OnClick(func() { panel.ArchiveID = "" })
			mgo.PrimaryButton(c, model.T(label)).Disabled(panel.Writing || panel.Uncertain || panel.SetArchived == nil).OnClick(func() { panel.SetArchived(panel.ArchiveID, panel.ArchiveValue) })
		})
	}
}

func sessionActions(menu *mgo.Menu, model *Model, session SessionSummary) {
	panel := &model.SessionPanel
	disabled := !model.CanChooseSession()
	if panel.CanRename && !session.Archived {
		if menu.Item(model.T("Rename")).Disabled(disabled || panel.OpenRename == nil).Chosen() {
			panel.OpenRename(session.ID)
		}
	}
	if panel.CanArchive {
		label := "Archive"
		if session.Archived {
			label = "Restore"
		}
		if menu.Item(model.T(label)).Disabled(disabled || panel.SetArchived == nil).Chosen() {
			panel.ArchiveID, panel.ArchiveValue = session.ID, !session.Archived
		}
	}
}

func sessionTooltip(model *Model, session SessionSummary) string {
	label := model.sessionLabel(session)
	if session.CWD != "" {
		label += "\n" + session.CWD
	}
	status := session.Status
	if session.Archived {
		status = "archived"
	}
	if status != "" {
		label += "\n" + model.T(status)
	}
	if session.MetadataCached {
		label += "\n" + model.T("Cached metadata")
	}
	if session.MetadataIncomplete {
		label += "\n" + model.T("Open to rebuild metadata")
	}
	return label
}

func (model *Model) ResetTranscript(items []projection.Item) {
	model.Items = items
	model.List = mgo.ListState{FollowEnd: true}
	model.History.scroll = mgo.ScrollState{}
	model.History.autoStart = 0
	model.History.observedStart = 0
	model.ToolCards = nil
	model.Activity = nil
	model.ToolInspector = nil
	model.CopiedMessageID = ""
	model.CopiedMessageText = ""
}

func sessionLabel(session SessionSummary) string {
	if session.Title != "" {
		return session.Title
	}
	identity := session.ID
	if len(identity) > 12 {
		identity = identity[:12]
	}
	return "Session " + identity
}

func (model *Model) sessionLabel(session SessionSummary) string {
	label := sessionLabel(session)
	if session.Title == "" {
		return model.T("Session ") + strings.TrimPrefix(label, "Session ")
	}
	return label
}

func sessionsModal(c *mgo.Context, model *Model) {
	if !model.CanHistory || !model.SessionsOpen || len(model.Approvals) > 0 || len(model.Questions) > 0 {
		return
	}
	disabled := !model.CanChooseSession()
	modal := mgo.Modal(c.Key("session-picker"), &model.SessionsOpen, func() {
		mgo.Text(c, model.T("Conversations")).FontSize(18).Bold()
		mgo.Text(c, fmt.Sprintf(model.T("%d sessions · current profile"), len(model.Sessions))).FontSize(11).TextColor(c.Theme().TextMuted)
		mgo.Text(c, model.T("Resume uses the launch model and stored directory.")).FontSize(11).TextColor(c.Theme().TextMuted)

		_, height := c.Size()
		mgo.Scroll(c.Key("session-panel-scroll")).MaxHeight(max(160, height-180)).FillWidth().Children(func() {
			mgo.SearchField(c.Key("session-search"), &model.SessionPanel.Search).
				Placeholder(model.T("Search titles")).Label(model.T("Search titles")).FillWidth()
			sessionSearch(c, model)
			sessionPanel(c, model, true)
		})
		mgo.Row(c).Gap(8).Children(func() {
			mgo.Button(c.Key("close-sessions"), model.T("Close")).Disabled(model.SessionSwitching || model.SessionPanel.Writing || model.CloseSessions == nil).OnClick(func() { model.CloseSessions() })
			mgo.Spacer(c)
			mgo.Button(c.Key("refresh-sessions"), model.T("Refresh")).Disabled(!model.CanRefreshSessions() || model.RefreshSessions == nil).OnClick(func() { model.RefreshSessions() })
			mgo.PrimaryButton(c.Key("new-session"), model.T("New conversation")).Disabled(disabled || model.NewSession == nil).OnClick(func() { model.NewSession() })
		})
	})
	if modal.Dismissed() {
		if model.SessionSwitching || model.SessionPanel.Writing {
			model.SessionsOpen = true
		} else if model.CloseSessions != nil {
			model.CloseSessions()
		}
	}
}
