package view

import mgo "github.com/egoist/mygo/ui"

func conversationSettingsPage(c *mgo.Context, model *Model, disabled bool) {
	tokens := tokensFor(c.Theme())
	mgo.Text(c, model.T("Manage the model providers available in this profile.")).FontSize(textCaption).TextColor(tokens.TextMuted)
	if model.ActiveRoute.Provider != "" {
		settingsRow(c, model.T("Current conversation"), "", func() { mgo.Text(c, model.ModelLabel).FontSize(textLabel).Selectable() })
	}
	if model.Catalog.Reason != "" {
		mgo.Text(c, model.T(model.Catalog.Reason)).FontSize(textCaption).TextColor(tokens.Warning)
	}
	mgo.Column(c.Key("provider-list")).FillWidth().Gap(space10).Children(func() {
		for _, provider := range model.Catalog.Providers {
			mgo.Column(c.Key("provider-card-"+provider.ID)).FillWidth().Padding(space16).Gap(space12).Radius(radiusXl).Border(1, tokens.BorderStrong).Children(func() {
				mgo.Row(c).FillWidth().Gap(space8).AlignItems(mgo.Center).Children(func() {
					mgo.Column(c).Grow(1).MinWidth(0).Gap(space4).Children(func() {
						mgo.Text(c, provider.Name).FontSize(textLabel).Bold().SingleLine()
						status := model.T("Available in profile")
						if provider.ID == model.ActiveRoute.Provider {
							status = model.T("Current conversation") + " · " + model.ActiveRoute.Model
						}
						mgo.Text(c, status).FontSize(textMicro).TextColor(tokens.TextMuted).SingleLine()
					})
					mgo.Button(c.Key("edit-provider"), model.T("Edit")).Label(model.T("Edit provider · ")+provider.Name).Padding(space6, space12).Radius(radiusSm).Disabled(disabled).OnClick(func() {
						model.SettingsProviderEditing = true
						if model.SettingsProvider != provider.ID {
							model.SettingsProvider = provider.ID
							model.SettingsModel = ""
							model.SettingsCatalogModel = ""
							model.SettingsEffort = effortChoice("")
							if model.RefreshSettings != nil {
								model.RefreshSettings()
							}
						}
					})
				})
				if model.SettingsProviderEditing && model.SettingsProvider == provider.ID {
					mgo.Column(c.Key("provider-editor")).FillWidth().Gap(space12).Padding(space12, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(tokens.Border).Children(func() { modelRouteEditor(c, model, disabled) })
				}
			})
		}
	})
	if len(model.Catalog.Providers) == 0 && !model.SettingsBusy {
		mgo.Text(c, model.T("Refresh to load model providers.")).FontSize(textCaption).TextColor(tokens.TextMuted)
	}
}
