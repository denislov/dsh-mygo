package app

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"testing"
	"time"

	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

func TestInitializeRequestEncodingKeepsCustomAndSDKShapes(t *testing.T) {
	caps, err := json.Marshal(protocol.ClientCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, effort, want string
		sdk                bool
	}{
		{"custom", "high", fmt.Sprintf(`{"protocolVersion":%d,"client":{"name":"dsh-mygo","version":%q,"platform":%q,"locale":"zh-CN"},"capabilities":%s}`, channel.ProtocolVersion, channel.ReleaseVersion, runtime.GOOS, caps), false},
		{"SDK default effort", "", `{"cwd":"C:/work","provider":"test","model":"echo"}`, true},
		{"SDK explicit effort", "high", `{"cwd":"C:/work","provider":"test","model":"echo","reasoningEffort":"high"}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeClient{result: json.RawMessage(`{}`)}
			application := New(Options{SDKProtocol: test.sdk, CWD: "C:/work", Provider: "test", Model: "echo", ReasoningEffort: test.effort})
			application.hostLocale.Store("zh-CN")
			// Invalid response stops after the first RPC; inspect the outgoing wire.
			if _, err := application.initialize(context.Background(), client, ""); err == nil {
				t.Fatal("invalid response accepted")
			}
			if calls := client.called(); len(calls) != 1 || calls[0] != "initialize" {
				t.Fatalf("calls = %v", calls)
			}
			data, err := json.Marshal(client.params[0])
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(test.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("request changed: %s, want %s", data, test.want)
			}
		})
	}
}

func TestSDKInitializeResponseCompatibility(t *testing.T) {
	for _, test := range []struct {
		name, wire string
		valid      bool
	}{
		{"name only", `{"serverInfo":{"name":"sdk"}}`, true},
		{"null optional version", `{"serverInfo":{"name":"sdk","version":null}}`, true},
		{"unknown field", `{"serverInfo":{"name":"sdk","future":true}}`, true},
		{"null server info", `{"serverInfo":null}`, false},
		{"null name", `{"serverInfo":{"name":null}}`, false},
		{"empty name", `{"serverInfo":{"name":""}}`, false},
		{"wrong name type", `{"serverInfo":{"name":1}}`, false},
		{"wrong version type", `{"serverInfo":{"name":"sdk","version":1}}`, false},
		{"custom response", `{"server":{"name":"custom"}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeClient{result: json.RawMessage(test.wire)}
			application := New(Options{SDKProtocol: true, SessionID: "sdk-session"})
			result, err := application.initialize(context.Background(), client, "")
			if (err == nil) != test.valid || test.valid && result.sessionID != "sdk-session" {
				t.Fatalf("SDK initialize = %+v, %v", result, err)
			}
		})
	}
}

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
					return json.RawMessage(`{"protocolVersion":1,"server":{"name":"host","version":"0.1.2","dshVersion":"0.2.1-rc.2"},"capabilities":["session.history","settings.configuration"]}`), nil
				case "session/resume":
					fields := requestFields(t, params)
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
				"session/create": json.RawMessage(`{"session":{"id":"new","title":"","cwd":"C:/work","provider":"","model":"","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"nextSeq":0}}`),
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

func TestSessionCreateRequiresTheSharedSummaryContract(t *testing.T) {
	base := map[string]any{
		"id": "new", "title": "", "cwd": "C:/work", "provider": "test", "model": "echo", "effort": "",
		"createdAt": 1, "updatedAt": 1, "status": "idle", "archived": false, "nextSeq": 0,
	}
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
		valid  bool
	}{
		{"complete", func(map[string]any) {}, true},
		{"missing title", func(summary map[string]any) { delete(summary, "title") }, false},
		{"missing cwd", func(summary map[string]any) { delete(summary, "cwd") }, false},
		{"missing archived", func(summary map[string]any) { delete(summary, "archived") }, false},
		{"missing createdAt", func(summary map[string]any) { delete(summary, "createdAt") }, false},
		{"null createdAt", func(summary map[string]any) { summary["createdAt"] = nil }, false},
		{"negative updatedAt", func(summary map[string]any) { summary["updatedAt"] = -1 }, false},
		{"missing nextSeq", func(summary map[string]any) { delete(summary, "nextSeq") }, false},
		{"unsafe nextSeq", func(summary map[string]any) { summary["nextSeq"] = int64(1 << 53) }, false},
		{"stored status", func(summary map[string]any) { summary["status"] = "stored" }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			summary := make(map[string]any, len(base))
			for key, value := range base {
				summary[key] = value
			}
			test.mutate(summary)
			body, err := json.Marshal(map[string]any{"session": summary})
			if err != nil {
				t.Fatal(err)
			}
			client := &fakeClient{results: map[string]json.RawMessage{
				"initialize":     json.RawMessage(`{"protocolVersion":1,"server":{"name":"host","version":"0.1.2","dshVersion":"0.2.1-rc.2"},"capabilities":[]}`),
				"session/create": body,
			}}
			application := New(Options{Provider: "test", Model: "echo"})
			if _, err := application.initializeSession(context.Background(), client, sessionInitialization{}); (err == nil) != test.valid {
				t.Fatalf("initialize error = %v", err)
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
