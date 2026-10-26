package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/view"
)

func selectStartupTestRoute(t *testing.T, application *App, updates <-chan func(), provider string) view.ModelRoute {
	t.Helper()
	application.OpenSettings()
	waitFor(t, application, updates, 20*time.Second, func() bool { return !application.model.SettingsBusy })
	application.model.SettingsModel = "route-probe"
	application.model.SettingsEffort = "Effort: high"
	application.ApplySettings()
	waitFor(t, application, updates, 20*time.Second, func() bool { return !application.model.SettingsBusy })
	want := view.ModelRoute{Provider: provider, Model: "route-probe", Effort: "high"}
	if application.model.SettingsOpen || application.model.ActiveRoute != want {
		t.Fatalf("route selection = %+v; error = %s", application.model.ActiveRoute, application.model.SettingsError)
	}
	return want
}

func TestInitializeUsesCapturedRecoveryRouteWithoutViewModel(t *testing.T) {
	for _, effort := range []string{"", "high"} {
		t.Run("effort="+effort, func(t *testing.T) {
			application := New(Options{Provider: "fallback", Model: "fallback", UseHostDefaults: true})
			application.model.SetActiveRoute(view.ModelRoute{Provider: "selected", Model: "recovery", Effort: effort})
			request := sessionInitialization{resumeID: "saved", route: application.model.ActiveRoute}
			// The worker must need only the captured value, not a live UI model.
			application.model = nil
			client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
				switch method {
				case "initialize":
					return json.RawMessage(`{"protocolVersion":1,"server":{"name":"host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":["session.history","settings.configuration"]}`), nil
				case "session/resume":
					fields := params.(map[string]any)
					if fields["sessionId"] != "saved" || fields["provider"] != "selected" || fields["model"] != "recovery" || fields["useDefaultModel"] != nil {
						t.Errorf("resume ignored captured route: %v", fields)
					}
					if effort == "" && fields["effort"] != nil || effort != "" && fields["effort"] != effort {
						t.Errorf("effort changed: %v", fields)
					}
					return json.RawMessage(fmt.Sprintf(`{"session":{"id":"saved","title":"","cwd":"C:/work","provider":"selected","model":"recovery","effort":%q,"createdAt":1,"updatedAt":1,"status":"idle","archived":false,"nextSeq":0}}`, effort)), nil
				case "session/history":
					return json.RawMessage(`{"events":[],"nextSeq":0,"hasMore":false}`), nil
				default:
					return nil, fmt.Errorf("unexpected request %s", method)
				}
			}}
			initialized, err := application.initializeSession(context.Background(), client, request)
			if err != nil || initialized.route != request.route || initialized.sessionID != "saved" {
				t.Fatalf("initialize = %+v, %v", initialized, err)
			}
		})
	}
}

func TestInitializeChecksIdentityOfThisLaunch(t *testing.T) {
	for _, test := range []struct {
		name     string
		identity channel.HostIdentity
		valid    bool
	}{
		{"selected host", channel.HostIdentity{Version: "0.1.2", DSHVersion: channel.DSHVersion}, true},
		{"different host", channel.HostIdentity{Version: "0.1.3", DSHVersion: channel.DSHVersion}, false},
		{"different DSH", channel.HostIdentity{Version: "0.1.2", DSHVersion: "0.2.1-rc.3"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeClient{results: map[string]json.RawMessage{
				"initialize":     json.RawMessage(`{"protocolVersion":1,"server":{"name":"host","version":"0.1.2","dshVersion":"0.2.1-rc.2","appMin":"0.1.0","appMaxExclusive":"0.2.0"},"capabilities":[]}`),
				"session/create": json.RawMessage(`{"session":{"id":"new","status":"idle","nextSeq":0}}`),
			}}
			application := New(Options{})
			application.model = nil
			_, err := application.initializeSession(context.Background(), client, sessionInitialization{identity: test.identity})
			if (err == nil) != test.valid {
				t.Fatalf("initialize error = %v", err)
			}
			if !test.valid && len(client.called()) != 1 {
				t.Fatal("created a session before identity validation")
			}
		})
	}
}

func TestSendKeepsDraftWhileBusinessStateIsBlocked(t *testing.T) {
	for _, block := range []func(*view.Model){
		func(m *view.Model) { m.Runtime.SetupRequired = true },
		func(m *view.Model) { m.Permission.Uncertain = true },
		func(m *view.Model) { m.History.Detached = true },
	} {
		client := &fakeClient{}
		application := newTestApp(client)
		application.model.Draft = "keep draft"
		block(application.model)
		application.Send(application.model.Draft)
		if len(client.called()) != 0 || application.model.Draft != "keep draft" || application.model.Phase != view.PhaseIdle {
			t.Fatal("command bypassed the shared readiness policy")
		}
	}
}
