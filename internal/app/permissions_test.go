package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"dsh-mygo/internal/protocol"
)

const permissionJSON = `{"sessionId":"original","current":"workspace-write","sandbox":"workspace-write","approval":"ask","nextSeq":0,"options":[{"id":"workspace-write","name":"Workspace","description":"","sandbox":"workspace-write","approval":"ask","requiresConfirmation":false},{"id":"danger-full-access","name":"Full","description":"","sandbox":"danger-full-access","approval":"never","requiresConfirmation":true}]}`

func TestPermissionSnapshotValidatesIdentityKnobsAndConfirmation(t *testing.T) {
	if _, err := decodePermissions(json.RawMessage(permissionJSON), "original"); err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{`{}`, strings.Replace(permissionJSON, `"original"`, `"foreign"`, 1), strings.Replace(permissionJSON, `"nextSeq":0`, `"nextSeq":null`, 1), strings.Replace(permissionJSON, `"requiresConfirmation":true`, `"requiresConfirmation":false`, 1), strings.Replace(permissionJSON, `"requiresConfirmation":true`, `"requiresConfirmation":null`, 1), strings.Replace(permissionJSON, `"description":""`, `"description":null`, 1), strings.Replace(permissionJSON, `"sandbox":"workspace-write"`, `"sandbox":"unknown"`, 1), strings.Replace(permissionJSON, `"options":`, `"secret":"private-token","options":`, 1)} {
		if _, err := decodePermissions(json.RawMessage(payload), "original"); err == nil {
			t.Fatal("invalid permission snapshot accepted")
		}
	}
}
func TestPermissionApplyRequiresConfirmationAndFailedWriteMustRefresh(t *testing.T) {
	writes := 0
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method == protocol.MethodPermissionGet {
			request, ok := params.(protocol.PermissionGetParams)
			if !ok || request.SessionID != "original" {
				t.Errorf("permission read = %T %+v", params, params)
			}
			return json.RawMessage(permissionJSON), nil
		}
		writes++
		request, ok := params.(protocol.PermissionSelectParams)
		if method != protocol.MethodPermissionSelect || !ok || !request.ConfirmDangerous || request.Preset != "danger-full-access" {
			t.Errorf("permission selection = %T %+v", params, params)
		}
		return nil, errors.New("private-token")
	}}
	a, updates := sessionTestApp(client)
	a.model.Permission.Available = true
	snapshot, _ := decodePermissions(json.RawMessage(permissionJSON), "original")
	a.model.Permission.Accept(snapshot)
	a.model.Permission.Selected = "danger-full-access"
	a.model.SettingsOpen = true
	a.model.Draft = "draft"
	a.ApplyPermission()
	if writes != 0 || a.model.SettingsBusy {
		t.Fatal("missing confirmation started a write")
	}
	a.model.Permission.Confirm = true
	a.ApplyPermission()
	completeSettingsJob(t, a, updates)
	if !a.model.Permission.Uncertain || strings.Contains(a.model.SettingsError, "private-token") || a.model.Draft != "draft" || a.model.Permission.Confirm {
		t.Fatal("failed permission write lost state or exposed secret")
	}
	a.CloseSettings()
	a.Send("blocked")
	a.NewSession()
	if writes != 1 || a.model.SessionBusy || a.model.Draft != "draft" {
		t.Fatal("uncertainty failed to block work")
	}
	a.OpenSettings()
	completeSettingsJob(t, a, updates)
	if a.model.Permission.Uncertain || !a.model.Permission.Ready {
		t.Fatal("refresh did not confirm permissions")
	}
}

func TestPermissionApplyRejectsChangedModeWithoutCommittedEvents(t *testing.T) {
	payload := strings.Replace(permissionJSON, `"current":"workspace-write","sandbox":"workspace-write","approval":"ask"`, `"current":"danger-full-access","sandbox":"danger-full-access","approval":"never"`, 1)
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) { return json.RawMessage(payload), nil }}
	a, updates := sessionTestApp(client)
	a.model.Permission.Available = true
	snapshot, _ := decodePermissions(json.RawMessage(permissionJSON), "original")
	a.model.Permission.Accept(snapshot)
	a.model.Permission.Selected = "danger-full-access"
	a.model.Permission.Confirm = true
	a.model.SettingsOpen = true
	a.ApplyPermission()
	completeSettingsJob(t, a, updates)
	if !a.model.Permission.Uncertain || a.model.Permission.Snapshot.Current != "workspace-write" {
		t.Fatal("uncommitted permission replaced confirmed state")
	}
}
