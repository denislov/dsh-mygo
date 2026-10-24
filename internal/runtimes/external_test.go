package runtimes

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"dsh-mygo/internal/version"
)

func TestProbeExternalVersions(t *testing.T) {
	dir := t.TempDir()
	path := os.Getenv("PATH")
	if runtime.GOOS == "windows" {
		writeScript(t, filepath.Join(dir, "node.cmd"), "v24.14.0")
		writeScript(t, filepath.Join(dir, "dsh.cmd"), "0.2.1-rc.2")
		path = dir + string(os.PathListSeparator) + path
	} else {
		writeScript(t, filepath.Join(dir, "node"), "v24.14.0")
		writeScript(t, filepath.Join(dir, "dsh"), "0.2.1-rc.2")
		path = dir + string(os.PathListSeparator) + path
	}
	t.Setenv("PATH", path)
	result, err := ProbeExternal(context.Background(), "")
	if err != nil {
		t.Fatalf("ProbeExternal: %v", err)
	}
	if !result.Compatible || result.DSHVersion != "0.2.1-rc.2" || result.NodeVersion != "24.14.0" {
		t.Fatalf("unexpected probe: %+v", result)
	}
	if !strings.HasPrefix(result.Runtime().ID, "external-") {
		t.Fatalf("runtime id = %q", result.Runtime().ID)
	}
}

func TestProbeExternalReportsIncompatibleVersions(t *testing.T) {
	dir := t.TempDir()
	path := os.Getenv("PATH")
	if runtime.GOOS == "windows" {
		writeScript(t, filepath.Join(dir, "node.cmd"), "v22.0.0")
		writeScript(t, filepath.Join(dir, "dsh.cmd"), "0.1.0")
	} else {
		writeScript(t, filepath.Join(dir, "node"), "v22.0.0")
		writeScript(t, filepath.Join(dir, "dsh"), "0.1.0")
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+path)
	result, err := ProbeExternal(context.Background(), "")
	if err != nil {
		t.Fatalf("ProbeExternal: %v", err)
	}
	if result.Compatible || result.Reason == "" {
		t.Fatalf("expected an incompatible result: %+v", result)
	}
}

func TestLaunchRuntimeRepairsMissingExternalShim(t *testing.T) {
	dir := t.TempDir()
	nodeName, dshName := "node", "dsh"
	if runtime.GOOS == "windows" {
		nodeName, dshName = "node.cmd", "dsh.cmd"
	}
	writeScript(t, filepath.Join(dir, nodeName), "v24.14.0")
	writeScript(t, filepath.Join(dir, dshName), version.DSH)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager := NewManager(t.TempDir(), t.TempDir(), "denislov/dsh-mygo")
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	external := External{
		Command: filepath.Join(dir, "removed-shim.cmd"), Node: filepath.Join(dir, "removed-node.exe"),
		DSHVersion: version.DSH, NodeVersion: "24.14.0", Compatible: true,
	}
	installed, err := manager.AddExternal(external)
	if err != nil {
		t.Fatal(err)
	}
	config, _, repaired, err := manager.LaunchRuntime(installed, "mygo", dir, "")
	if err != nil {
		t.Fatalf("LaunchRuntime: %v", err)
	}
	if repaired.Command != stablePath(filepath.Join(dir, dshName)) || !fileExists(repaired.Command) {
		t.Fatalf("repaired command = %q", repaired.Command)
	}
	if config.Command != "cmd.exe" && runtime.GOOS == "windows" {
		t.Fatalf("config command = %q", config.Command)
	}
	stored, ok := manager.Find(installed.ID)
	if !ok || stored.Command != repaired.Command {
		t.Fatalf("stored runtime = %+v, %v", stored, ok)
	}
}

func TestStablePathResolvesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.cmd")
	if err := os.WriteFile(target, []byte("@echo ok\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "shim.cmd")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	if got := stablePath(link); got != resolved {
		t.Fatalf("stablePath = %q, want %q", got, resolved)
	}
}

func writeScript(t *testing.T, path, version string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(path, []byte("@echo "+version+"\r\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho "+version+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
