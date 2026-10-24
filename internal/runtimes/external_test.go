package runtimes

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
