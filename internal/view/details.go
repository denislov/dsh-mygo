package view

import (
	"fmt"

	mgo "github.com/egoist/mygo/ui"
)

func detailsPaneVisible(c *mgo.Context, model *Model) bool {
	width, _ := c.Size()
	return width >= detailsBreakpoint && model.Shell.DetailsOpen
}

// detailsPane is the wide-window counterpart of the details Popover. Both
// render the same read-only body so diagnostics never diverge between layouts.
func detailsPane(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	mgo.Column(c.Key("details-pane")).Width(detailsWidth).FillHeight().Shrink(0).
		Background(tokens.Sidebar).BorderWidth(0, 0, 0, 1).BorderColor(tokens.Border).Children(func() {
		mgo.Row(c.Key("details-title")).Padding(space16, space16, space8, space16).AlignItems(mgo.Center).Children(func() {
			mgo.Text(c, model.T("Details")).FontSize(textTitle).Bold()
		})
		mgo.Scroll(c.Key("details-scroll")).Grow(1).FillWidth().
			Padding(space4, space12, space16, space12).Children(func() {
			detailsBody(c, model)
		})
	})
}

func detailsBody(c *mgo.Context, model *Model) {
	mgo.Column(c).FillWidth().Gap(space12).Children(func() {
		detailSection(c, "progress", model.T("Progress"), iconActivity, &model.Shell.ProgressOpen, func() {
			progressDetails(c, model)
		})
		detailSection(c, "folder", model.T("Working folder"), iconFolder, &model.Shell.FolderOpen, func() {
			folderDetails(c, model)
		})
		detailSection(c, "context", model.T("Context"), iconInfo, &model.Shell.ContextOpen, func() {
			contextDetails(c, model)
		})
	})
}

func detailSection(c *mgo.Context, key, title string, icon *mgo.SVG, open *bool, body func()) {
	tokens := tokensFor(c.Theme())
	card := mgo.Column(c.Key("details-"+key)).FillWidth().Padding(space12).
		Radius(radiusLg).Background(tokens.SurfaceRaised).Border(1, tokens.Border)
	card = shadowCard.apply(card)
	card.Children(func() {
		disclosure := mgo.CollapsibleBase(c, open)
		disclosure.Trigger.FillWidth().Gap(space8).AlignItems(mgo.Center).Label(title)
		disclosure.Trigger.Children(func() {
			mgo.Icon(c, icon).Size(14, 14).TextColor(tokens.TextMuted)
			mgo.Text(c, title).FontSize(textCaption).Bold().Grow(1)
			mgo.Icon(c, iconChevronRight).Size(12, 12).TextColor(tokens.TextMuted).Rotate(90 * disclosure.Progress())
		})
		disclosure.Panel(func() {
			mgo.Column(c).FillWidth().Gap(space8).Padding(space8, 0, 0, 0).Children(body)
		})
	})
}

func progressDetails(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	mgo.Row(c).Gap(space6).AlignItems(mgo.Center).Children(func() {
		if model.Phase == PhaseRunning || model.Phase == PhaseStarting || model.Phase == PhaseResyncing {
			mgo.Spinner(c).Size(10, 10)
		} else {
			mgo.Box(c).Size(6, 6).Radius(3).Background(phaseColor(model.Phase, c.Theme()))
		}
		mgo.Text(c, model.T(phaseLabel(model.Phase))).FontSize(textCaption).TextColor(tokens.TextMuted)
	})
	if usage := usageContextLabel(model); usage != "" {
		mgo.Text(c, usage).FontSize(textMicro).TextColor(tokens.TextMuted).LineHeight(1.4)
	}
	done, total := taskProgress(model.Tasks)
	if total > 0 {
		ratio := float64(done) / float64(total)
		mgo.Progress(c.Key("details-progress"), ratio).FillWidth().
			Label(fmt.Sprintf(model.T("Tasks %d/%d"), done, total))
		mgo.Text(c, fmt.Sprintf(model.T("Tasks %d/%d"), done, total)).FontSize(textMicro).TextColor(tokens.TextMuted)
		const maxRows = 4
		visible := model.Tasks
		if len(visible) > maxRows {
			visible = visible[:maxRows]
		}
		for _, task := range visible {
			prefix, color := "○", tokens.TextMuted
			switch task.Status {
			case "completed":
				prefix = "✓"
			case "in_progress":
				prefix, color = "▶", tokens.Accent
			}
			mgo.Text(c, prefix+" "+task.Content).FontSize(textMicro).TextColor(color).SingleLine()
		}
		if len(model.Tasks) > maxRows {
			mgo.Text(c, fmt.Sprintf(model.T("+%d more"), len(model.Tasks)-maxRows)).
				FontSize(textMicro).TextColor(tokens.TextMuted)
		}
	}
	if depth := depthLabel(model); depth != "" {
		mgo.Text(c, depth).FontSize(textMicro).TextColor(tokens.TextMuted).LineHeight(1.4)
	}
}

