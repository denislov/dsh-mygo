package runtimes

import (
	"encoding/json"
	"testing"

	"dsh-mygo/internal/version"
)

func validManifest() Manifest {
	manifest := Manifest{
		SchemaVersion:   1,
		Kind:            "dsh-runtime",
		DSH:             version.DSH,
		Node:            "24.14.0",
		Platform:        CurrentExpected().Platform,
		Arch:            CurrentExpected().Arch,
		ProtocolVersion: version.Protocol,
		HostVersion:     version.App,
		AppMin:          version.AppMin,
		AppMaxExclusive: version.AppMaxExclusive,
		Entry:           entryName,
		HostBundle:      hostBundleName,
	}
	manifest.ID = RuntimeID(manifest)
	return manifest
}

func alternatePlatform(platform string) string {
	if platform == "win32" {
		return "linux"
	}
	return "win32"
}

func TestParseManifest(t *testing.T) {
	manifest := validManifest()
	data, _ := json.Marshal(manifest)
	parsed, err := ParseManifest(data, CurrentExpected())
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if parsed.ID != manifest.ID {
		t.Fatalf("id = %q, want %q", parsed.ID, manifest.ID)
	}
	for _, test := range []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"schema", func(m *Manifest) { m.SchemaVersion = 2 }},
		{"kind", func(m *Manifest) { m.Kind = "other" }},
		{"id", func(m *Manifest) { m.ID = "dsh-wrong" }},
		{"platform", func(m *Manifest) { m.Platform = alternatePlatform(m.Platform) }},
		{"protocol", func(m *Manifest) { m.ProtocolVersion = 99 }},
		{"app range", func(m *Manifest) { m.AppMaxExclusive = "0.0.1" }},
		{"entry", func(m *Manifest) { m.Entry = "../entry" }},
		{"host bundle", func(m *Manifest) { m.HostBundle = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := manifest
			test.mutate(&value)
			if test.name != "id" {
				value.ID = RuntimeID(value)
			}
			data, _ := json.Marshal(value)
			if _, err := ParseManifest(data, CurrentExpected()); err == nil {
				t.Fatal("ParseManifest accepted an invalid manifest")
			}
		})
	}
}

func TestParseCatalogFiltersIncompatibleEntries(t *testing.T) {
	manifest := validManifest()
	entry := CatalogEntry{
		ID: manifest.ID, DSH: manifest.DSH, Node: manifest.Node, Platform: manifest.Platform,
		Arch: manifest.Arch, ProtocolVersion: manifest.ProtocolVersion, HostVersion: manifest.HostVersion,
		AppMin: manifest.AppMin, AppMaxExclusive: manifest.AppMaxExclusive,
		Asset: "dsh-mygo-runtime-test.tgz", SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Size: 42,
	}
	data, _ := json.Marshal(Catalog{SchemaVersion: 1, Runtimes: []CatalogEntry{entry}})
	catalog, err := ParseCatalog(data, CurrentExpected())
	if err != nil {
		t.Fatalf("ParseCatalog: %v", err)
	}
	if len(catalog.Runtimes) != 1 {
		t.Fatalf("catalog entries = %d, want 1", len(catalog.Runtimes))
	}
	incompatible := entry
	incompatible.ID = "dsh-other"
	incompatible.AppMaxExclusive = "0.0.1"
	data, _ = json.Marshal(Catalog{SchemaVersion: 1, Runtimes: []CatalogEntry{incompatible}})
	catalog, err = ParseCatalog(data, CurrentExpected())
	if err != nil {
		t.Fatalf("ParseCatalog incompatible: %v", err)
	}
	if len(catalog.Runtimes) != 0 {
		t.Fatal("incompatible runtime was not filtered")
	}
	entry.Size = 0
	data, _ = json.Marshal(Catalog{SchemaVersion: 1, Runtimes: []CatalogEntry{entry}})
	if _, err := ParseCatalog(data, CurrentExpected()); err == nil {
		t.Fatal("ParseCatalog accepted an invalid size")
	}
}
