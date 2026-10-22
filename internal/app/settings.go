package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"dsh-mygo/internal/view"
)

func validModelLabel(value string, empty bool) bool {
	if len(value) > 256 || !empty && strings.TrimSpace(value) == "" {
		return false
	}
	for _, character := range value {
		if character < 32 || character == 127 {
			return false
		}
	}
	return true
}

func readModelCatalog(ctx context.Context, client rpcClient, id, provider string) (view.ModelCatalog, error) {
	var catalog view.ModelCatalog
	params := map[string]any{"sessionId": id}
	if provider != "" {
		params["provider"] = provider
	}
	raw, err := client.Call(ctx, "model/list", params)
	if err != nil {
		return catalog, errors.New("Could not load model catalog. Refresh to retry")
	}
	var required struct {
		Provider  *string `json:"provider"`
		Available *bool   `json:"available"`
		Selection struct {
			Provider *string `json:"provider"`
			Model    *string `json:"model"`
			Effort   *string `json:"effort"`
		} `json:"selection"`
		Models []struct {
			Available *bool `json:"available"`
		} `json:"models"`
	}
	invalid := errors.New("Invalid or oversized model catalog")
	if len(raw) > 512*1024 || json.Unmarshal(raw, &catalog) != nil || json.Unmarshal(raw, &required) != nil || required.Provider == nil || required.Available == nil || required.Selection.Provider == nil || required.Selection.Model == nil || required.Selection.Effort == nil || catalog.Providers == nil || catalog.Models == nil || len(catalog.Providers) > 32 || len(catalog.Models) > 128 || !validModelLabel(catalog.Provider, false) || provider != "" && catalog.Provider != provider || !validModelLabel(catalog.Selection.Provider, false) || !validModelLabel(catalog.Selection.Model, false) || !validModelLabel(catalog.Selection.Effort, true) || len(catalog.Reason) > 1024 {
		return view.ModelCatalog{}, invalid
	}
	providers := make(map[string]bool)
	for _, entry := range catalog.Providers {
		if !validModelLabel(entry.ID, false) || !validModelLabel(entry.Name, false) || providers[entry.ID] {
			return view.ModelCatalog{}, invalid
		}
		providers[entry.ID] = true
	}
	if catalog.Available && !providers[catalog.Provider] {
		return view.ModelCatalog{}, invalid
	}
	models := make(map[string]bool)
	for index, entry := range catalog.Models {
		if !validModelLabel(entry.ID, false) || !validModelLabel(entry.Name, false) || required.Models[index].Available == nil || models[entry.ID] || entry.Efforts == nil || len(entry.Efforts) > 32 || len(entry.Reason) > 1024 {
			return view.ModelCatalog{}, invalid
		}
		models[entry.ID] = true
		efforts := make(map[string]bool)
		for _, effort := range entry.Efforts {
			if !validModelLabel(effort.ID, false) || !validModelLabel(effort.Name, false) || efforts[effort.ID] {
				return view.ModelCatalog{}, invalid
			}
			efforts[effort.ID] = true
		}
	}
	return catalog, nil
}

func (a *App) canEditSettings() bool {
	return !a.options.SDKProtocol && !a.shuttingDown.Load() && a.model.CanEditSettings() && a.currentClient() != nil
}

func (a *App) OpenSettings() {
	if !a.canEditSettings() {
		return
	}
	a.model.SettingsOpen = true
	a.model.ToolInspector = nil
	a.model.Catalog = view.ModelCatalog{}
	a.model.PrepareSettings(a.model.ActiveRoute)
	a.RefreshSettings()
}

func (a *App) CloseSettings() {
	if a.model.SettingsApplying {
		return
	}
	a.cancelSettingsOperation()
	a.model.SettingsOpen = false
}

func (a *App) cancelSettingsOperation() {
	a.settingsOperation++
	if a.settingsCancel != nil {
		a.settingsCancel()
		a.settingsCancel = nil
	}
	a.model.SettingsBusy = false
	a.model.SettingsApplying = false
}

