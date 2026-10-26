package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPermissionPayloadsMatchSharedFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "protocol", "permission.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Get        json.RawMessage `json:"get"`
		Select     json.RawMessage `json:"select"`
		SelectSafe json.RawMessage `json:"selectSafe"`
		Snapshot   json.RawMessage `json:"snapshot"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		value any
		want  json.RawMessage
	}{
		{"get", PermissionGetParams{SessionID: "s1"}, fixture.Get},
		{"select dangerous", PermissionSelectParams{SessionID: "s1", Preset: "danger-full-access", ExpectedNextSeq: 7, ConfirmDangerous: true}, fixture.Select},
		{"select safe", PermissionSelectParams{SessionID: "s1", Preset: "workspace-write", ExpectedNextSeq: 7, ConfirmDangerous: false}, fixture.SelectSafe},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertJSONEqual(t, test.value, test.want)
		})
	}
	var snapshot PermissionSnapshot
	if err := json.Unmarshal(fixture.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	want := PermissionSnapshot{
		SessionID: "s1", Current: "workspace-write", Sandbox: "workspace-write", Approval: "ask", NextSeq: 7,
		Options: []PermissionOption{
			{ID: "workspace-write", Name: "Workspace", Description: "", Sandbox: "workspace-write", Approval: "ask", RequiresConfirmation: false},
			{ID: "danger-full-access", Name: "Full", Description: "", Sandbox: "danger-full-access", Approval: "never", RequiresConfirmation: true},
		},
	}
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("snapshot = %+v, want %+v", snapshot, want)
	}
	if MethodPermissionGet != "permission/get" || MethodPermissionSelect != "permission/select" {
		t.Fatal("permission method names changed")
	}
}
