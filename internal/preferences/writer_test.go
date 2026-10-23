package preferences

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestWriterFlushKeepsNewestSnapshotAndOwnsItsCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	if _, _, err := store.Load(); err != nil {
		t.Fatal(err)
	}
	writer := NewWriter(store, time.Hour, nil)
	defer writer.Stop()
	state := Empty()
	state.Drafts["s"] = "first"
	old := writer.Submit(state)
	state.Drafts["s"] = "last"
	writer.Submit(state)
	state.Drafts["s"] = "mutated by caller"
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := writer.write(old); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := NewStore(path).Load()
	if err != nil || loaded.Drafts["s"] != "last" {
		t.Fatal("stale write or shared map corrupted the newest draft")
	}
}
