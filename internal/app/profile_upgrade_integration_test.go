package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/host"
	"dsh-mygo/internal/view"
)

func TestAgainstMygoProfileUpgradeAndSecondStart(t *testing.T) {
	launch, root := realMygoLaunch(t, true)
	var home string
	for _, entry := range launch.Env {
		if strings.HasPrefix(entry, "DSH_HOME=") {
			home = strings.TrimPrefix(entry, "DSH_HOME=")
		}
	}
	if home == "" {
		t.Fatal("missing isolated Host home")
	}
	profile := filepath.Join(home, "profiles", "mygo")
	packagePath := filepath.Join(profile, "node_modules", "@dsh-mygo", "host", "package.json")
	data, err := os.ReadFile(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	var packageInfo map[string]any
	if err := json.Unmarshal(data, &packageInfo); err != nil {
		t.Fatal(err)
	}
	packageInfo["version"] = "0.1.0"
	data, _ = json.Marshal(packageInfo)
	if err := os.WriteFile(packagePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	patchPath := filepath.Join(profile, "cordis.patch.yml")
	patchBefore, _ := os.ReadFile(patchPath)
	bundle := filepath.Join(root, "resources", "host")
	if resources := os.Getenv("DSH_MYGO_RESOURCES_DIR"); resources != "" {
		bundle = filepath.Join(resources, "host")
	}
	state := filepath.Join(t.TempDir(), "editing.json")
	var sessionID string
	for boot := 0; boot < 2; boot++ {
		updates := make(chan func(), 512)
		a := New(Options{Launch: launch, CWD: root, Provider: "mygo-mock", Model: "echo", PreferencesPath: state, Preflight: func() error { return host.EnsureProfile(home, bundle, "mygo") }, Post: func(fn func()) { updates <- fn }})
		go a.Start()
		waitFor(t, a, updates, 90*time.Second, func() bool { return a.model.Phase == view.PhaseIdle || a.model.Phase == view.PhaseFailed })
		if a.model.Phase != view.PhaseIdle {
			a.Shutdown(10 * time.Second)
			t.Fatal("upgraded profile could not start")
		}
		if boot > 0 && (a.model.SessionID != sessionID || a.model.Draft != "upgrade draft") {
			a.Shutdown(10 * time.Second)
			t.Fatalf("second launch: session=%q expected=%q draft=%q", a.model.SessionID, sessionID, a.model.Draft)
		}
		a.model.Draft = "upgrade draft"
		a.DraftChanged()
		sessionID = a.model.SessionID
		a.Shutdown(10 * time.Second)
	}
	data, _ = os.ReadFile(packagePath)
	if json.Unmarshal(data, &packageInfo) != nil || packageInfo["version"] != channel.ReleaseVersion {
		t.Fatal("private Host package version did not follow the app")
	}
	patchAfter, _ := os.ReadFile(patchPath)
	if string(patchBefore) != string(patchAfter) {
		t.Fatal("profile upgrade changed user configuration")
	}
}
