package preferences

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRoundTripBudgetsAndConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	first := NewStore(path)
	second := NewStore(path)
	if _, _, err := first.Load(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := second.Load(); err != nil {
		t.Fatal(err)
	}
	state := Snapshot{Version: 1, LastSessionID: "s1", Drafts: map[string]string{"s1": "你好\n draft", "s2": "other"}}
	if err := first.Save(state); err != nil {
		t.Fatal(err)
	}
	if err := second.Save(state); err != ErrConflict {
		t.Fatal("another writer silently replaced the file")
	}
	third := NewStore(path)
	loaded, _, err := third.Load()
	if err != nil || loaded.Drafts["s1"] != state.Drafts["s1"] || loaded.LastSessionID != "s1" {
		t.Fatal("state did not survive a new store")
	}
	state.Drafts["s1"] = strings.Repeat("x", MaxDraftBytes+1)
	if err := third.Save(state); err != ErrBudget {
		t.Fatal("oversized draft was not rejected")
	}
	again, _, err := NewStore(path).Load()
	if err != nil || again.Drafts["s1"] != loaded.Drafts["s1"] {
		t.Fatal("failed write changed the last valid state")
	}
}

func TestStoreCorruptionBacksUpOnceAndProtectsNewerVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	corrupt := []byte(`{"version":1,"drafts":broken}`)
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	state, notice, err := store.Load()
	if err != nil || notice == "" || len(state.Drafts) != 0 {
		t.Fatal("corrupt state did not fall back")
	}
	backup, err := os.ReadFile(path + ".corrupt")
	if err != nil || string(backup) != string(corrupt) {
		t.Fatal("original corrupt bytes not backed up")
	}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":2,"future":"keep"}`), 0600); err != nil {
		t.Fatal(err)
	}
	newer := NewStore(path)
	if _, _, err := newer.Load(); err != ErrVersion {
		t.Fatal("unknown version was not protected")
	}
	if err := newer.Save(state); err == nil {
		t.Fatal("newer state overwritten")
	}
}

func TestStoreRejectsInvalidSchemaAndTotalBudget(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"version":1,"lastSessionId":"../bad","drafts":{}}`, `{"version":1,"lastSessionId":"","drafts":null}`, `{"version":1,"lastSessionId":"","drafts":{"s1":null}}`, `{"version":1,"lastSessionId":"","drafts":{},"secret":"bad"}`} {
		if _, err := decode([]byte(raw)); err == nil {
			t.Fatal("invalid schema accepted")
		}
	}
	state := Empty()
	for i := 0; i < 17; i++ {
		state.Drafts[strings.Repeat("s", i+1)] = strings.Repeat("x", MaxDraftBytes)
	}
	if err := Validate(state); err != ErrBudget {
		t.Fatal("total draft limit not enforced")
	}
	if StatePath("root", "home-a", "mygo") == StatePath("root", "home-b", "mygo") || StatePath("root", "home-a", "mygo") == StatePath("root", "home-a", "other") {
		t.Fatal("profiles share draft state")
	}
}

func TestFailedSaveRetainsOldFileAndCanRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "editing.json")
	store := NewStore(path)
	if _, _, err := store.Load(); err != nil {
		t.Fatal(err)
	}
	state := Empty()
	state.Drafts["s"] = "last valid draft"
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	unlock, err := lock(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	state.Drafts["s"] = "pending edit"
	if err := store.Save(state); err != ErrUnavailable {
		unlock()
		t.Fatal("contended write did not fail safely")
	}
	unlock()
	loaded, _, err := NewStore(path).Load()
	if err != nil || loaded.Drafts["s"] != "last valid draft" {
		t.Fatal("failed save replaced the valid file")
	}
	if err := store.Save(state); err != nil {
		t.Fatal("retry failed after contention ended")
	}
}

func TestOversizedCorruptFileIsBackedUpWithoutTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "editing.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(maxFileBytes + 256); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	state, notice, err := NewStore(path).Load()
	if err != nil || notice == "" || len(state.Drafts) != 0 {
		t.Fatal("oversized corrupt state did not fall back")
	}
	info, err := os.Stat(path + ".corrupt")
	if err != nil || info.Size() != maxFileBytes+256 {
		t.Fatal("corrupt backup was truncated")
	}
}
