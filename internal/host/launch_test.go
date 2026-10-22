package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLaunchOverride(t *testing.T) {
	config, err := ResolveLaunch(LaunchOptions{
		CWD:     "C:/work",
		Command: "node",
		Args:    []string{"host.mjs"},
	})
	if err != nil {
		t.Fatalf("ResolveLaunch: %v", err)
	}
	if config.Command != "node" || len(config.Args) != 1 || config.Args[0] != "host.mjs" {
		t.Fatalf("config = %+v", config)
	}
	if config.Dir != "C:/work" {
		t.Fatalf("dir = %q", config.Dir)
	}
}

func TestResolveLaunchSourceCheckout(t *testing.T) {
	directory := t.TempDir()
	entry := filepath.Join(directory, "apps", "cli", "src", "bin.ts")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("// entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config, err := ResolveLaunch(LaunchOptions{DshDir: directory, Profile: "sdk", CWD: "C:/work"})
	if err != nil {
		t.Fatalf("ResolveLaunch: %v", err)
	}
	if config.Command != "node" {
		t.Fatalf("command = %q", config.Command)
	}
	joined := strings.Join(config.Args, " ")
	if !strings.Contains(joined, "--import tsx/esm") || !strings.Contains(joined, "--profile sdk") {
		t.Fatalf("args = %v", config.Args)
	}
	if config.Dir != directory {
		t.Fatalf("dir = %q, want %q", config.Dir, directory)
	}
}

func TestResolveLaunchMissingSourceEntry(t *testing.T) {
	if _, err := ResolveLaunch(LaunchOptions{DshDir: t.TempDir(), Profile: "sdk", CWD: "C:/work"}); err == nil {
		t.Fatal("ResolveLaunch accepted a checkout without an entry")
	}
}

func TestResolveLaunchRequiresProfile(t *testing.T) {
	if _, err := ResolveLaunch(LaunchOptions{CWD: "C:/work"}); err == nil {
		t.Fatal("ResolveLaunch accepted a host without a profile")
	}
}

func TestSetEnvReplacesCaseInsensitively(t *testing.T) {
	environment := []string{"PATH=/bin", "dsh_home=/old", "HOME=/home"}
	updated := setEnv(environment, "DSH_HOME", "/new")
	joined := strings.Join(updated, "\n")
	if strings.Contains(joined, "/old") {
		t.Fatalf("old DSH_HOME retained: %v", updated)
	}
	if !strings.Contains(joined, "DSH_HOME=/new") {
		t.Fatalf("new DSH_HOME missing: %v", updated)
	}
}
