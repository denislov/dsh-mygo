package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dsh-mygo/internal/preferences"
	"dsh-mygo/internal/protocol"
)

func TestUIPreferencesSaveWithDraftAndHandshakeLocale(t *testing.T) {
	a, updates, path := editingTestApp(t, &fakeClient{})
	theme := ""
	locale := ""
	a.options.ApplyTheme = func(value string) { theme = value }
	a.options.ApplyLocale = func(value string) { locale = value }
	a.model.Draft = "keep draft"
	a.model.OpenPreferences()
	a.model.Prefs.Draft.Theme = "dark"
	a.model.Prefs.Draft.Locale = "zh-CN"
	a.model.Prefs.Draft.Send = "Ctrl+Enter"
	a.ApplyUIPreferences()
	if !a.model.SettingsOpen {
		t.Fatal("saving an individual preference closed settings")
	}
	flushEditing(t, a)
	state, _, err := preferences.NewStore(path).Load()
	if err != nil || state.UI.Theme != "dark" || state.UI.Locale != "zh-CN" || state.Drafts["original"] != "keep draft" || theme != "dark" || locale != "zh-CN" {
		t.Fatalf("state=%+v theme=%s err=%v", state, theme, err)
	}
	client := &fakeClient{results: map[string]json.RawMessage{
		"initialize":     json.RawMessage(`{"protocolVersion":1,"server":{"name":"host","version":"0.1.1","dshVersion":"0.2.1-rc.2"},"capabilities":[]}`),
		"session/create": json.RawMessage(`{"session":{"id":"new","status":"idle","nextSeq":0}}`),
	}}
	if _, err := a.initialize(context.Background(), client, ""); err != nil {
		t.Fatal(err)
	}
	if client.params[0].(protocol.InitializeParams).Client.Locale != "zh-CN" {
		t.Fatal("locale did not reach handshake")
	}
	// A restarted app loads locale before queued UI callbacks are drained.
	b := New(Options{PreferencesPath: path, Post: func(fn func()) { updates <- fn }})
	defer b.Shutdown(0)
	b.loadLocalState()
	if b.currentLocale() != "zh-CN" {
		t.Fatal("startup handshake raced queued UI updates")
	}
}

func TestUIPreferenceSaveFailureKeepsDraftAndPreviousFile(t *testing.T) {
	a, updates, path := editingTestApp(t, &fakeClient{})
	a.model.Draft = "last valid"
	a.DraftChanged()
	flushEditing(t, a)
	a.model.Draft = strings.Repeat("x", preferences.MaxDraftBytes+1)
	a.model.OpenPreferences()
	a.model.Prefs.Draft.Theme = "light"
	a.ApplyUIPreferences()
	if err := a.local.writer.Flush(context.Background()); err != preferences.ErrBudget {
		t.Fatalf("budget error=%v", err)
	}
	pumpEditing(t, updates, func() bool { return a.model.LocalState.Error != "" })
	state, _, err := preferences.NewStore(path).Load()
	if err != nil || state.UI.Theme != "system" || state.Drafts["original"] != "last valid" || len(a.model.Draft) != preferences.MaxDraftBytes+1 || a.model.Prefs.Value.Theme != "light" {
		t.Fatal("failed preference save lost editing state or replaced previous file")
	}
}
