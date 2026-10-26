package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlainBuildsDoNotRunBusinessActions(t *testing.T) {
	model := sessionModel()
	calls := 0
	model.DraftChanged = func() { calls++ }
	model.Send = func(string) { calls++ }
	model.Cancel = func() { calls++ }
	model.SwitchSession = func(string) { calls++ }
	model.SaveCredential = func() { calls++ }
	tester := tester(model)
	tester.Frame()
	tester.Frame()
	if calls != 0 {
		t.Fatalf("plain builds ran %d business actions", calls)
	}
}

func TestSuggestionPersistsOnceAtBuildEnd(t *testing.T) {
	model := sessionModel()
	model.Send = func(string) {}
	persisted := 0
	model.DraftChanged = func() { persisted++ }
	tester := tester(model)
	if err := tester.Click("Summarize this project"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if model.Draft != "Summarize this project" || persisted != 1 {
		t.Fatalf("draft=%q persisted=%d", model.Draft, persisted)
	}
	tester.Frame()
	if persisted != 1 {
		t.Fatalf("repeat build repeated persistence: %d", persisted)
	}
}

func TestViewSourcesDoNotUseFrameMemo(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatal("view sources missing")
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(source), ".Memo(") {
			t.Fatalf("%s uses the unavailable Context.Memo API", path)
		}
	}
}
