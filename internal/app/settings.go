package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"dsh-mygo/internal/protocol"
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
	var catalog protocol.ModelCatalog
	params := protocol.ModelListParams{SessionID: id, Provider: provider}
	raw, err := client.Call(ctx, protocol.MethodModelList, params)
	if err != nil {
		return view.ModelCatalog{}, errors.New("Could not load model catalog. Refresh to retry")
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
	return modelCatalogView(catalog), nil
}

// modelCatalogView maps the validated wire catalog into UI data. Input
// modalities stay on the wire because the settings UI does not consume them.
func modelCatalogView(catalog protocol.ModelCatalog) view.ModelCatalog {
	result := view.ModelCatalog{
		Providers: make([]view.CatalogName, 0, len(catalog.Providers)),
		Provider:  catalog.Provider,
		Models:    make([]view.CatalogModel, 0, len(catalog.Models)),
		Selection: view.ModelRoute{Provider: catalog.Selection.Provider, Model: catalog.Selection.Model, Effort: catalog.Selection.Effort},
		Available: catalog.Available,
		Reason:    catalog.Reason,
	}
	for _, provider := range catalog.Providers {
		result.Providers = append(result.Providers, view.CatalogName{ID: provider.ID, Name: provider.Name})
	}
	for _, model := range catalog.Models {
		entry := view.CatalogModel{
			ID: model.ID, Name: model.Name, Available: model.Available, Reason: model.Reason,
			Efforts: make([]view.CatalogName, 0, len(model.Efforts)),
		}
		for _, effort := range model.Efforts {
			entry.Efforts = append(entry.Efforts, view.CatalogName{ID: effort.ID, Name: effort.Name})
		}
		result.Models = append(result.Models, entry)
	}
	return result
}

func (a *App) canEditSettings() bool {
	return a.protocol.richUI() && !a.shuttingDown.Load() && a.model.CanEditSettings() && a.currentClient() != nil
}

func (a *App) OpenSettings() {
	a.openSettings(false)
}

func (a *App) OpenConnectionSettings() {
	if a.model.CanConfigure {
		a.openSettings(true)
	}
}

func (a *App) openSettings(connection bool) {
	if !a.canEditSettings() {
		return
	}
	if a.model.History.Busy {
		a.cancelSessionOperation()
	}
	a.model.SettingsOpen = true
	a.model.SettingsSearch = ""
	a.model.SettingsProviderEditing = false
	a.model.QuickSelector = ""
	a.model.SettingsConnection = connection
	if connection {
		a.model.SettingsSection = view.SettingsSectionConnection
	} else {
		a.model.SettingsSection = view.SettingsSectionConversation
	}
	if a.model.Prefs.Ready {
		a.model.Prefs.Draft = a.model.Prefs.Value
		a.model.Prefs.Error = ""
	}
	a.model.ConfigurationReady = false
	a.model.SettingsNotice = ""
	a.model.ConfirmCredentialClear = false
	a.model.Permission.Confirm = false
	a.model.ClearCredentialDraft()
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
	a.model.QuickSelector = ""
	a.model.SettingsConnection = false
	a.model.SettingsSection = view.SettingsSectionConversation
	a.model.ClearCredentialDraft()
	a.model.ConfirmCredentialClear = false
	a.model.Permission.Confirm = false
	if (a.model.Phase == view.PhaseFailed || a.model.Phase == view.PhaseResyncing) && a.model.Recoverable {
		a.model.RecoveryOpen = true
	}
}

func (a *App) cancelSettingsOperation() {
	a.model.ClearCredentialDraft()
	a.settingsComponent.invalidate()
	a.model.SettingsBusy = false
	a.model.SettingsApplying = false
	a.model.SettingsWritingConfiguration = false
	a.model.Permission.Writing = false
}

func (a *App) beginSettingsOperation() (context.Context, uint64, context.CancelFunc) {
	return a.beginSettingsOperationTimeout(15 * time.Second)
}

