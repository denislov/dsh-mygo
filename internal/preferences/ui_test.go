package preferences

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUIStateMigrationAndValidation(t *testing.T) {
	state, err := decode([]byte(`{"version":1,"lastSessionId":"s1","drafts":{"s1":"你好"}}`))
	if err != nil || state.Version != 2 || state.UI != DefaultUI() || state.Drafts["s1"] != "你好" {
		t.Fatalf("migration: %+v %v", state, err)
	}
	for _, change := range []func(*UISettings){
		func(p *UISettings) { p.Theme = "blue" }, func(p *UISettings) { p.Locale = "xx" },
		func(p *UISettings) { p.Send = "Escape" }, func(p *UISettings) { p.Stop = p.NewSession },
	} {
		p := DefaultUI()
		change(&p)
		if p.Validate() == nil {
			t.Fatalf("invalid settings: %+v", p)
		}
	}
	state.UI.Theme = "dark"
	state.UI.Locale = "zh-CN"
	state.UI.Send = "Ctrl+Enter"
	raw, _ := json.Marshal(state)
	again, err := decode(raw)
	if err != nil || again.UI != state.UI {
		t.Fatalf("roundtrip: %+v %v", again, err)
	}
	if _, err := decode([]byte(`{"version":2,"lastSessionId":"","drafts":{}}`)); err == nil {
		t.Fatal("v2 accepted missing preferences")
	}
	if _, err := decode([]byte(`{"version":1,"lastSessionId":"","drafts":{},"ui":null}`)); err == nil {
		t.Fatal("legacy state accepted an extra null UI field")
	}
	if _, err := decode([]byte(`{"version":3,"future":"preserve"}`)); err != ErrVersion {
		t.Fatal("future version not protected")
	}
}

func TestInvalidUIPreferencesPreserveExistingDraftFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "editing.json")
	state := Empty()
	state.Drafts["s"] = "keep original"
	state.UI.Stop = state.UI.NewSession
	raw, _ := json.Marshal(state)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if _, _, err := store.Load(); err != ErrUI {
		t.Fatalf("invalid UI error: %v", err)
	}
	if err := store.Save(Empty()); err == nil {
		t.Fatal("invalid original overwritten")
	}
	retained, _ := os.ReadFile(path)
	if string(retained) != string(raw) {
		t.Fatal("valid drafts were removed by invalid preferences")
	}
}
