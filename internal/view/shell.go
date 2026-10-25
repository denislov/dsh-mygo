package view

import (
	"time"

	mgo "github.com/egoist/mygo/ui"
)

// ShellState contains device-only disclosure state, never session facts.
type ShellState struct {
	pendingAction func()
	SidebarOpen   bool
	ProgressOpen  bool
	FolderOpen    bool
	ContextOpen   bool
	TasksExpanded bool
}

func emptyConversation(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	_, height := c.Size()
	compact := height-titleBarHeight(c) < 560
	padding := space12
	if compact {
		padding = 0
	}
	mgo.Column(c.Key("empty-conversation")).FillWidth().AlignItems(mgo.Center).Gap(space12).Padding(padding).Children(func() {
		if !compact {
			mgo.Column(c.Key("empty-spark")).Size(44, 44).Radius(radiusPill).
				Background(tokens.AccentSoft).Justify(mgo.Center).AlignItems(mgo.Center).Children(func() {
				mgo.Icon(c, iconSpark).Size(21, 21).TextColor(tokens.Accent)
			})
			mgo.Text(c, model.T("Start a conversation")).FontSize(textDisplay).Bold().TextAlign(mgo.Center)
			mgo.Text(c, model.T("A little context. A clear next step.")).FontSize(textBody).
				TextColor(tokens.TextMuted).TextAlign(mgo.Center)
		}
		if !compact && model.Draft == "" && model.Send != nil && !model.SetupBlocked() {
			emptySuggestions(c, model)
		}
	})
}

func emptySuggestions(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	prompts := []string{"Summarize this project", "Suggest the next step", "Explain the codebase"}
	mgo.Row(c.Key("empty-suggestions")).Wrap().Justify(mgo.Center).Gap(space8).Padding(space8, 0, 0, 0).Children(func() {
		for _, prompt := range prompts {
			button := mgo.Button(c.Key("suggestion-"+prompt), "").Label(model.T(prompt)).Padding(space6, space12).
				Radius(radiusPill).Background(tokens.SurfaceRaised).Border(1, tokens.Border).Transition(quietTransition)
			button.Children(func() {
				mgo.Text(c, model.T(prompt)).FontSize(textCaption)
			})
			if button.Pressed() {
				button.Background(tokens.SurfacePressed)
			} else if button.Hovered() {
				button.Background(tokens.SurfaceHover)
			}
			if button.Clicked() {
				model.Draft = model.T(prompt)
				if model.DraftChanged != nil {
					model.DraftChanged()
				}
			}
		}
	})
}

// Layout transitions are kept off transcript rows so streaming never shifts
// the reading position. Native disclosures and chrome can animate independently.
var panelTransition = mgo.ElementTransition{Duration: 180 * time.Millisecond, Enter: &mgo.Motion{Y: 4}, Exit: &mgo.Motion{Y: 4}}
