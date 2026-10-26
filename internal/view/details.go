package view

import (
	"fmt"

	mgo "github.com/egoist/mygo/ui"
)

func detailsBody(c *mgo.Context, model *Model) {
	mgo.Column(c).FillWidth().Gap(space12).Children(func() {
		detailSection(c, "folder", model.T("Environment"), iconFolder, &model.Shell.FolderOpen, func() {
			mgo.Row(c).Gap(space8).Children(func() {
				mgo.Icon(c, iconMonitor).Size(16, 16)
				mgo.Text(c, model.T("This computer")).FontSize(textCaption)
			})
			folderDetails(c, model)
			if model.Workspace.Changes.Branch != "" {
				mgo.Text(c, model.Workspace.Changes.Branch).FontSize(textCaption)
			}
		})
		detailSection(c, "context", model.T("Context"), iconInfo, &model.Shell.ContextOpen, func() {
			if contextUsageAvailable(model) {
				contextUsagePanel(c, model, "details")
			} else {
				mgo.Text(c, model.T("No context metrics yet")).FontSize(textMicro).TextColor(tokensFor(c.Theme()).TextFaint)
			}
			contextDetails(c, model)
		})
		detailSection(c, "progress", model.T("Progress"), iconActivity, &model.Shell.ProgressOpen, func() { progressDetails(c, model) })
		if len(model.Workspace.Changes.Files) > 0 {
			mgo.Button(c.Key("rail-changes"), model.Tf("Changed files · %d", len(model.Workspace.Changes.Files))).FillWidth().OnClick(func() {
				if !model.Workspace.Open[PanelChanges] {
					model.ToggleWorkspacePanel(PanelChanges)
				}
			})
		}
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
			mgo.Icon(c, icon).Size(iconDefault, iconDefault).TextColor(tokens.TextMuted)
			mgo.Text(c, title).FontSize(textCaption).Bold().Grow(1)
			mgo.Icon(c, iconChevronRight).Size(iconSmall, iconSmall).TextColor(tokens.TextMuted).Rotate(90 * disclosure.Progress())
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