func (a *App) beginSettingsOperation() (context.Context, uint64) {
	a.cancelSettingsOperation()
	a.model.SettingsBusy = true
	a.model.SettingsError = ""
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	a.settingsCancel = cancel
	return ctx, a.settingsOperation
}

func (a *App) currentSettingsOperation(operation uint64, id string) bool {
	return operation == a.settingsOperation && a.sessionID == id && !a.shuttingDown.Load() && a.model.Phase != view.PhaseFailed
}

func (a *App) RefreshSettings() {
	if !a.canEditSettings() || !a.model.SettingsOpen {
		return
	}
	ctx, operation := a.beginSettingsOperation()
	client, id, provider, cancel := a.currentClient(), a.sessionID, a.model.SettingsProvider, a.settingsCancel
	go func() {
		defer cancel()
		catalog, err := readModelCatalog(ctx, client, id, provider)
		a.post(func() {
			if !a.currentSettingsOperation(operation, id) {
				return
			}
			a.cancelSettingsOperation()
			if err != nil {
				a.model.SettingsError = err.Error()
				return
			}
			a.model.Catalog = catalog
			a.model.SetActiveRoute(catalog.Selection)
			a.model.ModelUncertain = false
			a.model.SettingsProvider = catalog.Provider
			if a.model.SettingsModel == "" {
				if catalog.Provider == catalog.Selection.Provider {
					a.model.PrepareSettings(catalog.Selection)
				} else if len(catalog.Models) > 0 {
					a.model.SettingsModel = catalog.Models[0].ID
					a.model.SettingsCatalogModel = catalog.Models[0].ID
				}
			}
		})
	}()
}

func (a *App) ApplySettings() {
	if !a.canEditSettings() || !a.model.SettingsOpen {
		return
	}
	if a.model.ModelUncertain {
		a.model.SettingsError = "Refresh to confirm the active route before retrying"
		return
	}
	route := view.ModelRoute{Provider: a.model.SettingsProvider, Model: a.model.SettingsModel, Effort: a.model.SelectedEffort()}
	if !validModelLabel(route.Provider, false) || !validModelLabel(route.Model, false) || !validModelLabel(route.Effort, true) {
		a.model.SettingsError = "Choose a valid provider, model ID and effort"
		return
	}
	ctx, operation := a.beginSettingsOperation()
	a.model.SettingsApplying = true
	client, id, cancel, startSeq := a.currentClient(), a.sessionID, a.settingsCancel, a.nextSeq
	go func() {
		defer cancel()
		raw, err := client.Call(ctx, "model/select", map[string]any{"sessionId": id, "provider": route.Provider, "model": route.Model, "effort": route.Effort})
		var summary sessionSummary
		if err == nil {
			var response struct {
				Session json.RawMessage `json:"session"`
			}
			if json.Unmarshal(raw, &response) != nil {
				err = errors.New("Invalid model response")
			} else {
				summary, err = decodeSessionSummary(response.Session)
			}
			if err == nil && (summary.ID != id || summary.Status != "idle" || summary.NextSeq == nil || summary.Provider != route.Provider || summary.Model != route.Model || summary.Effort != route.Effort) {
				err = errors.New("Invalid model selection acknowledgment")
			}
		}
		a.post(func() {
			if !a.currentSettingsOperation(operation, id) {
				return
			}
			a.cancelSettingsOperation()
			if err != nil {
				a.model.ModelUncertain = true
				a.model.SettingsError = "Could not apply the model. Refresh to confirm the active route before retrying"
				return
			}
			if *summary.NextSeq <= startSeq || *summary.NextSeq > a.nextSeq {
				a.setFailure("Model selection event is missing", errors.New("Restart the host to recover from the durable log"))
				go a.stopHost()
				return
			}
			a.model.SetActiveRoute(route)
			a.model.ModelUncertain = false
			a.model.SettingsOpen = false
		})
	}()
}
