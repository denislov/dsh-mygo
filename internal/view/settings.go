package view

import (
	"strings"
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
	if model.Runtime.SetupRequired || model.Inspection.Open || model.Setup.Picking {
		return false
	}
	return (model.CanModels || model.Permission.Available || model.CanConfigure) && model.Phase == PhaseIdle && !model.LocalState.Closing && !model.SessionBusy && !model.SessionPanel.Uncertain && model.SessionPanel.EditingID == "" && model.SessionPanel.ArchiveID == "" && !model.SessionsOpen && !model.SettingsBusy && len(model.Approvals) == 0 && len(model.Questions) == 0
}
