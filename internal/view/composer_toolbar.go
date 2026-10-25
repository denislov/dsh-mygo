package view

import (
	"fmt"
	mgo "github.com/egoist/mygo/ui"
)

func composerToolbar(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	mgo.Row(c.Key("composer-toolbar")).FillWidth().MaxWidth(readingWidth).Wrap().Gap(space4).AlignItems(mgo.Center).Children(func() {
		if model.Setup.ChooseFolder != nil {
			label := projectName(model.SessionCWD)
			if label == "" {
				label = model.T("Choose project folder")
			}
			b := mgo.Button(c.Key("composer-project"), "").Label(model.T("Choose project folder")).Tooltip(model.SessionCWD).
				Padding(space4, space6).Gap(space6).MaxWidth(180).Radius(radiusSm).Background(mgo.Transparent).Border(0, mgo.Transparent).
				Disabled(!model.CanChooseSession()).OnClick(model.Setup.ChooseFolder)
			if b.Hovered() {
				b.Background(tokens.SurfaceHover)
			}
			b.Children(func() {
				mgo.Icon(c, iconFolder).Size(14, 14).TextColor(tokens.TextMuted)
				mgo.Text(c, label).FontSize(textCaption).TextColor(tokens.TextMuted).SingleLine()
			})
		}
		if model.Permission.Available && model.OpenSettings != nil {
			label := model.T("Permissions")
			if model.Permission.Ready {
				label = model.Permission.Snapshot.Current
				for _, o := range model.Permission.Snapshot.Options {
					if o.ID == model.Permission.Snapshot.Current {
						label, _ = permissionPresentation(model, o)
					}
				}
			}
			anchor := quickChip(c.Key("composer-permissions"), model.T("Choose permissions"), label).MaxWidth(190).
				Disabled(!model.CanEditSettings() || model.SettingsOpen).OnClick(func() { model.openQuickSelector("permission") })
			quickSelector(c, model, anchor, "permission")
		}
		mgo.Spacer(c)
		if model.CanModels && model.OpenSettings != nil {
			label := model.ActiveRoute.Model
			for _, entry := range model.Catalog.Models {
				if model.Catalog.Provider == model.ActiveRoute.Provider && entry.ID == model.ActiveRoute.Model {
					label = entry.Name
				}
			}
			if label == "" {
				label = model.ModelLabel
			}
			if label == "" {
				label = model.T("Choose model")
			}
			if model.ActiveRoute.Effort != "" {
				label += " · " + model.ActiveRoute.Effort
			}
			anchor := quickChip(c.Key("composer-model"), model.T("Choose model"), label).MaxWidth(360).
				Tooltip(model.ModelLabel).Disabled(!model.CanEditSettings() || model.SettingsOpen).OnClick(func() { model.openQuickSelector("model") })
			quickSelector(c, model, anchor, "model")
		}
		if text := usageContextLabel(model); text != "" {
			label := model.T("Context")
			if model.Context.Window > 0 {
				label = formatTokens(model.Context.Window)
				if model.Usage.HasUsage {
					used := min(maxReportedTokens, model.Usage.InputTokens+model.Usage.OutputTokens)
					label = fmt.Sprintf("%d%% · %s", used*100/model.Context.Window, label)
				}
			}
			mgo.Text(c.Key("composer-context"), label).FontSize(textMicro).TextColor(tokens.TextFaint).Tooltip(text)
		}
	})
}

func composerSetupStatus(c *mgo.Context, model *Model) {
	mgo.Column(c.Key("composer-setup-status")).FillWidth().MaxWidth(readingWidth).Gap(space4).Children(func() {
		if model.Setup.WorkspaceRequired {
			mgo.Button(c.Key("use-folder"), model.T("Use this folder")).Tooltip(model.SessionCWD).
				Disabled(!model.CanChooseSession() || model.Setup.UseFolder == nil).OnClick(model.Setup.UseFolder)
		}
		if model.Setup.Picking {
			mgo.Text(c, model.T("Choosing a project folder…")).FontSize(textMicro)
		}
		errorText := model.Setup.Error
		if errorText == "" {
			errorText = model.SessionsError
		}
		if errorText != "" {
			mgo.Text(c, model.T(errorText)).FontSize(textMicro).TextColor(c.Theme().Danger)
		}
		if model.NeedsCredential() {
			mgo.Text(c, model.T("Add an API key in Settings → Connection")).FontSize(textMicro).TextColor(c.Theme().Warning)
		}
	})
}
