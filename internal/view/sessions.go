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
	if model.Prefs.Open || model.Inspection.Open {
		return false
	}
	return model.CanHistory && model.Phase == PhaseIdle && !model.LocalState.Closing && !model.SessionBusy && !model.SessionPanel.Writing && !model.SettingsOpen && !model.SettingsBusy && !model.ModelUncertain && !model.ConfigurationUncertain && !model.Permission.Uncertain && len(model.Approvals) == 0 && len(model.Questions) == 0
}

func sessionSidebar(c *mgo.Context, model *Model) {
	mgo.Column(c.Key("session-sidebar")).Width(248).FillHeight().Padding(16, 12).Gap(14).Background(c.Theme().Surface).Children(func() {
		mgo.Row(c).Padding(4, 8).Gap(8).AlignItems(mgo.Center).Children(func() {
			mgo.Icon(c, iconSpark).Size(20, 20).TextColor(c.Theme().Accent)
			mgo.Text(c, "dsh-mygo").FontSize(16).Bold()
		})
		mgo.Button(c.Key("new"), "").Label(model.T("New conversation")).FillWidth().Padding(10, 12).
			Disabled(!model.CanChooseSession() || model.NewSession == nil).OnClick(model.NewSession).Children(func() {
			mgo.Icon(c, iconPlus).Size(16, 16)
			mgo.Text(c, model.T("New conversation"))
		})
		mgo.Text(c, model.T("Conversations")).Padding(2, 8).FontSize(11).Bold().TextColor(c.Theme().TextMuted)
		sessionPanel(c, model, false)
		mgo.Toolbar(c.Key("sidebar-footer"), func() {
			mgo.Button(c.Key("refresh"), model.T("Refresh sessions")).FontSize(12).Disabled(!model.CanRefreshSessions() || model.RefreshSessions == nil).OnClick(model.RefreshSessions)
		}).FillWidth()
	})
}

func sessionPanel(c *mgo.Context, model *Model, modal bool) {
	panel := &model.SessionPanel
	disabled := !model.CanChooseSession()
	mgo.SearchField(c.Key("session-search"), &panel.Search).Placeholder(model.T("Search titles")).Label(model.T("Search titles")).FillWidth()
	if panel.CanArchive {
		mgo.Checkbox(c.Key("archived-filter"), &panel.ShowArchived, model.T("Show archived"))
	}
	if model.SessionBusy || panel.Refreshing {
		label := "Loading sessions…"
		if model.SessionSwitching {
			label = "Loading session history…"
		}
		if panel.Writing {
			label = "Saving conversation…"
		}
		mgo.Text(c, model.T(label)).FontSize(12)
	}
	if model.SessionsError != "" {
		mgo.Text(c, model.T(model.SessionsError)).FontSize(12).TextColor(c.Theme().Danger)
	}
	if panel.ActiveArchived {
		mgo.Text(c, model.T("This conversation is archived. Restore it or choose another.")).FontSize(12)
	}
	sessions := model.FilteredSessions()
	if len(sessions) == 0 && !model.SessionBusy && !panel.Refreshing && model.SessionsError == "" {
		mgo.Text(c, model.T("No matching conversations.")).FontSize(12)
	}
	list := mgo.List(c.Key("session-list"), &model.SessionList, len(sessions)).FillWidth()
	if modal {
		list = list.Height(150)
	} else {
		list = list.Grow(1).MinHeight(60)
	}
	list.ItemKey(func(row int) any { return sessions[row].ID }).Rows(func(row mgo.ListRow) {
		session := sessions[row.Index]
		mgo.Column(c.Key(session.ID)).Gap(3).Padding(2, 0).Children(func() {
			label := model.sessionLabel(session)
			if session.ID == model.SessionID {
				label = model.T("Current · ") + label
			}
			mgo.Row(c).FillWidth().Gap(2).AlignItems(mgo.Center).Children(func() {
				bg := mgo.Transparent
				if session.ID == model.SessionID {
					bg = c.Theme().SurfaceHover
				}
				selectButton := mgo.Button(c.Key("select"), "").Label(label).Tooltip(sessionTooltip(model, session)).Grow(1).MinWidth(0).
					Padding(9, 10).Background(bg).Border(0, mgo.Transparent).Transition(quietTransition).
					Disabled(disabled || session.Archived || model.SwitchSession == nil).OnClick(func() { model.SwitchSession(session.ID) })
				if !disabled && !session.Archived && model.SwitchSession != nil {
					if selectButton.Pressed() {
						selectButton.Background(c.Theme().SurfacePressed)
					} else if selectButton.Hovered() {
						selectButton.Background(c.Theme().SurfaceHover)
					}
				}
				selectButton.Children(func() {
					mgo.Column(c).FillWidth().Gap(4).Children(func() {
						mgo.Text(c, model.sessionLabel(session)).FontSize(13).SingleLine()
						updated := session.UpdatedAt
						if updated == 0 {
							updated = session.CreatedAt
						}
						status := session.Status
						if session.Archived {
							status = "archived"
						}
						mgo.Text(c, time.UnixMilli(updated).Format("01-02 15:04")+" · "+model.T(status)).FontSize(10).TextColor(c.Theme().TextMuted)
						if session.MetadataIncomplete {
							mgo.Text(c, model.T("Open to rebuild metadata")).FontSize(10).SingleLine().TextColor(c.Theme().TextMuted)
						}
						if modal && session.CWD != "" {
							mgo.Text(c, session.CWD).FontSize(11).SingleLine().TextColor(c.Theme().TextMuted)
						}
					})
				})
				if panel.CanRename || panel.CanArchive {
					mgo.Toolbar(c.Key("session-menu"), func() {
						iconButton(c.Key("actions"), model.T("Conversation actions · ")+model.sessionLabel(session), iconMore).Disabled(disabled).
							Menu(func(menu *mgo.Menu) { sessionActions(menu, model, session) })
					}).Padding(0)
				}
			})
		})
	})
	if panel.EditingID != "" {
		mgo.Column(c.Key("rename-" + panel.EditingID)).Gap(6).Children(func() {
			mgo.Text(c, model.T("Rename conversation")).Bold().FontSize(12)
			mgo.TextInput(c.Key("title"), &panel.Title).Label(model.T("Conversation title")).FillWidth().Disabled(panel.Writing).Placeholder(model.T("Conversation title"))
			mgo.Row(c).Gap(6).Children(func() {
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
		mgo.Text(c, model.T(label)+"?").FontSize(12).Bold()
		mgo.Row(c).Gap(6).Children(func() {
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
	if session.MetadataCached {
		label += "\n" + model.T("Cached metadata")
	}
	return label
}

func (model *Model) ResetTranscript(items []projection.Item) {
	model.Items = items
	model.List = mgo.ListState{FollowEnd: true}
	model.List.Key = func(row int) any { return model.Items[row].ID }
	model.ToolCards = nil
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
		mgo.Scroll(c.Key("session-panel-scroll")).MaxHeight(max(160, height-180)).FillWidth().Children(func() { sessionPanel(c, model, true) })
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
