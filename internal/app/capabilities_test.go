package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/protocol"
)

func TestInitializePreservesCapabilitiesForCreateAndResume(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "protocol", "capabilities.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ ClientCapabilities []string }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, resumeID := range []string{"", "original"} {
		t.Run("resume="+resumeID, func(t *testing.T) {
			wire := channel.InitializeResult{ProtocolVersion: channel.ProtocolVersion, Capabilities: append(fixture.ClientCapabilities, "future.test")}
			wire.Server.Name, wire.Server.Version, wire.Server.DSHVersion = "host", channel.ReleaseVersion, channel.DSHVersion
			wire.Attachments = &channel.AttachmentPolicy{Image: *testLimits()}
			client := &sessionClient{request: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
				switch method {
				case "initialize":
					return json.Marshal(wire)
				case "session/create", "session/resume":
					if (method == "session/resume") != (resumeID != "") {
						t.Errorf("unexpected initialization path: %s", method)
					}
					return json.RawMessage(`{"session":{"id":"original","title":"","cwd":"C:/work","provider":"test","model":"echo","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"nextSeq":0}}`), nil
				case "permission/get":
					return json.RawMessage(permissionJSON), nil
				case "session/history":
					return json.RawMessage(`{"events":[],"startSeq":0,"endSeq":0,"nextSeq":0,"hasMore":false}`), nil
				default:
					return nil, fmt.Errorf("unexpected request %s", method)
				}
			}}
			application := New(Options{Provider: "test", Model: "echo"})
			initialized, err := application.initializeSession(context.Background(), client, sessionInitialization{resumeID: resumeID})
			if err != nil {
				t.Fatal(err)
			}
			if initialized.caps != protocol.ParseCapabilities(fixture.ClientCapabilities) || !initialized.caps.StreamBaselineReasoning {
				t.Fatalf("initialization lost negotiated capabilities: %+v", initialized.caps)
			}
			if initialized.permissions == nil || initialized.attachments == nil {
				t.Fatal("negotiated policy was lost")
			}
		})
	}
}

func TestInitializeDoesNotEnableUnnegotiatedCapabilities(t *testing.T) {
	for _, test := range []struct {
		name      string
		sdk       bool
		handshake string
		want      protocol.Capabilities
	}{
		{"SDK", true, `{"serverInfo":{"name":"sdk"}}`, protocol.Capabilities{}},
		{"future capability", false, `{"protocolVersion":1,"server":{"name":"host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":["future.test"]}`, protocol.Capabilities{}},
		{"minimal host", false, `{"protocolVersion":1,"server":{"name":"host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":["approval"]}`, protocol.Capabilities{Approval: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeClient{results: map[string]json.RawMessage{
				"initialize":     json.RawMessage(test.handshake),
				"session/create": json.RawMessage(`{"session":{"id":"new","title":"","cwd":"C:/work","provider":"","model":"","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"nextSeq":0}}`),
			}}
			application := New(Options{SDKProtocol: test.sdk})
			initialized, err := application.initializeSession(context.Background(), client, sessionInitialization{})
			if err != nil || initialized.caps != test.want || initialized.attachments != nil || initialized.permissions != nil {
				t.Fatalf("unnegotiated feature enabled: %+v, %v", initialized, err)
			}
		})
	}
}
