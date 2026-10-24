package view

import (
	"time"

	mgo "github.com/egoist/mygo/ui"
)

// ShellState contains device-only disclosure state, never session facts.
type ShellState struct {
	DetailsOpen   bool
	TasksExpanded bool
}

func header(c *mgo.Context, model *Model) {
	theme := c.Theme()
	mgo.Row(c.Key("conversation-header")).Padding(10, 20).Gap(12).AlignItems(mgo.Center).Children(func() {
		mgo.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			title := model.T("Untitled conversation")
			for _, session := range model.Sessions {
				if session.ID == model.SessionID && session.Title != "" {
					title = session.Title
					break
				}
			}
			mgo.Text(c, title).FontSize(15).Bold().SingleLine()
			mgo.Row(c).Gap(6).AlignItems(mgo.Center).Children(func() {
				if model.Phase == PhaseRunning || model.Phase == PhaseStarting || model.Phase == PhaseResyncing {
					mgo.Spinner(c.Key("activity-spinner")).Size(10, 10)
				} else {
					mgo.Box(c.Key("activity-dot")).Size(5, 5).Radius(3).Background(phaseColor(model.Phase, theme)).Transition(quietTransition)
				}
				mgo.Text(c.Key("phase"), model.T(phaseLabel(model.Phase))).FontSize(11).TextColor(theme.TextMuted).
					ContentTransition(mgo.ContentTransition{})
			})
		})
		mgo.Toolbar(c.Key("header-actions"), func() {
			if model.CanConfigure && model.Setup.Connect != nil {
				iconButton(c.Key("connection"), model.T("Connect DeepSeek"), iconSettings).Disabled(!model.CanEditSettings()).OnClick(model.Setup.Connect)
			}
			if model.Inspection.Available {
				iconButton(c.Key("subagents"), model.T("Subagents"), iconAgents).Disabled(!model.CanInspectSubagents()).OnClick(model.Inspection.Show)
			}
			if model.CanHistory && model.OpenSessions != nil {
				iconButton(c.Key("sessions"), model.T("Sessions"), iconSessions).Disabled(!model.CanRefreshSessions()).OnClick(model.OpenSessions)
			}
			iconButton(c.Key("ui-preferences"), model.T("Preferences"), iconSettings).Disabled(!model.CanOpenPreferences()).OnClick(model.OpenPreferences)
			anchor := iconButton(c.Key("details"), model.T("Conversation details"), iconInfo).Disabled(model.RecoveryOpen)
			if anchor.Clicked() {
				model.Shell.DetailsOpen = !model.Shell.DetailsOpen
			}
			if !model.SettingsOpen && !model.Prefs.Open && !model.SessionsOpen && len(model.Approvals) == 0 && len(model.Questions) == 0 && !model.RecoveryOpen {
				mgo.Popover(c.Key("conversation-details"), anchor, &model.Shell.DetailsOpen, func() { conversationDetails(c, model) })
			}
		}).Label(model.T("Conversation actions"))
	})
}

func conversationDetails(c *mgo.Context, model *Model) {
	_, height := c.Size()
	mgo.Column(c).Width(340).MaxWidth(360).Padding(8).Gap(12).Children(func() {
		mgo.Text(c, model.T("Conversation details")).FontSize(15).Bold()
		mgo.Scroll(c.Key("details-scroll")).MaxHeight(max(120, min(360, height-160))).FillWidth().Children(func() {
			mgo.Column(c).FillWidth().Gap(10).Selectable().Children(func() {
				detailField(c, model.T("Model"), model.ModelLabel)
				detailField(c, model.T("Working directory"), model.SessionCWD)
				detailField(c, model.T("Session ID"), model.SessionID)
				detailField(c, model.T("Runtime"), model.Detail)
				if model.Permission.Available {
					label := model.T("Permissions: refresh Settings")
					if model.Permission.Ready {
						label = model.T("Permissions: ") + model.Permission.Snapshot.Current + " · " + model.Permission.Snapshot.Sandbox + " / " + model.Permission.Snapshot.Approval
					}
					mgo.Text(c, label).FontSize(12)
				}
				for _, label := range []string{usageContextLabel(model), depthLabel(model), subagentLabel(model)} {
					if label != "" {
						mgo.Text(c, label).FontSize(12).LineHeight(1.4)
					}
				}
			})
		})
	})
}

func detailField(c *mgo.Context, title, value string) {
	if value == "" {
		return
	}
	mgo.Column(c).Gap(3).Children(func() {
		mgo.Text(c, title).FontSize(11).TextColor(c.Theme().TextMuted)
		mgo.Text(c, value).FontSize(12)
	})
}

func emptyConversation(c *mgo.Context, model *Model) {
	theme := c.Theme()
	_, height := c.Size()
	compact := height < 560 && model.Setup.ChooseFolder != nil
	padding := float32(12)
	if compact {
		padding = 0
	}
	mgo.Column(c.Key("empty-conversation")).FillWidth().AlignItems(mgo.Center).Gap(10).Padding(padding).Children(func() {
		if !compact {
			mgo.Icon(c, iconSpark).Size(24, 24).TextColor(theme.Accent)
			mgo.Text(c, model.T("Start a conversation")).FontSize(24).Bold()
		}
		if model.Setup.ChooseFolder != nil {
			firstUseControls(c, model)
		} else {
			mgo.Text(c, model.T("A little context. A clear next step.")).FontSize(14).TextColor(theme.TextMuted)
		}
	})
}

// Layout transitions are kept off transcript rows so streaming never shifts
// the reading position. Native disclosures and chrome can animate independently.
var panelTransition = mgo.ElementTransition{Duration: 180 * time.Millisecond, Enter: &mgo.Motion{Y: 4}, Exit: &mgo.Motion{Y: 4}}
