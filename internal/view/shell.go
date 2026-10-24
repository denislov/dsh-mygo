package view

import (
	"time"

	mgo "github.com/egoist/mygo/ui"
)

// ShellState contains device-only disclosure state, never session facts.
type ShellState struct {
	SidebarOpen        bool
	DetailsOpen        bool
	DetailsPopoverOpen bool
	TasksExpanded      bool
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
