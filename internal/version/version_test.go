package version_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dsh-mygo/internal/version"
)

type canonicalFile struct {
	Name            string `json:"name"`
	Identifier      string `json:"identifier"`
	Version         string `json:"version"`
	ProtocolVersion int    `json:"protocolVersion"`
	DSH             string `json:"dsh"`
	Node            string `json:"node"`
}

type mygoFile struct {
	Name       string `json:"name"`
	Identifier string `json:"identifier"`
	Version    string `json:"version"`
}

type hostFile struct {
	Version string `json:"version"`
	Engines struct {
		Node string `json:"node"`
		DSH  string `json:"dsh"`
	} `json:"engines"`
	DSHMygo struct {
		ProtocolVersion int `json:"protocolVersion"`
	} `json:"dshMygo"`
	PeerDependencies map[string]string `json:"peerDependencies"`
}

func readJSON[T any](t *testing.T, path string) T {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return value
}

// TestReleaseIdentityIsSingleSourced keeps versions.json authoritative while
// the build files and Go constants mirror it.
func TestReleaseIdentityIsSingleSourced(t *testing.T) {
	root := filepath.Join("..", "..")
	want := readJSON[canonicalFile](t, filepath.Join(root, "versions.json"))
	mygo := readJSON[mygoFile](t, filepath.Join(root, "mygo.json"))
	host := readJSON[hostFile](t, filepath.Join(root, "resources", "host", "package.json"))

	if want.Name == "" || want.Identifier == "" || want.Version == "" || want.DSH == "" || want.Node == "" || want.ProtocolVersion <= 0 {
		t.Fatalf("versions.json is incomplete: %+v", want)
	}
	if strings.HasPrefix(want.Identifier, "com.example.") {
		t.Fatalf("versions.json still uses a placeholder identifier %q", want.Identifier)
	}

	if mygo.Name != want.Name || mygo.Identifier != want.Identifier || mygo.Version != want.Version {
		t.Errorf("mygo.json = %+v, want name %q, identifier %q, version %q", mygo, want.Name, want.Identifier, want.Version)
	}
	if host.Version != want.Version {
		t.Errorf("host version = %q, want %q", host.Version, want.Version)
	}
	if host.Engines.DSH != want.DSH || host.Engines.Node != want.Node {
		t.Errorf("host engines = %+v, want dsh %q, node %q", host.Engines, want.DSH, want.Node)
	}
	if host.DSHMygo.ProtocolVersion != want.ProtocolVersion {
		t.Errorf("host protocolVersion = %d, want %d", host.DSHMygo.ProtocolVersion, want.ProtocolVersion)
	}
	if version.App != want.Version || version.Protocol != want.ProtocolVersion || version.DSH != want.DSH {
		t.Errorf("Go version = {App:%q Protocol:%d DSH:%q}, want {App:%q Protocol:%d DSH:%q}",
			version.App, version.Protocol, version.DSH, want.Version, want.ProtocolVersion, want.DSH)
	}

	foundPinned := false
	for name, pinned := range host.PeerDependencies {
		if !strings.HasPrefix(name, "@deepseek-ai/dsh") {
			continue
		}
		foundPinned = true
		if pinned != want.DSH {
			t.Errorf("peer dependency %s pinned to %q, want %q", name, pinned, want.DSH)
		}
	}
	if !foundPinned {
		t.Error("host package has no @deepseek-ai/dsh peer dependencies to verify")
	}
}
