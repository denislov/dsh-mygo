package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestProjectFolderCreatesSessionAndKeepsOldDraft(t *testing.T) {
	folder := t.TempDir()
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method == "session/create" {
			if params.(map[string]any)["cwd"] != folder {
				t.Error("wrong project folder")
			}
			return json.Marshal(map[string]any{"session": map[string]any{"id": "project", "title": "", "cwd": folder, "provider": "test", "model": "echo", "effort": "", "createdAt": 1, "updatedAt": 1, "archived": false, "status": "idle", "nextSeq": 0}})
		}
		return json.RawMessage(`{"events":[],"nextSeq":0,"hasMore":false}`), nil
	}}
	a, updates := sessionTestApp(client)
	a.model.Setup.WorkspaceRequired, a.model.Draft = true, "old draft"
	a.options.PickDirectory = func(string) (string, error) { return folder, nil }
	a.ChooseProjectFolder()
	waitFor(t, a, updates, 3*time.Second, func() bool { return !a.model.Setup.Picking && !a.model.SessionBusy })
	if a.sessionID != "project" || a.model.SessionCWD != folder || a.model.Setup.WorkspaceRequired || a.drafts["original"] != "old draft" {
		t.Fatal("folder change did not preserve session boundary")
	}
	a.NewSession()
	completeSessionJob(t, a, updates)
}

func TestProjectFolderCancellationFailureAndLateReplyKeepSession(t *testing.T) {
	for _, scenario := range []string{"cancel", "failure", "late", "not-directory"} {
		t.Run(scenario, func(t *testing.T) {
			a, updates := sessionTestApp(&fakeClient{})
			a.model.Draft = "keep"
			a.options.PickDirectory = func(string) (string, error) {
				if scenario == "failure" {
					return "", errors.New("private-marker")
				}
				if scenario == "not-directory" {
					return filepath.Join(t.TempDir(), "missing"), nil
				}
				return "", nil
			}
			a.ChooseProjectFolder()
			if scenario == "late" {
				a.hostGeneration.Add(1)
				a.model.Setup.Picking = false
				select {
				case update := <-updates:
					update()
				case <-time.After(3 * time.Second):
					t.Fatal("missing picker result")
				}
			}
			waitFor(t, a, updates, 3*time.Second, func() bool { return !a.model.Setup.Picking })
			if a.sessionID != "original" || a.model.Draft != "keep" {
				t.Fatal("picker changed the old session")
			}
		})
	}
}

func TestConnectionSetupReadsCredentialsWithoutModelCatalog(t *testing.T) {
	client := &sessionClient{request: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
		if method != "configuration/get" {
			t.Errorf("connection requested %s", method)
		}
		return json.RawMessage(configurationJSON), nil
	}}
	a, updates := sessionTestApp(client)
	a.model.CanConfigure, a.model.CanModels = true, true
	a.model.ModelUncertain = true
	a.model.Draft = "keep"
	a.OpenConnectionSettings()
	waitFor(t, a, updates, 3*time.Second, func() bool { return !a.model.SettingsBusy })
	if !a.model.ConfigurationReady || !a.model.SettingsConnection || !a.model.ModelUncertain || a.model.Draft != "keep" {
		t.Fatal("connection setup changed unrelated session state")
	}
	a.CloseSettings()
	if a.model.SettingsConnection || a.model.SettingsOpen {
		t.Fatal("connection modal stayed open")
	}
}