func (a *App) beginSettingsOperationTimeout(timeout time.Duration) (context.Context, uint64, context.CancelFunc) {
	a.cancelSettingsOperation()
	a.model.SettingsBusy = true
	a.model.SettingsError = ""
	return a.settingsComponent.begin(timeout, a.sessionID)
}

func (a *App) currentSettingsOperation(operation uint64, id string) bool {
	return a.settingsComponent.current(operation, id) && !a.shuttingDown.Load() && a.model.Phase != view.PhaseFailed
}

// settingsHostChanged invalidates in-flight Host settings reads/writes for a new
// generation while leaving device preferences intact.
func (a *App) settingsHostChanged() {
	a.settingsComponent.hostChanged()
	a.model.SettingsBusy = false
	a.model.SettingsApplying = false
	a.model.SettingsWritingConfiguration = false
	a.model.Permission.Writing = false
}

func (a *App) RefreshSettings() {
	if !a.canEditSettings() || !a.model.SettingsOpen {
		return
	}
	ctx, operation, cancel := a.beginSettingsOperation()
	client, id, provider := a.currentClient(), a.sessionID, a.model.SettingsProvider
	canConfigure := a.model.CanConfigure
	canModels, canPermissions := a.model.CanModels && !a.model.SettingsConnection, a.model.Permission.Available && !a.model.SettingsConnection
	a.model.Permission.Confirm = false
	go func() {
		defer cancel()
		var catalog view.ModelCatalog
		var err error
		if canModels {
			catalog, err = readModelCatalog(ctx, client, id, provider)
		}
		var configuration view.Configuration
		var permissions view.PermissionSnapshot
		if err == nil && canConfigure {
			configuration, err = readConfiguration(ctx, client)
		}
		if err == nil && canPermissions {
			permissions, err = readPermissions(ctx, client, id)
		}
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
			if canPermissions {
				if permissions.NextSeq != a.nextSeq {
					a.model.SettingsError = msgPermissionsChanged
					return
				}
				a.model.Permission.Accept(permissions)
			}
			if canConfigure {
				a.model.Configuration = configuration
				a.model.ConfigurationReady = true
				a.model.ConfigurationUncertain = false
			}
			if canModels {
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
			}
		})
	}()
}

func (a *App) ApplySettings() {
	if !a.canEditSettings() || !a.model.SettingsOpen || !a.model.CanModels {
		return
	}
	if a.model.ModelUncertain || a.model.ConfigurationUncertain || a.model.Permission.Uncertain {
		a.model.SettingsError = msgSettingsRefreshRoute
		return
	}
	route := view.ModelRoute{Provider: a.model.SettingsProvider, Model: a.model.SettingsModel, Effort: a.model.SelectedEffort()}
	if !validModelLabel(route.Provider, false) || !validModelLabel(route.Model, false) || !validModelLabel(route.Effort, true) {
		a.model.SettingsError = msgSettingsInvalidRoute
		return
	}
	ctx, operation, cancel := a.beginSettingsOperation()
	a.model.SettingsApplying = true
	client, id, startSeq := a.currentClient(), a.sessionID, a.nextSeq
	go func() {
		defer cancel()
		raw, err := client.Call(ctx, protocol.MethodModelSelect, protocol.ModelSelectParams{
			SessionID: id, Provider: route.Provider, Model: route.Model, Effort: route.Effort,
		})
		var summary protocol.SessionSummary
		if err == nil {
			var response protocol.SessionResult
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
				a.model.SettingsError = msgSettingsApplyFailed
				return
			}
			if *summary.NextSeq <= startSeq || *summary.NextSeq > a.nextSeq {
				a.setFailure(msgModelSelectionMissing, errors.New("Restart the host to recover from the durable log"))
				go a.stopHost()
				return
			}
			a.model.SetActiveRoute(route)
			a.model.ModelUncertain = false
			a.model.SettingsOpen = false
			a.model.QuickSelector = ""
		})
	}()
}
