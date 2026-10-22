package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"dsh-mygo/internal/view"
)

func validCredentialReference(value string) bool {
	if len(value) == 0 || len(value) > 256 {
		return false
	}
	for index, character := range value {
		if character != '_' && !(character >= 'A' && character <= 'Z') && !(character >= 'a' && character <= 'z') && !(index > 0 && character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}

func decodeConfiguration(raw json.RawMessage) (view.Configuration, error) {
	var snapshot view.Configuration
	var required struct {
		Revision *uint64 `json:"revision"`
		Defaults struct {
			Provider *string `json:"provider"`
			Model    *string `json:"model"`
			Effort   *string `json:"effort"`
		} `json:"defaults"`
		Credential struct {
			Available  *bool   `json:"available"`
			Configured *bool   `json:"configured"`
			Writable   *bool   `json:"writable"`
			Reference  *string `json:"reference"`
			Source     *string `json:"source"`
		} `json:"credential"`
	}
	invalid := errors.New("Invalid configuration response. Refresh to retry")
	if len(raw) > 8192 || json.Unmarshal(raw, &required) != nil || required.Revision == nil || *required.Revision > maxSafeInteger || required.Defaults.Provider == nil || required.Defaults.Model == nil || required.Defaults.Effort == nil || required.Credential.Available == nil || required.Credential.Configured == nil || required.Credential.Writable == nil || required.Credential.Reference == nil || required.Credential.Source == nil {
		return snapshot, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || !validModelLabel(snapshot.Defaults.Provider, false) || !validModelLabel(snapshot.Defaults.Model, false) || !validModelLabel(snapshot.Defaults.Effort, true) {
		return view.Configuration{}, invalid
	}
	credential := snapshot.Credential
	if credential.Source != "none" && credential.Source != "managed" && credential.Source != "environment" && credential.Source != "fallback" || credential.Configured == (credential.Source == "none") || credential.Available && !validCredentialReference(credential.Reference) || !credential.Available && (credential.Reference != "" || credential.Configured || credential.Writable) {
		return view.Configuration{}, invalid
	}
	return snapshot, nil
}

func readConfiguration(ctx context.Context, client rpcClient) (view.Configuration, error) {
	raw, err := client.Call(ctx, "configuration/get", map[string]any{})
	if err != nil {
		return view.Configuration{}, errors.New("Could not load configuration. Refresh to retry")
	}
	return decodeConfiguration(raw)
}

func (a *App) SaveDefaultModel() {
	if !a.canWriteConfiguration() || a.model.ModelUncertain {
		return
	}
	route := view.ModelRoute{Provider: a.model.SettingsProvider, Model: a.model.SettingsModel, Effort: a.model.SelectedEffort()}
	if !validModelLabel(route.Provider, false) || !validModelLabel(route.Model, false) || !validModelLabel(route.Effort, true) {
		a.model.SettingsError = "Choose a valid provider, model ID and effort"
		return
	}
	a.writeConfiguration("configuration/default-model", map[string]any{"revision": a.model.Configuration.Revision, "provider": route.Provider, "model": route.Model, "effort": route.Effort}, &route)
}

func (a *App) canWriteConfiguration() bool {
	return a.canEditSettings() && a.model.SettingsOpen && a.model.CanConfigure && a.model.ConfigurationReady && !a.model.ConfigurationUncertain && !a.model.ModelUncertain
}

func (a *App) SaveCredential() {
	value := a.model.TakeCredential()
	if !a.canWriteConfiguration() || !a.model.Configuration.Credential.Writable || !a.model.Configuration.Credential.Available {
		return
	}
	if value == "" || len(value) > 8192 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\t\x00 ") {
		a.model.SettingsError = "Enter a non-empty API key without whitespace (maximum 8192 bytes)"
		return
	}
	a.writeConfiguration("configuration/credential", map[string]any{"reference": a.model.Configuration.Credential.Reference, "operation": "set", "value": value}, nil)
}

func (a *App) ClearCredential() {
	a.model.ClearCredentialDraft()
	if !a.canWriteConfiguration() || !a.model.Configuration.Credential.Writable || !a.model.Configuration.Credential.Available || !a.model.ConfirmCredentialClear {
		return
	}
	a.model.ConfirmCredentialClear = false
	a.writeConfiguration("configuration/credential", map[string]any{"reference": a.model.Configuration.Credential.Reference, "operation": "unset"}, nil)
}

func (a *App) writeConfiguration(method string, params map[string]any, expected *view.ModelRoute) {
	ctx, operation := a.beginSettingsOperationTimeout(60 * time.Second)
	cancel, client, id := a.settingsCancel, a.currentClient(), a.sessionID
	a.model.SettingsApplying = true
	a.model.SettingsWritingConfiguration = true
	a.model.SettingsNotice = ""
	go func() {
		defer cancel()
		raw, err := client.Call(ctx, method, params)
		delete(params, "value")
		var snapshot view.Configuration
		if err == nil {
			snapshot, err = decodeConfiguration(raw)
			if err == nil && expected != nil && snapshot.Defaults != *expected {
				err = errors.New("Default model was not confirmed")
			}
			if err == nil && expected == nil && (!snapshot.Credential.Available || snapshot.Credential.Reference != params["reference"] || params["operation"] == "set" && !snapshot.Credential.Configured) {
				err = errors.New("Credential write was not confirmed")
			}
		}
		a.post(func() {
			if !a.currentSettingsOperation(operation, id) {
				return
			}
			a.cancelSettingsOperation()
			if err != nil {
				a.model.ConfigurationUncertain = true
				a.model.SettingsError = "Could not confirm the configuration write. Refresh before continuing or retrying"
				return
			}
			a.model.Configuration = snapshot
			a.model.ConfigurationReady = true
			a.model.ConfigurationUncertain = false
			a.model.SettingsNotice = "Configuration saved. Existing session model is unchanged."
		})
	}()
}

func (a *App) routeParams(canConfigure bool) map[string]any {
	if a.options.UseHostDefaults && canConfigure {
		return map[string]any{"useDefaultModel": true}
	}
	params := map[string]any{"provider": a.options.Provider, "model": a.options.Model}
	if a.options.ReasoningEffort != "" {
		params["effort"] = a.options.ReasoningEffort
	}
	return params
}
