package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"dsh-mygo/internal/protocol"
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
	var snapshot protocol.ConfigurationSnapshot
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
		return view.Configuration{}, invalid
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
	return configurationView(snapshot), nil
}

func configurationView(snapshot protocol.ConfigurationSnapshot) view.Configuration {
	return view.Configuration{
		Revision: snapshot.Revision,
		Defaults: view.ModelRoute{Provider: snapshot.Defaults.Provider, Model: snapshot.Defaults.Model, Effort: snapshot.Defaults.Effort},
		Credential: view.CredentialStatus{
			Available: snapshot.Credential.Available, Reference: snapshot.Credential.Reference,
			Configured: snapshot.Credential.Configured, Writable: snapshot.Credential.Writable, Source: snapshot.Credential.Source,
		},
	}
}

func readConfiguration(ctx context.Context, client rpcClient) (view.Configuration, error) {
	raw, err := client.Call(ctx, protocol.MethodConfigurationGet, struct{}{})
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
		a.model.SettingsError = msgSettingsInvalidRoute
		return
	}
	a.writeConfiguration(protocol.MethodConfigurationDefaultModel, protocol.ConfigurationDefaultModelParams{
		Revision: a.model.Configuration.Revision, Provider: route.Provider, Model: route.Model, Effort: route.Effort,
	}, func(snapshot view.Configuration) error {
		if snapshot.Defaults != route {
			return errors.New("Default model was not confirmed")
		}
		return nil
	})
}

func (a *App) canWriteConfiguration() bool {
	return a.canEditSettings() && a.model.SettingsOpen && a.model.CanConfigure && a.model.ConfigurationReady && !a.model.ConfigurationUncertain && !a.model.ModelUncertain && !a.model.Permission.Uncertain
}

func (a *App) SaveCredential() {
	value := a.model.TakeCredential()
	if !a.canWriteConfiguration() || !a.model.Configuration.Credential.Writable || !a.model.Configuration.Credential.Available {
		return
	}
	if value == "" || len(value) > 8192 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\t\x00 ") {
		a.model.SettingsError = msgSettingsInvalidCredential
		return
	}
	reference := a.model.Configuration.Credential.Reference
	request := &protocol.ConfigurationCredentialParams{Reference: reference, Operation: protocol.CredentialOperationSet, Value: &value}
	a.writeConfiguration(protocol.MethodConfigurationCredential, request, func(snapshot view.Configuration) error {
		if !snapshot.Credential.Available || snapshot.Credential.Reference != reference || !snapshot.Credential.Configured {
			return errors.New("Credential write was not confirmed")
		}
		return nil
	})
}

func (a *App) ClearCredential() {
	a.model.ClearCredentialDraft()
	if !a.canWriteConfiguration() || !a.model.Configuration.Credential.Writable || !a.model.Configuration.Credential.Available || !a.model.ConfirmCredentialClear {
		return
	}
	a.model.ConfirmCredentialClear = false
	reference := a.model.Configuration.Credential.Reference
	a.writeConfiguration(protocol.MethodConfigurationCredential, &protocol.ConfigurationCredentialParams{Reference: reference, Operation: protocol.CredentialOperationUnset}, func(snapshot view.Configuration) error {
		if !snapshot.Credential.Available || snapshot.Credential.Reference != reference {
			return errors.New("Credential write was not confirmed")
		}
		return nil
	})
}

func (a *App) writeConfiguration(method string, params any, confirm func(view.Configuration) error) {
	ctx, operation, cancel := a.beginSettingsOperationTimeout(60 * time.Second)
	client, id := a.currentClient(), a.sessionID
	a.model.SettingsApplying = true
	a.model.SettingsWritingConfiguration = true
	a.model.SettingsNotice = ""
	go func() {
		defer cancel()
		raw, err := client.Call(ctx, method, params)
		if request, ok := params.(*protocol.ConfigurationCredentialParams); ok {
			request.Value = nil
		}
		var snapshot view.Configuration
		if err == nil {
			snapshot, err = decodeConfiguration(raw)
			if err == nil && confirm != nil {
				err = confirm(snapshot)
			}
		}
		a.post(func() {
			if !a.currentSettingsOperation(operation, id) {
				return
			}
			a.cancelSettingsOperation()
			if err != nil {
				a.model.ConfigurationUncertain = true
				a.model.SettingsError = msgSettingsWriteUnconfirmed
				return
			}
			a.model.Configuration = snapshot
			a.model.ConfigurationReady = true
			a.model.ConfigurationUncertain = false
			a.model.SettingsNotice = msgSettingsSaved
		})
	}()
}

// sessionRoute returns the model route for a create/resume request. Host
// defaults require the negotiated configuration capability; explicit routes
// always carry provider and model and omit an empty effort.
func (a *App) sessionRoute(canConfigure bool) protocol.SessionRoute {
	if a.options.UseHostDefaults && canConfigure {
		return protocol.HostDefaultRoute()
	}
	return protocol.ExplicitRoute(a.options.Provider, a.options.Model, a.options.ReasoningEffort)
}
