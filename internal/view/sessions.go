package view

import (
	"fmt"
	"time"

	"dsh-mygo/internal/projection"
	mgo "github.com/egoist/mygo/ui"
)

type SessionSummary struct {
	ID        string
	Title     string
	CWD       string
	CreatedAt int64
	Status    string
}

func (model *Model) CanChooseSession() bool {
	return model.CanHistory && model.Phase == PhaseIdle && !model.SessionBusy && !model.SettingsOpen && !model.SettingsBusy && !model.ModelUncertain && len(model.Approvals) == 0 && len(model.Questions) == 0
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

func sessionsModal(c *mgo.Context, model *Model) {
	if !model.CanHistory || !model.SessionsOpen || len(model.Approvals) > 0 || len(model.Questions) > 0 {
		return
	}
	disabled := !model.CanChooseSession()
	modal := mgo.Modal(c.Key("session-picker"), &model.SessionsOpen, func() {
		mgo.Text(c, "Conversations").FontSize(18).Bold()
		mgo.Text(c, fmt.Sprintf("%d sessions · current profile", len(model.Sessions))).FontSize(11).TextColor(c.Theme().TextMuted)
		mgo.Text(c, "Resume uses the launch model and stored directory.").FontSize(11).TextColor(c.Theme().TextMuted)
		if model.SessionBusy {
			label := "Loading sessions…"
			if model.SessionSwitching {
				label = "Loading session history…"
			}
			mgo.Text(c, label).FontSize(12)
		}
		if model.SessionsError != "" {
			mgo.Text(c, model.SessionsError).FontSize(12).TextColor(c.Theme().Danger)
		}
		if len(model.Sessions) == 0 && !model.SessionBusy && model.SessionsError == "" {
			mgo.Text(c, "No stored sessions. Create a new conversation.").FontSize(12)
		}
		mgo.List(c.Key("session-list"), &model.SessionList, len(model.Sessions)).Height(180).FillWidth().ItemKey(func(row int) any { return model.Sessions[row].ID }).Rows(func(row mgo.ListRow) {
			session := model.Sessions[row.Index]
			mgo.Column(c.Key(session.ID)).Gap(3).Padding(4, 0).Children(func() {
				label := sessionLabel(session)
				if session.ID == model.SessionID {
					label = "Current · " + label
				}
				mgo.Button(c.Key("select"), label).FillWidth().Disabled(disabled || model.SwitchSession == nil).OnClick(func() { model.SwitchSession(session.ID) })
				mgo.Text(c, time.UnixMilli(session.CreatedAt).Format("2006-01-02 15:04")+" · "+session.Status).FontSize(11).TextColor(c.Theme().TextMuted)
				if session.CWD != "" {
					directory := []rune(session.CWD)
					if len(directory) > 70 {
						directory = append(directory[:70], '…')
					}
					mgo.Text(c, string(directory)).FontSize(11).TextColor(c.Theme().TextMuted)
				}
			})
		})
		mgo.Row(c).Gap(8).Children(func() {
			mgo.Button(c.Key("close-sessions"), "Close").Disabled(model.SessionSwitching || model.CloseSessions == nil).OnClick(func() { model.CloseSessions() })
			mgo.Spacer(c)
			mgo.Button(c.Key("refresh-sessions"), "Refresh").Disabled(disabled || model.RefreshSessions == nil).OnClick(func() { model.RefreshSessions() })
			mgo.PrimaryButton(c.Key("new-session"), "New conversation").Disabled(disabled || model.NewSession == nil).OnClick(func() { model.NewSession() })
		})
	})
	if modal.Dismissed() {
		if model.SessionSwitching {
			model.SessionsOpen = true
		} else if model.CloseSessions != nil {
			model.CloseSessions()
		}
	}
}
