package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestConfigurationPayloadsMatchSharedFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "protocol", "configuration.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Get                json.RawMessage `json:"get"`
		DefaultModel       json.RawMessage `json:"defaultModel"`
		DefaultModelEffort json.RawMessage `json:"defaultModelEffort"`
		CredentialSet      json.RawMessage `json:"credentialSet"`
		CredentialUnset    json.RawMessage `json:"credentialUnset"`
		Snapshot           json.RawMessage `json:"snapshot"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	value := "test-value"
	for _, test := range []struct {
		name  string
		value any
		want  json.RawMessage
	}{
		{"get", struct{}{}, fixture.Get},
		{"default model empty effort", ConfigurationDefaultModelParams{Revision: 2, Provider: "test", Model: "saved"}, fixture.DefaultModel},
		{"default model effort", ConfigurationDefaultModelParams{Revision: 2, Provider: "test", Model: "saved", Effort: "high"}, fixture.DefaultModelEffort},
		{"credential set", ConfigurationCredentialParams{Reference: "MYGO_TEST_KEY", Operation: CredentialOperationSet, Value: &value}, fixture.CredentialSet},
		{"credential unset", ConfigurationCredentialParams{Reference: "MYGO_TEST_KEY", Operation: CredentialOperationUnset}, fixture.CredentialUnset},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertJSONEqual(t, test.value, test.want)
		})
	}
	var snapshot ConfigurationSnapshot
	if err := json.Unmarshal(fixture.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	want := ConfigurationSnapshot{
		Revision: 2,
		Defaults: ModelRoute{Provider: "test", Model: "saved", Effort: ""},
		Credential: ConfigurationCredential{
			Available: true, Reference: "MYGO_TEST_KEY", Configured: false, Writable: true, Source: "none",
		},
	}
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("snapshot = %+v, want %+v", snapshot, want)
	}
	if MethodConfigurationGet != "configuration/get" || MethodConfigurationDefaultModel != "configuration/default-model" || MethodConfigurationCredential != "configuration/credential" {
		t.Fatal("configuration method names changed")
	}
	if CredentialOperationSet != "set" || CredentialOperationUnset != "unset" {
		t.Fatal("credential operation names changed")
	}
}
