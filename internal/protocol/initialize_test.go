package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInitializePayloadsMatchSharedFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "protocol", "initialize.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Request, Response, SDKResponse json.RawMessage
		SDKRequests                    []json.RawMessage
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	request := InitializeParams{ProtocolVersion: 1, Client: ClientInfo{Name: "dsh-mygo", Version: "0.1.1", Platform: "test", Locale: "zh-CN"}, Capabilities: []string{"approval", "session.history"}}
	for _, test := range []struct {
		name  string
		value any
		want  json.RawMessage
	}{
		{"custom", request, fixture.Request},
		{"SDK default effort", SDKInitializeParams{CWD: "C:/work", Provider: "test", Model: "echo"}, fixture.SDKRequests[0]},
		{"SDK explicit effort", SDKInitializeParams{CWD: "C:/work", Provider: "test", Model: "echo", ReasoningEffort: "high"}, fixture.SDKRequests[1]},
		{"SDK required empty strings", SDKInitializeParams{}, json.RawMessage(`{"cwd":"","provider":"","model":""}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(test.want, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("encoded = %s, want %s", data, test.want)
			}
		})
	}
	var response InitializeResult
	if err := json.Unmarshal(fixture.Response, &response); err != nil {
		t.Fatal(err)
	}
	if response.ProtocolVersion != 1 || response.Server != (ServerInfo{Name: "dsh-mygo-host", Version: "0.1.1", DSHVersion: "0.2.1-rc.2", AppMin: "0.1.1", AppMaxExclusive: "0.2.0"}) || !reflect.DeepEqual(response.Capabilities, []string{"approval"}) || response.Attachments != nil {
		t.Fatalf("custom response changed: %+v", response)
	}
	var sdkResponse SDKInitializeResult
	if err := json.Unmarshal(fixture.SDKResponse, &sdkResponse); err != nil {
		t.Fatal(err)
	}
	if sdkResponse.ServerInfo != (SDKServerInfo{Name: "sdk", Version: "1.0.0"}) {
		t.Fatalf("SDK response changed: %+v", sdkResponse)
	}
	if MethodInitialize != "initialize" {
		t.Fatal("initialize method changed")
	}
}
