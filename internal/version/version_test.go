package version_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dsh-mygo/internal/version"
)

type appCompatibility struct {
	Min          string `json:"min"`
	MaxExclusive string `json:"maxExclusive"`
}

type canonicalFile struct {
	Name             string           `json:"name"`
	Identifier       string           `json:"identifier"`
	Repository       string           `json:"repository"`
	Version          string           `json:"version"`
	ProtocolVersion  int              `json:"protocolVersion"`
	AppCompatibility appCompatibility `json:"appCompatibility"`
	DSH              string           `json:"dsh"`
	Node             string           `json:"node"`
	NodeRuntime      string           `json:"nodeRuntime"`
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
		ProtocolVersion  int              `json:"protocolVersion"`
		AppCompatibility appCompatibility `json:"appCompatibility"`
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

	if want.Name == "" || want.Identifier == "" || want.Repository == "" || want.Version == "" || want.DSH == "" || want.Node == "" || want.NodeRuntime == "" || want.ProtocolVersion <= 0 {
		t.Fatalf("versions.json is incomplete: %+v", want)
	}
	if ok, err := version.AppVersionInRange(want.Version, want.AppCompatibility.Min, want.AppCompatibility.MaxExclusive); err != nil || !ok {
		t.Fatalf("app version %q is outside its own compatibility range %+v: %v", want.Version, want.AppCompatibility, err)
	}
	if strings.HasPrefix(want.Node, ">=") && !strings.HasPrefix(want.NodeRuntime, strings.TrimPrefix(want.Node, ">=")+".") {
		t.Errorf("nodeRuntime %q does not satisfy node requirement %q", want.NodeRuntime, want.Node)
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
	if host.DSHMygo.AppCompatibility != want.AppCompatibility {
		t.Errorf("host app compatibility = %+v, want %+v", host.DSHMygo.AppCompatibility, want.AppCompatibility)
	}
	if version.App != want.Version || version.Protocol != want.ProtocolVersion || version.DSH != want.DSH ||
		version.Repository != want.Repository || version.AppMin != want.AppCompatibility.Min ||
		version.AppMaxExclusive != want.AppCompatibility.MaxExclusive {
		t.Errorf("Go version = {App:%q Protocol:%d DSH:%q Repository:%q Min:%q Max:%q}, want {App:%q Protocol:%d DSH:%q Repository:%q Min:%q Max:%q}",
			version.App, version.Protocol, version.DSH, version.Repository, version.AppMin, version.AppMaxExclusive,
			want.Version, want.ProtocolVersion, want.DSH, want.Repository, want.AppCompatibility.Min, want.AppCompatibility.MaxExclusive)
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

func TestCompareAppVersion(t *testing.T) {
	for _, test := range []struct {
		left, right string
		want        int
		valid       bool
	}{
		{"0.1.1", "0.1.1", 0, true},
		{"0.1.0", "0.1.1", -1, true},
		{"0.2.0", "0.1.99", 1, true},
		{"1.0.0", "0.999.999", 1, true},
		{"0.1", "0.1.0", 0, false},
		{"0.1.1-rc.1", "0.1.1", 0, false},
		{"v0.1.1", "0.1.1", 0, false},
	} {
		got, err := version.CompareAppVersion(test.left, test.right)
		if (err == nil) != test.valid {
			t.Fatalf("CompareAppVersion(%q, %q) error = %v, want valid=%v", test.left, test.right, err, test.valid)
		}
		if err == nil && got != test.want {
			t.Errorf("CompareAppVersion(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestAppVersionInRange(t *testing.T) {
	for _, test := range []struct {
		value, min, max string
		want            bool
	}{
		{"0.1.1", "0.1.1", "0.2.0", true},
		{"0.1.99", "0.1.1", "0.2.0", true},
		{"0.1.0", "0.1.1", "0.2.0", false},
		{"0.2.0", "0.1.1", "0.2.0", false},
	} {
		got, err := version.AppVersionInRange(test.value, test.min, test.max)
		if err != nil || got != test.want {
			t.Errorf("AppVersionInRange(%q, %q, %q) = %v, %v; want %v", test.value, test.min, test.max, got, err, test.want)
		}
	}
}
