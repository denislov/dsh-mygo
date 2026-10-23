package view

import (
	"strings"

	mgo "github.com/egoist/mygo/ui"
)

type ModelRoute struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
}

type CatalogName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CatalogModel struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Efforts   []CatalogName `json:"efforts"`
	Available bool          `json:"available"`
	Reason    string        `json:"reason"`
}

type ModelCatalog struct {
	Providers []CatalogName  `json:"providers"`
	Provider  string         `json:"provider"`
	Models    []CatalogModel `json:"models"`
	Selection ModelRoute     `json:"selection"`
	Available bool           `json:"available"`
	Reason    string         `json:"reason"`
}

func effortChoice(effort string) string {
	if effort == "" {
		return "Provider default"
	}
	return "Effort: " + effort
}

func (model *Model) SelectedEffort() string {
	if model.SettingsEffort == "Provider default" {
		return ""
	}
	return strings.TrimPrefix(model.SettingsEffort, "Effort: ")
}

func (model *Model) SetActiveRoute(route ModelRoute) {
	model.ActiveRoute = route
	model.ModelLabel = route.Provider + " · " + route.Model
	if route.Effort != "" {
		model.ModelLabel += " · " + route.Effort
	}
}

func (model *Model) PrepareSettings(route ModelRoute) {
	model.SettingsProvider = route.Provider
	model.SettingsModel = route.Model
	model.SettingsCatalogModel = route.Model
	model.SettingsEffort = effortChoice(route.Effort)
}

func (model *Model) CanEditSettings() bool {
	if model.Prefs.Open {
		return false
	}
	return (model.CanModels || model.Permission.Available) && model.Phase == PhaseIdle && !model.LocalState.Closing && !model.SessionBusy && !model.SessionPanel.Uncertain && model.SessionPanel.EditingID == "" && model.SessionPanel.ArchiveID == "" && !model.SessionsOpen && !model.SettingsBusy && len(model.Approvals) == 0 && len(model.Questions) == 0
}

func settingsModal(c *mgo.Context, model *Model) {
	if !model.SettingsOpen || !(model.CanModels || model.Permission.Available) || len(model.Approvals) > 0 || len(model.Questions) > 0 {
		return
	}
	disabled := !model.CanEditSettings()
	modal := mgo.Modal(c.Key("model-settings"), &model.SettingsOpen, func() {
		title := "Model settings"
		if model.Permission.Available {
			title = "Conversation settings"
		}
		mgo.Text(c, model.T(title)).FontSize(18).Bold()
		scope := "Current session only · next turn · launch defaults unchanged"
		if model.CanConfigure {
			scope = "Apply model: current session · Save default: future sessions"
		}
		mgo.Text(c, model.T(scope)).FontSize(11).TextColor(c.Theme().TextMuted)
		mgo.Scroll(c.Key("settings-body")).MaxHeight(230).FillWidth().Children(func() {
			mgo.Column(c).Gap(4).Children(func() {
				if model.SettingsBusy {
					label := "Loading model catalog…"
					if model.SettingsApplying {
						label = "Applying model…"
					}
					if model.SettingsWritingConfiguration {
						label = "Saving configuration…"
					}
					if model.Permission.Writing {
						label = "Applying permissions…"
					}
					mgo.Text(c, model.T(label)).FontSize(12)
				}
				if model.SettingsError != "" {
					mgo.Text(c, model.T(model.SettingsError)).FontSize(12).TextColor(c.Theme().Danger)
				}
				if model.SettingsNotice != "" {
					mgo.Text(c, model.T(model.SettingsNotice)).FontSize(11)
				}
				if model.Catalog.Reason != "" {
					mgo.Text(c, model.Catalog.Reason).FontSize(11).TextColor(c.Theme().Warning)
				}
				permissionControls(c, model, disabled)
				if model.CanModels {
					providers := make([]string, 0, len(model.Catalog.Providers))
					for _, provider := range model.Catalog.Providers {
						providers = append(providers, provider.ID)
					}
					mgo.Text(c, model.T("Provider route")).FontSize(11).TextColor(c.Theme().TextMuted)
					mgo.Select(c.Key("settings-provider"), &model.SettingsProvider, providers).Label(model.T("Provider")).Disabled(disabled).OnChange(func() {
						model.SettingsModel = ""
						model.SettingsCatalogModel = ""
						model.SettingsEffort = effortChoice("")
						if model.RefreshSettings != nil {
							model.RefreshSettings()
						}
					})
					models := []string{}
					efforts := []string{effortChoice("")}
					for _, entry := range model.Catalog.Models {
						if !entry.Available {
							mgo.Text(c, "Unavailable: "+entry.ID+" · "+entry.Reason).FontSize(11).TextColor(c.Theme().Warning)
						}
						if entry.Available {
							models = append(models, entry.ID)
						}
						if entry.ID == model.SettingsModel && entry.Available {
							for _, effort := range entry.Efforts {
								efforts = append(efforts, effortChoice(effort.ID))
							}
						}
					}
					if len(models) > 0 {
						mgo.Text(c, model.T("Catalog suggestion (optional)")).FontSize(11).TextColor(c.Theme().TextMuted)
						mgo.Select(c.Key("settings-model-catalog"), &model.SettingsCatalogModel, models).Label(model.T("Catalog model")).Disabled(disabled).OnChange(func() {
							model.SettingsModel = model.SettingsCatalogModel
							model.SettingsEffort = effortChoice("")
						})
					}
					mgo.Text(c, model.T("Model ID (applied)")).FontSize(11).TextColor(c.Theme().TextMuted)
					mgo.TextInput(c.Key("settings-model-id"), &model.SettingsModel).Label(model.T("Model ID")).Disabled(disabled).OnChange(func() { model.SettingsEffort = effortChoice("") })
					mgo.Text(c, model.T("Effort")).FontSize(11).TextColor(c.Theme().TextMuted)
					mgo.Select(c.Key("settings-effort"), &model.SettingsEffort, efforts).Label(model.T("Reasoning effort")).Disabled(disabled)
					mgo.Text(c, model.T("Manual model IDs are validated by the Host. Apply model affects only this session.")).FontSize(11).TextColor(c.Theme().TextMuted)
					configurationControls(c, model, disabled)
				}
			})
		})
		mgo.Row(c).Gap(8).Children(func() {
			mgo.Button(c.Key("settings-close"), model.T("Close")).Disabled(model.SettingsApplying || model.CloseSettings == nil).OnClick(func() { model.CloseSettings() })
			mgo.Spacer(c)
			mgo.Button(c.Key("settings-refresh"), model.T("Refresh")).Disabled(disabled || model.RefreshSettings == nil).OnClick(func() { model.RefreshSettings() })
			if model.CanModels {
				mgo.PrimaryButton(c.Key("settings-apply"), model.T("Apply model")).Disabled(disabled || model.ModelUncertain || model.ConfigurationUncertain || model.Permission.Uncertain || model.ApplySettings == nil || strings.TrimSpace(model.SettingsProvider) == "" || strings.TrimSpace(model.SettingsModel) == "").OnClick(func() { model.ApplySettings() })
			}
		})
	})
	if modal.Dismissed() {
		if model.SettingsApplying {
			model.SettingsOpen = true
		} else if model.CloseSettings != nil {
			model.CloseSettings()
		}
	}
}
