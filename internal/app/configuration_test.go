package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

const configurationJSON = `{"revision":2,"defaults":{"provider":"test","model":"saved","effort":""},"credential":{"available":true,"reference":"MYGO_TEST_KEY","configured":false,"writable":true,"source":"none"}}`
const configuredConfigurationJSON = `{"revision":3,"defaults":{"provider":"test","model":"saved","effort":""},"credential":{"available":true,"reference":"MYGO_TEST_KEY","configured":true,"writable":true,"source":"managed"}}`

func TestConfigurationValidationAndSafeErrors(t *testing.T) {
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method != protocol.MethodConfigurationGet || len(requestFields(t, params)) != 0 {
			t.Fatalf("configuration request = %s %v", method, params)
		}
		return json.RawMessage(configurationJSON), nil
	}}
	if snapshot, err := readConfiguration(context.Background(), client); err != nil || snapshot.Defaults.Model != "saved" {
		t.Fatal("valid configuration rejected")
	}
	for _, payload := range []string{`{}`, strings.Replace(configurationJSON, `"revision":2`, `"revision":-1`, 1), strings.Replace(configurationJSON, `"configured":false`, `"configured":null`, 1), strings.Replace(configurationJSON, `"source":"none"`, `"source":"unsafe-value"`, 1), strings.Replace(configurationJSON, `"reference":"MYGO_TEST_KEY"`, `"reference":"../bad"`, 1)} {
		client.request = func(context.Context, string, any) (json.RawMessage, error) { return json.RawMessage(payload), nil }
		if _, err := readConfiguration(context.Background(), client); err == nil {
			t.Fatal("accepted invalid configuration")
		}
	}
	client.request = func(context.Context, string, any) (json.RawMessage, error) {
		return nil, errors.New("fake-secret-must-not-appear")
	}
	if _, err := readConfiguration(context.Background(), client); err == nil || strings.Contains(err.Error(), "fake-secret") {
		t.Fatal("configuration error was not redacted")
	}
}

func TestConfigurationSaveRequiresReadbackAndPreservesSession(t *testing.T) {
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		request, ok := params.(protocol.ConfigurationDefaultModelParams)
		if method != protocol.MethodConfigurationDefaultModel || !ok || request.Revision != 2 {
			t.Fatalf("unexpected configuration request: %T %+v", params, params)
		}
		if value, present := requestFields(t, params)["effort"]; !present || value != "" {
			t.Fatal("empty default effort must be sent to clear the value")
		}
		return json.RawMessage(configurationJSON), nil
	}}
	application, updates := sessionTestApp(client)
	application.model.CanModels = true
	application.model.CanConfigure = true
	application.model.SettingsOpen = true
	application.model.ConfigurationReady = true
	application.model.Configuration.Revision = 2
	application.model.PrepareSettings(application.model.ActiveRoute)
	application.model.SettingsModel = "saved"
	application.model.Draft = "draft"
	application.SaveDefaultModel()
	application.CloseSettings()
	if !application.model.SettingsApplying || !application.model.SettingsOpen {
		t.Fatal("save could be dismissed")
	}
	completeSettingsJob(t, application, updates)
	if application.model.ActiveRoute.Model != "echo" || application.model.Draft != "draft" || application.model.Configuration.Defaults.Model != "saved" || !application.model.SettingsOpen {
		t.Fatal("default save changed current session")
	}
}

