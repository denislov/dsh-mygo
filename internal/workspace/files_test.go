package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectFilesRejectEscapesAndKeepDirectoriesFirst(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "src"), 0700)
	os.WriteFile(filepath.Join(root, "hello.md"), []byte("# hello"), 0600)
	entries, err := ReadDir(root, "")
	if err != nil || len(entries) != 2 || !entries[0].Directory {
		t.Fatalf("entries=%v error=%v", entries, err)
	}
	file, err := ReadFile(root, "hello.md")
	if err != nil || file.Text != "# hello" {
		t.Fatalf("file=%v error=%v", file, err)
	}
	for _, path := range []string{"../outside", filepath.Join(t.TempDir(), "secret"), "src/../../outside"} {
		if _, err := ReadFile(root, path); err == nil {
			t.Fatalf("accepted escape %q", path)
		}
	}
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("private"), 0600)
	if os.Symlink(outside, filepath.Join(root, "link")) == nil {
		if _, err := ReadFile(root, "link/secret"); err == nil {
			t.Fatal("followed outside symlink")
		}
	}
}

func TestPreviewRejectsBinaryAndOversizeFiles(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string][]byte{"binary": {0, 1, 2}, "large": []byte(strings.Repeat("x", MaxPreviewBytes+1))} {
		os.WriteFile(filepath.Join(root, name), data, 0600)
		if _, err := ReadFile(root, name); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
}

func TestChangesIncludeUntrackedAndReadOnlyDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "fixture@example.invalid"}, {"config", "user.name", "Fixture"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	path := filepath.Join(root, "note.txt")
	os.WriteFile(path, []byte("before\n"), 0600)
	for _, args := range [][]string{{"add", "note.txt"}, {"commit", "-m", "fixture"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(path, []byte("after\n"), 0600)
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("new\n"), 0600)
	changes, err := ReadChanges(ctx, root)
	if err != nil || len(changes.Files) != 2 {
		t.Fatalf("changes=%v error=%v", changes, err)
	}
	diff, err := ReadDiff(ctx, root, "note.txt")
	if err != nil || !strings.Contains(diff, "-before") || !strings.Contains(diff, "+after") {
		t.Fatalf("diff=%s error=%v", diff, err)
	}
}
