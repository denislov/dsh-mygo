package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/view"
)

func TestAgainstMygoConfiguration(t *testing.T) {
	for _, mode := range []string{"fake", "real"} {
		t.Run(mode, func(t *testing.T) {
			cwd := t.TempDir()
			var launch host.Config
			provider := "test"
			if mode == "real" {
				launch, _ = realMygoLaunch(t, true)
				provider = "mygo-mock"
				launch.Dir = cwd
				launch.Args[1] = "file:///" + filepath.ToSlash(filepath.Join(os.Getenv("DSH_MYGO_DSH_DIR"), "node_modules", "tsx", "dist", "esm", "index.mjs"))
				patch := filepath.Join(t.TempDir(), "isolated-credential.patch.yml")
				if err := os.WriteFile(patch, []byte(`[{"id":"llm-deepseek","config":{"apiKeyEnv":"MYGO_TEST_ONLY_KEY"}}]`), 0o600); err != nil {
					t.Fatal(err)
				}
				launch.Args = append(launch.Args, "--patch", patch)
				filtered := make([]string, 0, len(launch.Env))
				for _, entry := range launch.Env {
					if !strings.HasPrefix(strings.ToUpper(entry), "MYGO_TEST_ONLY_KEY=") && !strings.HasPrefix(strings.ToUpper(entry), "DEEPSEEK_API_KEY=") {
						filtered = append(filtered, entry)
					}
				}
				launch.Env = append(filtered, "TSX_TSCONFIG_PATH="+filepath.Join(os.Getenv("DSH_MYGO_DSH_DIR"), "tsconfig.json"))
			} else {
				node, err := exec.LookPath("node")
				if err != nil {
					t.Skip("node is not installed")
				}
				fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
				if err != nil {
					t.Fatal(err)
				}
				launch = host.Config{Command: node, Args: []string{fixture, "--sessions", "--models", "--configuration"}, Dir: cwd}
			}
			updates := make(chan func(), 512)
			initialUpdates := updates
			application := New(Options{Launch: launch, CWD: cwd, Provider: provider, Model: "echo", Post: func(update func()) { initialUpdates <- update }})
			defer application.Shutdown(10 * time.Second)
			go application.Start()
			waitFor(t, application, updates, 90*time.Second, func() bool { return application.model.Phase == view.PhaseIdle })
			if !application.model.CanConfigure {
				t.Fatal("configuration was not negotiated")
			}
			originalID := application.sessionID
			application.OpenSettings()
			waitFor(t, application, updates, 30*time.Second, func() bool { return !application.model.SettingsBusy })
			if !application.model.ConfigurationReady || application.model.Configuration.Credential.Reference != "MYGO_TEST_ONLY_KEY" || application.model.Configuration.Credential.Configured {
				t.Fatalf("isolated configuration was not readable: %s", application.model.SettingsError)
			}
			application.model.SettingsModel = "route-probe"
			application.model.SettingsEffort = "Effort: high"
			application.SaveDefaultModel()
			waitFor(t, application, updates, 30*time.Second, func() bool { return !application.model.SettingsBusy })
			if application.model.SettingsError != "" || application.model.ActiveRoute.Model != "echo" || application.model.Configuration.Defaults.Effort != "high" {
				t.Fatalf("default was not saved independently: %s", application.model.SettingsError)
			}
			application.CloseSettings()
			application.options.UseHostDefaults = true
			application.NewSession()
			waitFor(t, application, updates, 30*time.Second, func() bool { return !application.model.SessionBusy })
			if application.model.ActiveRoute.Model != "route-probe" || application.model.ActiveRoute.Effort != "high" {
				t.Fatal("new session did not use Host defaults")
			}
			application.Send("configuration route probe")
			waitFor(t, application, updates, 30*time.Second, func() bool {
				return application.model.Phase == view.PhaseIdle && hasAssistantText(application.model.Items, "Selected "+provider+"/route-probe/high")
			})
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			fakeKey := "mygo-fake-key-for-isolated-tests-only"
			raw, err := application.currentClient().Call(ctx, "configuration/credential", map[string]any{"reference": "MYGO_TEST_ONLY_KEY", "operation": "set", "value": fakeKey})
			if err != nil {
				t.Fatal("isolated credential write failed")
			}
			snapshot, err := decodeConfiguration(raw)
			if err != nil || !snapshot.Credential.Configured || snapshot.Credential.Source != "managed" || strings.Contains(string(raw), fakeKey) || strings.Contains(application.supervisor.StderrTail(), fakeKey) {
				t.Fatal("credential status or redaction failed")
			}
			if mode == "real" {
				application.Shutdown(10 * time.Second)
				updates = make(chan func(), 512)
				restartedUpdates := updates
				application = New(Options{Launch: launch, CWD: cwd, Provider: provider, Model: "echo", UseHostDefaults: true, Post: func(update func()) { restartedUpdates <- update }})
				defer application.Shutdown(10 * time.Second)
				go application.Start()
				waitFor(t, application, updates, 90*time.Second, func() bool { return application.model.Phase == view.PhaseIdle })
				if application.model.ActiveRoute.Model != "route-probe" || application.model.ActiveRoute.Effort != "high" {
					t.Fatal("default model did not survive Host restart")
				}
				snapshot, err = readConfiguration(ctx, application.currentClient())
				if err != nil || !snapshot.Credential.Configured {
					t.Fatal("managed test credential did not survive restart")
				}
			}
			raw, err = application.currentClient().Call(ctx, "configuration/credential", map[string]any{"reference": "MYGO_TEST_ONLY_KEY", "operation": "unset"})
			if err != nil || json.Unmarshal(raw, &snapshot) != nil || snapshot.Credential.Configured {
				t.Fatal("managed credential was not cleared")
			}
			application.OpenSettings()
			waitFor(t, application, updates, 30*time.Second, func() bool { return !application.model.SettingsBusy })
			application.model.SettingsModel = "route-probe"
			application.model.SettingsEffort = "Provider default"
			application.SaveDefaultModel()
			waitFor(t, application, updates, 30*time.Second, func() bool { return !application.model.SettingsBusy })
			if application.model.SettingsError != "" || application.model.Configuration.Defaults.Effort != "" || application.model.ActiveRoute.Effort != "high" {
				t.Fatal("default effort clear affected the existing session")
			}
			application.CloseSettings()
			application.SwitchSession(originalID)
			waitFor(t, application, updates, 30*time.Second, func() bool { return !application.model.SessionBusy })
			if mode == "real" && (application.model.ActiveRoute.Model != "route-probe" || application.model.ActiveRoute.Effort != "") {
				t.Fatal("cold restore did not use current Host defaults")
			}
			if mode == "real" {
				application.Send("restored default effort probe")
				waitFor(t, application, updates, 30*time.Second, func() bool {
					return application.model.Phase == view.PhaseIdle && hasAssistantText(application.model.Items, "Selected "+provider+"/route-probe/default")
				})
			}
			if mode == "fake" && application.model.ActiveRoute.Model != "echo" {
				t.Fatal("owned session route was changed by a default save")
			}
		})
	}
}
