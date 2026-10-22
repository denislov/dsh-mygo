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

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("// test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveLaunchBundledHost(t *testing.T) {
	resources := t.TempDir()
	runtimeDir := filepath.Join(resources, "runtime")
	hostEntry := filepath.Join(runtimeDir, "mygo-host.mjs")
	node := filepath.Join(runtimeDir, "node", "node.exe")
	writeTestFile(t, hostEntry)
	writeTestFile(t, node)

	config, err := ResolveLaunch(LaunchOptions{ResourcesDir: resources, Profile: "mygo", CWD: "C:/work"})
	if err != nil {
		t.Fatalf("ResolveLaunch: %v", err)
	}
	if config.Command != node {
		t.Fatalf("command = %q, want %q", config.Command, node)
	}
	if len(config.Args) != 2 || config.Args[0] != hostEntry || config.Args[1] != "mygo" {
		t.Fatalf("args = %v", config.Args)
	}
	if config.Dir != "C:/work" {
		t.Fatalf("dir = %q", config.Dir)
	}
	wantPrefix := filepath.Dir(node) + string(os.PathListSeparator)
	found := false
	for _, entry := range config.Env {
		if strings.HasPrefix(strings.ToUpper(entry), "PATH=") {
			found = true
			if !strings.HasPrefix(entry[len("PATH="):], wantPrefix) {
				t.Fatalf("PATH = %q, want prefix %q", entry, wantPrefix)
			}
		}
	}
	if !found {
		t.Fatal("bundled host did not set PATH")
	}
}

func TestResolveLaunchBundledHostMissingNode(t *testing.T) {
	resources := t.TempDir()
	writeTestFile(t, filepath.Join(resources, "runtime", "mygo-host.mjs"))

	_, err := ResolveLaunch(LaunchOptions{ResourcesDir: resources, Profile: "mygo", CWD: "C:/work"})
	if err == nil || !strings.Contains(err.Error(), "missing Node") {
		t.Fatalf("err = %v, want missing Node", err)
	}
}

func TestResolveLaunchBundledDshFallback(t *testing.T) {
	resources := t.TempDir()
	runtimeDir := filepath.Join(resources, "runtime")
	node := filepath.Join(runtimeDir, "node", "node.exe")
	dshEntry := filepath.Join(runtimeDir, "node_modules", "@deepseek-ai", "dsh", "lib", "bin.js")
	writeTestFile(t, node)
	writeTestFile(t, dshEntry)

	config, err := ResolveLaunch(LaunchOptions{ResourcesDir: resources, Profile: "mygo", CWD: "C:/work"})
	if err != nil {
		t.Fatalf("ResolveLaunch: %v", err)
	}
	if config.Command != node {
		t.Fatalf("command = %q, want %q", config.Command, node)
	}
	if len(config.Args) != 3 || config.Args[0] != dshEntry || config.Args[1] != "--profile" || config.Args[2] != "mygo" {
		t.Fatalf("args = %v", config.Args)
	}
}

func TestResolveLaunchBundledRequiresProfile(t *testing.T) {
	resources := t.TempDir()
	writeTestFile(t, filepath.Join(resources, "runtime", "mygo-host.mjs"))
	writeTestFile(t, filepath.Join(resources, "runtime", "node", "node.exe"))

	if _, err := ResolveLaunch(LaunchOptions{ResourcesDir: resources, CWD: "C:/work"}); err == nil {
		t.Fatal("ResolveLaunch accepted a bundled host without a profile")
	}
}

func TestResolveLaunchOverridesBeatBundled(t *testing.T) {
	resources := t.TempDir()
	writeTestFile(t, filepath.Join(resources, "runtime", "mygo-host.mjs"))
	writeTestFile(t, filepath.Join(resources, "runtime", "node", "node.exe"))

	override, err := ResolveLaunch(LaunchOptions{ResourcesDir: resources, Profile: "mygo", CWD: "C:/work", Command: "custom"})
	if err != nil {
		t.Fatalf("command override: %v", err)
	}
	if override.Command != "custom" {
		t.Fatalf("command = %q, want custom", override.Command)
	}

	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "apps", "cli", "src", "bin.ts"))
	checkout, err := ResolveLaunch(LaunchOptions{ResourcesDir: resources, DshDir: source, Profile: "mygo", CWD: "C:/work"})
	if err != nil {
		t.Fatalf("dsh-dir override: %v", err)
	}
	if checkout.Command != "node" || !strings.Contains(strings.Join(checkout.Args, " "), "tsx/esm") {
		t.Fatalf("dsh-dir launch = %+v", checkout)
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
