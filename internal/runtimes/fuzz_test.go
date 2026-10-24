package runtimes

import (
	"encoding/json"
	"testing"
)

func FuzzParseManifest(f *testing.F) {
	manifest := validManifest()
	seed, _ := json.Marshal(manifest)
	f.Add(seed)
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"schemaVersion":1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseManifest(data, CurrentExpected())
	})
}

func FuzzParseCatalog(f *testing.F) {
	manifest := validManifest()
	entry := CatalogEntry{
		ID: manifest.ID, DSH: manifest.DSH, Node: manifest.Node, Platform: manifest.Platform,
		Arch: manifest.Arch, ProtocolVersion: manifest.ProtocolVersion, HostVersion: manifest.HostVersion,
		AppMin: manifest.AppMin, AppMaxExclusive: manifest.AppMaxExclusive,
		Asset: "dsh-mygo-runtime-test.tgz", SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Size: 42,
	}
	seed, _ := json.Marshal(Catalog{SchemaVersion: 1, Runtimes: []CatalogEntry{entry}})
	f.Add(seed)
	f.Add([]byte(`{"schemaVersion":1,"runtimes":[]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseCatalog(data, CurrentExpected())
	})
}

func FuzzValidateArchivePath(f *testing.F) {
	for _, seed := range []string{"manifest.json", "host/package.json", "../evil", `node\evil.exe`, "node/CON", "a:b", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		_ = validateArchivePath(name)
	})
}