func TestCredentialSubmissionClearsInputAndQuarantinesFailedWrite(t *testing.T) {
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		request, ok := params.(*protocol.ConfigurationCredentialParams)
		if method != protocol.MethodConfigurationCredential || !ok || request.Operation != protocol.CredentialOperationUnset || request.Value != nil {
			t.Fatalf("unexpected credential request: %T %+v", params, params)
		}
		return nil, errors.New("fake-secret-must-not-appear")
	}}
	application, updates := sessionTestApp(client)
	application.model.CanModels = true
	application.model.CanConfigure = true
	application.model.SettingsOpen = true
	application.model.ConfigurationReady = true
	application.model.Configuration, _ = decodeConfiguration(json.RawMessage(configurationJSON))
	application.SaveCredential()
	if application.model.SettingsBusy {
		t.Fatal("empty credential was submitted")
	}
	application.model.ConfirmCredentialClear = true
	application.ClearCredential()
	completeSettingsJob(t, application, updates)
	if !application.model.ConfigurationUncertain || application.model.SettingsError == "" || strings.Contains(application.model.SettingsError, "fake-secret") {
		t.Fatal("failed write was not quarantined")
	}
	application.SaveDefaultModel()
	if application.model.SettingsBusy {
		t.Fatal("unconfirmed write retried without refresh")
	}
}

func TestCredentialRequestDropsSecretAfterCall(t *testing.T) {
	value := "fake-only-secret"
	request := &protocol.ConfigurationCredentialParams{Reference: "MYGO_TEST_KEY", Operation: protocol.CredentialOperationSet, Value: &value}
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method != protocol.MethodConfigurationCredential || params != request {
			t.Fatalf("unexpected credential request: %T", params)
		}
		return json.RawMessage(configuredConfigurationJSON), nil
	}}
	application, updates := sessionTestApp(client)
	application.model.CanConfigure = true
	application.model.SettingsOpen = true
	application.model.ConfigurationReady = true
	application.writeConfiguration(protocol.MethodConfigurationCredential, request, func(snapshot view.Configuration) error {
		if !snapshot.Credential.Configured {
			return errors.New("Credential write was not confirmed")
		}
		return nil
	})
	completeSettingsJob(t, application, updates)
	if request.Value != nil || !application.model.Configuration.Credential.Configured || application.model.ConfigurationUncertain || application.model.SettingsError != "" {
		t.Fatal("credential payload survived transport or was not applied")
	}
}

func TestConfigurationFailureBlocksSendAndNavigationUntilCompleteRefresh(t *testing.T) {
	client := &sessionClient{request: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
		switch method {
		case "model/list":
			return json.RawMessage(modelCatalogJSON), nil
		case "configuration/get":
			return json.RawMessage(configurationJSON), nil
		default:
			t.Error("unconfirmed configuration allowed an unexpected operation")
			return nil, errors.New("not allowed")
		}
	}}
	application, updates := sessionTestApp(client)
	application.model.CanModels = true
	application.model.CanConfigure = true
	application.model.ConfigurationUncertain = true
	application.model.Draft = "draft"
	application.Send("must not send")
	application.NewSession()
	application.OpenSessions()
	if application.model.SessionBusy || application.model.SessionsOpen {
		t.Fatal("unconfirmed configuration allowed navigation")
	}
	application.OpenSettings()
	completeSettingsJob(t, application, updates)
	if application.model.ConfigurationUncertain || !application.model.ConfigurationReady || application.model.Draft != "draft" || application.model.ActiveRoute.Model != "echo" {
		t.Fatal("configuration refresh did not safely recover")
	}
}

func TestDefaultRoutingIsNegotiatedAndExplicitOptionsRemainOverrides(t *testing.T) {
	application, _ := sessionTestApp(&sessionClient{})
	application.options.UseHostDefaults = true
	defaultRoute := requestFields(t, application.sessionRoute(true))
	if defaultRoute["useDefaultModel"] != true || defaultRoute["provider"] != nil {
		t.Fatal("Host defaults were not capability gated")
	}
	explicitRoute := requestFields(t, application.sessionRoute(false))
	if explicitRoute["provider"] != "test" || explicitRoute["useDefaultModel"] != nil {
		t.Fatal("unnegotiated Host defaults leaked into the explicit route")
	}
	application.options.UseHostDefaults = false
	if requestFields(t, application.sessionRoute(true))["provider"] != "test" {
		t.Fatal("explicit application route did not override Host defaults")
	}
}
