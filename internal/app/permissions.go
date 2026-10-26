package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

func validPermissionKnobs(sandbox, approval string) bool {
	return (sandbox == "read-only" || sandbox == "workspace-write" || sandbox == "danger-full-access") && (approval == "ask" || approval == "never")
}
func decodePermissions(raw json.RawMessage, id string) (view.PermissionSnapshot, error) {
	var snapshot protocol.PermissionSnapshot
	var required struct {
		NextSeq *uint64 `json:"nextSeq"`
		Options []struct {
			RequiresConfirmation *bool   `json:"requiresConfirmation"`
			Description          *string `json:"description"`
		} `json:"options"`
	}
	invalid := errors.New("Invalid session permissions. Refresh to retry")
	if len(raw) > 32*1024 || json.Unmarshal(raw, &required) != nil || required.NextSeq == nil || *required.NextSeq > maxSafeInteger {
		return view.PermissionSnapshot{}, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || snapshot.SessionID != id || !validSessionID(id) || !validModelLabel(snapshot.Current, false) || !validPermissionKnobs(snapshot.Sandbox, snapshot.Approval) || snapshot.Options == nil || len(snapshot.Options) > 32 {
		return view.PermissionSnapshot{}, invalid
	}
	found := snapshot.Current == "custom"
	seen := make(map[string]bool)
	for i, option := range snapshot.Options {
		if option.ID == "custom" || !validModelLabel(option.ID, false) || !validModelLabel(option.Name, false) || len(option.Description) > 2048 || required.Options[i].Description == nil || required.Options[i].RequiresConfirmation == nil || !validPermissionKnobs(option.Sandbox, option.Approval) || seen[option.ID] || option.RequiresConfirmation != (option.Sandbox == "danger-full-access" || option.Approval == "never") {
			return view.PermissionSnapshot{}, invalid
		}
		seen[option.ID] = true
		if option.ID == snapshot.Current {
			found = option.Sandbox == snapshot.Sandbox && option.Approval == snapshot.Approval
		}
	}
	if !found {
		return view.PermissionSnapshot{}, invalid
	}
	return permissionSnapshotView(snapshot), nil
}

func permissionSnapshotView(snapshot protocol.PermissionSnapshot) view.PermissionSnapshot {
	result := view.PermissionSnapshot{
		SessionID: snapshot.SessionID, Current: snapshot.Current, Sandbox: snapshot.Sandbox,
		Approval: snapshot.Approval, NextSeq: snapshot.NextSeq,
		Options: make([]view.PermissionOption, 0, len(snapshot.Options)),
	}
	for _, option := range snapshot.Options {
		result.Options = append(result.Options, view.PermissionOption{
			ID: option.ID, Name: option.Name, Description: option.Description, Sandbox: option.Sandbox,
			Approval: option.Approval, RequiresConfirmation: option.RequiresConfirmation,
		})
	}
	return result
}

func readPermissions(ctx context.Context, client rpcClient, id string) (view.PermissionSnapshot, error) {
	raw, err := client.Call(ctx, protocol.MethodPermissionGet, protocol.PermissionGetParams{SessionID: id})
	if err != nil {
		return view.PermissionSnapshot{}, errors.New("Could not read session permissions. Refresh to retry")
	}
	return decodePermissions(raw, id)
}
func (a *App) ApplyPermission() {
	state := &a.model.Permission
	if !a.canEditSettings() || !a.model.SettingsOpen || !state.Available || !state.Ready || state.Uncertain || a.model.ModelUncertain || a.model.ConfigurationUncertain {
		return
	}
	option := state.SelectedOption()
	if option == nil || option.RequiresConfirmation && !state.Confirm {
		return
	}
	expected := *option
	before := state.Snapshot
	changed := before.Current != expected.ID || before.Sandbox != expected.Sandbox || before.Approval != expected.Approval
	params := protocol.PermissionSelectParams{
		SessionID: a.sessionID, Preset: option.ID, ExpectedNextSeq: a.nextSeq, ConfirmDangerous: state.Confirm,
	}
	state.Confirm = false
	ctx, operation, cancel := a.beginSettingsOperationTimeout(60 * time.Second)
	a.model.SettingsApplying = true
	a.model.Permission.Writing = true
	client, id, startSeq := a.currentClient(), a.sessionID, a.nextSeq
	go func() {
		defer cancel()
		raw, err := client.Call(ctx, protocol.MethodPermissionSelect, params)
		var snapshot view.PermissionSnapshot
		if err == nil {
			snapshot, err = decodePermissions(raw, id)
		}
		if err == nil && (snapshot.Current != expected.ID || snapshot.Sandbox != expected.Sandbox || snapshot.Approval != expected.Approval || snapshot.NextSeq < startSeq || changed && snapshot.NextSeq == startSeq) {
			err = errors.New("Permission selection was not confirmed")
		}
		a.post(func() {
			if !a.currentSettingsOperation(operation, id) {
				return
			}
			a.cancelSettingsOperation()
			if err != nil {
				a.model.Permission.Uncertain = true
				a.model.SettingsError = msgPermissionsUnconfirmed
				return
			}
			if snapshot.NextSeq != a.nextSeq {
				a.setFailure(msgPermissionsEventsMissing, errors.New("Restart the host to recover the durable session"))
				go a.stopHostInstance(a.currentHost())
				return
			}
			a.model.Permission.Accept(snapshot)
			a.model.SettingsNotice = msgPermissionsSaved
			if a.model.QuickSelector == "permission" {
				a.model.SettingsOpen = false
				a.model.QuickSelector = ""
			}
		})
	}()
}