func folderDetails(c *mgo.Context, model *Model) {
	if model.SessionCWD == "" {
		mgo.Text(c, model.T("No working folder")).FontSize(textMicro).TextColor(tokensFor(c.Theme()).TextMuted)
	} else {
		detailValue(c, model, model.T("Working directory"), model.SessionCWD, model.T("Copy working folder"))
	}
	if model.SessionID != "" {
		detailValue(c, model, model.T("Session ID"), model.SessionID, model.T("Copy session ID"))
	}
}

func contextDetails(c *mgo.Context, model *Model) {
	if model.ModelLabel != "" {
		detailValue(c, model, model.T("Model"), model.ModelLabel, model.T("Copy model"))
	}
	if model.Permission.Available {
		label := model.T("Permissions: refresh Settings")
		if model.Permission.Ready {
			label = model.T("Permissions: ") + model.Permission.Snapshot.Current + " · " +
				model.Permission.Snapshot.Sandbox + " / " + model.Permission.Snapshot.Approval
		}
		mgo.Text(c, label).FontSize(textMicro).TextColor(tokensFor(c.Theme()).TextMuted).LineHeight(1.4)
	}
	if subagents := subagentLabel(model); subagents != "" {
		mgo.Text(c, subagents).FontSize(textMicro).TextColor(tokensFor(c.Theme()).TextMuted).LineHeight(1.4)
	}
	if model.Detail != "" {
		detailField(c, model.T("Runtime"), model.Detail)
	}
}

func detailField(c *mgo.Context, title, value string) {
	if value == "" {
		return
	}
	mgo.Column(c).Gap(space2).Children(func() {
		mgo.Text(c, title).FontSize(textMicro).TextColor(tokensFor(c.Theme()).TextMuted)
		mgo.Text(c, value).FontSize(textCaption).Selectable()
	})
}

func detailValue(c *mgo.Context, model *Model, title, value, copyLabel string) {
	if value == "" {
		return
	}
	tokens := tokensFor(c.Theme())
	mgo.Row(c).FillWidth().Gap(space6).AlignItems(mgo.Start).Children(func() {
		mgo.Column(c).Grow(1).MinWidth(0).Gap(space2).Children(func() {
			mgo.Text(c, title).FontSize(textMicro).TextColor(tokens.TextMuted)
			mgo.Text(c, value).FontSize(textCaption).Selectable()
		})
		iconButton(c.Key("copy-"+title), copyLabel, iconCopy).Padding(space2).
			OnClick(func() {
				c.WriteClipboard(value)
				c.Toast(model.T("Copied"))
			})
	})
}

// conversationDetails keeps the narrow-window Popover on the same body as the
// wide pane.
func conversationDetails(c *mgo.Context, model *Model) {
	_, height := c.Size()
	mgo.Column(c).Width(340).MaxWidth(360).Padding(space8).Gap(space8).Children(func() {
		mgo.Text(c, model.T("Conversation details")).FontSize(textBody).Bold()
		mgo.Scroll(c.Key("details-scroll")).MaxHeight(max(120, min(420, height-titleBarHeight(c)-140))).FillWidth().Children(func() {
			detailsBody(c, model)
		})
	})
}
