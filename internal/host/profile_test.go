package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func testBundle(t *testing.T) string {
	t.Helper()
	bundle := t.TempDir()
	files := map[string]string{
		"package.json":     `{"name":"@dsh-mygo/host","version":"0.1.0","type":"module","main":"index.mjs"}`,
		"index.mjs":        "export const name = 'mygo-app-startup'\n",
		"cordis.patch.yml": "[]\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(bundle, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return bundle
}

func TestEnsureProfileCreatesSeed(t *testing.T) {
	home := t.TempDir()
	bundle := testBundle(t)
	if err := EnsureProfile(home, bundle, "mygo"); err != nil {
		t.Fatalf("EnsureProfile: %v", err)
	}

	profile := filepath.Join(home, "profiles", "mygo")
	data, err := os.ReadFile(filepath.Join(profile, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Name    string `json:"name"`
		Private bool   `json:"private"`
		DSH     struct {
			Profile struct {
				Bundles []string `json:"bundles"`
			} `json:"profile"`
		} `json:"dsh"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse profile manifest: %v\n%s", err, data)
	}
	if manifest.Name != "dsh-profile-mygo" || !manifest.Private {
		t.Fatalf("manifest = %+v", manifest)
	}
	for _, bundleName := range []string{"@deepseek-ai/dsh-base", "@dsh-mygo/host"} {
		if !slices.Contains(manifest.DSH.Profile.Bundles, bundleName) {
			t.Fatalf("bundles = %v, want %s", manifest.DSH.Profile.Bundles, bundleName)
		}
	}
	copied := filepath.Join(profile, "node_modules", "@dsh-mygo", "host", "index.mjs")
	if _, err := os.Stat(copied); err != nil {
		t.Fatalf("host bundle was not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(profile, "cordis.patch.yml")); err != nil {
		t.Fatalf("profile patch is missing: %v", err)
	}
}

func TestEnsureProfileDoesNotOverwrite(t *testing.T) {
	home := t.TempDir()
	bundle := testBundle(t)
	if err := EnsureProfile(home, bundle, "mygo"); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, "profiles", "mygo", "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProfile(home, bundle, "mygo"); err != nil {
		t.Fatalf("second EnsureProfile: %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
		t.Fatalf("existing profile was overwritten: %q, %v", data, err)
	}
}

func TestEnsureProfileSkipsExistingWithoutBundle(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, "profiles", "mygo")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "package.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProfile(home, filepath.Join(home, "missing"), "mygo"); err != nil {
		t.Fatalf("existing profile should not require a bundle: %v", err)
	}
}

func TestEnsureProfileRequiresBundle(t *testing.T) {
	home := t.TempDir()
	if err := EnsureProfile(home, filepath.Join(home, "missing"), "mygo"); err == nil {
		t.Fatal("EnsureProfile accepted a missing bundle")
	}
}
