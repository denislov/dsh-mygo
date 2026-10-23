package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestListMentionCandidatesOrdersDirectoriesFirst(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"notes.txt", "src", ".hidden", "README.md"} {
		if name == "src" {
			if err := os.Mkdir(filepath.Join(directory, name), 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(filepath.Join(directory, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := listMentionCandidates(directory, "")
	want := []string{"src/", "notes.txt", "README.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v", got)
	}
	if hidden := listMentionCandidates(directory, ".h"); !reflect.DeepEqual(hidden, []string{".hidden"}) {
		t.Fatalf("hidden = %v", hidden)
	}
	if nested := listMentionCandidates(directory, "src/"); len(nested) != 0 {
		t.Fatalf("nested = %v", nested)
	}
}
