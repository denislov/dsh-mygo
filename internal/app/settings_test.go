package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

const modelCatalogJSON = `{"providers":[{"id":"test","name":"Test"}],"provider":"test","models":[{"id":"echo","name":"Echo","available":true,"efforts":[]}],"selection":{"provider":"test","model":"echo","effort":""},"available":true}`

func TestModelCatalogValidationAndRedaction(t *testing.T) {
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) {
		return json.RawMessage(modelCatalogJSON), nil
	}}
	if catalog, err := readModelCatalog(context.Background(), client, "original", "test"); err != nil || catalog.Selection.Model != "echo" {
		t.Fatalf("catalog: %+v, %v", catalog, err)
	}
	for _, payload := range []string{`{}`, strings.Replace(modelCatalogJSON, `"available":true`, `"available":null`, 1), strings.Replace(modelCatalogJSON, `"provider":"test"`, `"provider":"other"`, 1), strings.Replace(modelCatalogJSON, `"efforts":[]`, `"efforts":null`, 1), strings.Replace(modelCatalogJSON, `"effort":""`, `"effort":null`, 1)} {
		client.request = func(context.Context, string, any) (json.RawMessage, error) { return json.RawMessage(payload), nil }
		if _, err := readModelCatalog(context.Background(), client, "original", "test"); err == nil {
			t.Errorf("accepted invalid catalog: %s", payload)
		}
	}
	client.request = func(context.Context, string, any) (json.RawMessage, error) { return nil, errors.New("private-token") }
	if _, err := readModelCatalog(context.Background(), client, "original", "test"); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("unsafe catalog error")
	}
}

func completeSettingsJob(t *testing.T, application *App, updates <-chan func()) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for application.model.SettingsBusy {
		select {
		case update := <-updates:
			update()
		case <-deadline:
			t.Fatal("settings job did not finish")
		}
	}
}

func TestModelSettingsOnlyUpdatesAfterHostAcknowledgment(t *testing.T) {
	var application *App
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method == "model/list" {
			return json.RawMessage(modelCatalogJSON), nil
		}
		request, ok := params.(protocol.ModelSelectParams)
		if method != "model/select" || !ok || request.Model != "other" || request.Effort != "" {
			t.Fatalf("unexpected model request: %T %+v", params, params)
		}
		if field, present := requestFields(t, params)["effort"]; !present || field != "" {
			t.Fatal("empty effort must be sent to clear the override")
		}
		application.post(func() {
			application.handleNotification("session/event", json.RawMessage(`{"sessionId":"original","seq":0,"event":{"seq":0,"time":1,"type":"model/selection","data":{"provider":"test","model":"other"}}}`))
		})
		return json.RawMessage(`{"session":{"id":"original","title":"","cwd":"C:/work","provider":"test","model":"other","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"nextSeq":1}}`), nil
	}}
	application, updates := sessionTestApp(client)
	application.model.CanModels = true
	application.model.Draft = "keep draft"
	application.OpenSettings()
	completeSettingsJob(t, application, updates)
	application.model.SettingsModel = "other"
	application.ApplySettings()
	application.Send("must not send")
	application.NewSession()
	application.CloseSettings()
	if !application.model.SettingsApplying || application.model.ActiveRoute.Model != "echo" || !application.model.SettingsOpen {
		t.Fatal("model changed optimistically or apply could be dismissed")
	}
	completeSettingsJob(t, application, updates)
	if application.model.ActiveRoute.Model != "other" || application.model.SettingsOpen || application.model.Draft != "keep draft" || application.options.Model != "echo" || application.nextSeq != 1 || len(application.model.Items) != 0 {
		t.Fatal("model scope, draft or durable sequence changed incorrectly")
	}
}

func TestModelSettingsFailurePreservesSelectionAndDraft(t *testing.T) {
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) { return nil, errors.New("private-token") }}
	application, updates := sessionTestApp(client)
	application.model.CanModels = true
	application.model.SetActiveRoute(view.ModelRoute{Provider: "test", Model: "echo"})
	application.model.PrepareSettings(application.model.ActiveRoute)
	application.model.SettingsOpen = true
	application.model.SettingsModel = "bad"
	application.model.Draft = "draft"
	application.ApplySettings()
	completeSettingsJob(t, application, updates)
	if application.model.ActiveRoute.Model != "echo" || application.model.Draft != "draft" || application.model.SettingsModel != "bad" || !application.model.SettingsOpen || application.model.SettingsError == "" || strings.Contains(application.model.SettingsError, "private-token") {
		t.Fatal("failed apply lost state or leaked a secret")
	}
}

func TestClosedSettingsDiscardsLateCatalogAndRespectsNavigationGuards(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) {
		close(started)
		<-finish
		return json.RawMessage(modelCatalogJSON), nil
	}}
	application, updates := sessionTestApp(client)
	application.model.CanModels = true
	application.OpenSettings()
	<-started
	application.OpenSessions()
	if application.model.SessionsOpen {
		t.Fatal("session picker overlapped settings")
	}
	application.CloseSettings()
	close(finish)
	select {
	case update := <-updates:
		update()
	case <-time.After(3 * time.Second):
		t.Fatal("catalog did not return")
	}
	if application.model.SettingsOpen || len(application.model.Catalog.Providers) != 0 {
		t.Fatal("late catalog reopened settings")
	}
	application.model.CanModels = false
	application.OpenSettings()
	if application.model.SettingsOpen {
		t.Fatal("unnegotiated settings opened")
	}
}

func TestModelAcknowledgmentWithoutDurableEventFailsClosed(t *testing.T) {
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) {
		return json.RawMessage(`{"session":{"id":"original","title":"","cwd":"C:/work","provider":"test","model":"other","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"nextSeq":0}}`), nil
	}}
	application, updates := sessionTestApp(client)
	application.model.CanModels = true
	application.model.SettingsOpen = true
	application.model.PrepareSettings(view.ModelRoute{Provider: "test", Model: "other"})
	application.ApplySettings()
	completeSettingsJob(t, application, updates)
	if application.model.Phase != view.PhaseFailed || application.model.ActiveRoute.Model == "other" {
		t.Fatal("acknowledgment without a durable event changed the displayed route")
	}
}

func TestUncertainModelBlocksSendingUntilHostRefresh(t *testing.T) {
	client := &sessionClient{request: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
		if method == "model/list" {
			return json.RawMessage(modelCatalogJSON), nil
		}
		return nil, context.DeadlineExceeded
	}}
	application, updates := sessionTestApp(client)
	application.model.CanModels = true
	application.model.SettingsOpen = true
	application.model.PrepareSettings(view.ModelRoute{Provider: "test", Model: "other"})
	application.ApplySettings()
	completeSettingsJob(t, application, updates)
	application.CloseSettings()
	application.Send("must not send")
	application.NewSession()
	if !application.model.ModelUncertain || application.model.Phase != view.PhaseIdle || application.model.SessionBusy {
		t.Fatal("unconfirmed model allowed sending or navigation")
	}
	application.OpenSettings()
	completeSettingsJob(t, application, updates)
	if application.model.ModelUncertain || application.model.ActiveRoute.Model != "echo" {
		t.Fatal("Host refresh did not reconcile the active route")
	}
}
