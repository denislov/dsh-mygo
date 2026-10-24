package runtimes

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type archiveEntry struct {
	name string
	body string
	typ  byte
}

func validRuntimeEntries(t *testing.T) []archiveEntry {
	t.Helper()
	manifest := validManifest()
	manifestData, _ := json.Marshal(manifest)
	host, _ := json.Marshal(map[string]any{
		"name":    "@dsh-mygo/host",
		"version": manifest.HostVersion,
		"engines": map[string]string{"node": ">=24", "dsh": manifest.DSH},
		"dshMygo": map[string]any{
			"protocolVersion": manifest.ProtocolVersion,
			"appCompatibility": map[string]string{
				"min": manifest.AppMin, "maxExclusive": manifest.AppMaxExclusive,
			},
		},
	})
	return []archiveEntry{
		{name: "manifest.json", body: string(manifestData), typ: tar.TypeReg},
		{name: "host/", typ: tar.TypeDir},
		{name: "host/package.json", body: string(host), typ: tar.TypeReg},
		{name: "host/index.mjs", body: "// host\n", typ: tar.TypeReg},
		{name: "mygo-host.mjs", body: "// entry\n", typ: tar.TypeReg},
		{name: "node/", typ: tar.TypeDir},
		{name: "node/node.exe", body: "fake node", typ: tar.TypeReg},
		{name: "node_modules/@deepseek-ai/dsh/package.json", body: `{"version":"` + manifest.DSH + `"}`, typ: tar.TypeReg},
	}
}

func writeArchive(t *testing.T, entries []archiveEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime.tgz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.body)), Typeflag: entry.typ}
		if entry.typ == tar.TypeDir {
			header.Mode = 0o755
			header.Size = 0
		}
		if entry.typ == tar.TypeSymlink {
			header.Linkname = "target"
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.typ == tar.TypeReg && len(entry.body) > 0 {
			if _, err := tarWriter.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	manager := NewManager(t.TempDir(), t.TempDir(), "denislov/dsh-mygo")
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestInstallArchiveAndState(t *testing.T) {
	manager := newTestManager(t)
	archive := writeArchive(t, validRuntimeEntries(t))
	installed, err := manager.InstallArchive(context.Background(), archive, nil)
	if err != nil {
		t.Fatalf("InstallArchive: %v", err)
	}
	if installed.Kind != KindManaged || installed.DSH != validManifest().DSH {
		t.Fatalf("unexpected runtime: %+v", installed)
	}
	if _, err := ValidateInstalled(installed.Dir, CurrentExpected()); err != nil {
		t.Fatalf("ValidateInstalled: %v", err)
	}
	if _, ok := manager.Selected(); ok {
		t.Fatal("install unexpectedly selected the runtime")
	}
	if err := manager.Select(installed.ID); err != nil {
		t.Fatal(err)
	}
	selected, ok := manager.Selected()
	if !ok || selected.ID != installed.ID {
		t.Fatalf("selected = %+v, %v", selected, ok)
	}
	config, hostBundle, _, err := manager.Launch("mygo", t.TempDir(), "")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if config.Command != filepath.Join(installed.Dir, "node", "node.exe") || len(config.Args) != 2 || hostBundle != filepath.Join(installed.Dir, "host") {
		t.Fatalf("unexpected launch config: %+v bundle=%s", config, hostBundle)
	}
	if err := manager.Remove(installed.ID); err == nil {
		t.Fatal("removed the selected runtime")
	}
	second, err := manager.InstallArchive(context.Background(), writeArchive(t, runtimeEntriesWithNode(t, "24.15.0")), nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == installed.ID {
		t.Fatal("second runtime reused the first id")
	}
	if err := manager.Select(second.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Remove(installed.ID); err != nil {
		t.Fatalf("remove unselected runtime: %v", err)
	}
	if _, ok := manager.Find(installed.ID); ok {
		t.Fatal("removed runtime is still in the store")
	}
}

func runtimeEntriesWithNode(t *testing.T, node string) []archiveEntry {
	t.Helper()
	entries := validRuntimeEntries(t)
	var manifest Manifest
	if err := json.Unmarshal([]byte(entries[0].body), &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Node = node
	manifest.ID = RuntimeID(manifest)
	data, _ := json.Marshal(manifest)
	entries[0].body = string(data)
	return entries
}

func TestInstallArchiveRejectsUnsafeEntries(t *testing.T) {
	base := validRuntimeEntries(t)
	for _, test := range []struct {
		name  string
		extra archiveEntry
	}{
		{"traversal", archiveEntry{name: "../evil.txt", body: "x", typ: tar.TypeReg}},
		{"absolute", archiveEntry{name: "/evil.txt", body: "x", typ: tar.TypeReg}},
		{"backslash", archiveEntry{name: `node\evil.exe`, body: "x", typ: tar.TypeReg}},
		{"symlink", archiveEntry{name: "node/link", typ: tar.TypeSymlink}},
		{"reserved", archiveEntry{name: "node/CON", body: "x", typ: tar.TypeReg}},
		{"duplicate case", archiveEntry{name: "node/NODE.EXE", body: "x", typ: tar.TypeReg}},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := newTestManager(t)
			entries := append(append([]archiveEntry{}, base...), test.extra)
			archive := writeArchive(t, entries)
			if _, err := manager.InstallArchive(context.Background(), archive, nil); err == nil {
				t.Fatal("InstallArchive accepted an unsafe archive")
			}
			if _, err := os.Stat(filepath.Join(manager.Root, "evil.txt")); err == nil {
				t.Fatal("unsafe path escaped the staging directory")
			}
		})
	}
}

func TestInstallArchiveRequiresManifestFirst(t *testing.T) {
	entries := validRuntimeEntries(t)
	reordered := append([]archiveEntry{entries[1]}, entries[0])
	reordered = append(reordered, entries[2:]...)
	manager := newTestManager(t)
	if _, err := manager.InstallArchive(context.Background(), writeArchive(t, reordered), nil); err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("InstallArchive error = %v, want manifest error", err)
	}
}
