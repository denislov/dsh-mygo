package app

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/view"
)

func TestAgainstMygoModelSettings(t *testing.T) {
	for _, mode := range []string{"fake", "real"} {
		t.Run(mode, func(t *testing.T) {
			var launch host.Config
			var root string
			provider := "test"
			if mode == "real" {
				launch, root = realMygoLaunch(t, true)
				provider = "mygo-mock"
			} else {
				node, err := exec.LookPath("node")
				if err != nil {
					t.Skip("node is not installed")
				}
				fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
				if err != nil {
					t.Fatal(err)
				}
				launch = host.Config{Command: node, Args: []string{fixture, "--models", "--sessions"}}
				root = t.TempDir()
			}
			updates := make(chan func(), 512)
			application := New(Options{Launch: launch, CWD: root, Provider: provider, Model: "echo", Post: func(fn func()) { updates <- fn }})
			defer application.Shutdown(10 * time.Second)
			go application.Start()
			waitFor(t, application, updates, 90*time.Second, func() bool { return application.model.Phase == view.PhaseIdle })
			if !application.model.CanModels {
				t.Fatal("model capability was not negotiated")
			}
			original := application.sessionID
			application.Send("initial model")
			waitFor(t, application, updates, 30*time.Second, func() bool { return application.model.Phase == view.PhaseIdle && assistantAnswers(application) == 1 })
			application.model.Draft = "keep model draft"
			selectModel := func(model, effort string) {
				t.Helper()
				application.OpenSettings()
				waitFor(t, application, updates, 20*time.Second, func() bool { return !application.model.SettingsBusy })
				// The fake host advertises echo and route-probe; the real mock
				// adapter also advertises todo-probe, subagent-probe and the
				// stdout-probe that logs during metadata resolution.
				wantCatalog := 2
				if mode == "real" {
					wantCatalog = 5
				}
				if len(application.model.Catalog.Models) != wantCatalog {
					t.Fatalf("catalog model count = %d; error = %s", len(application.model.Catalog.Models), application.model.SettingsError)
				}
				application.model.SettingsModel = model
				application.model.SettingsEffort = "Provider default"
				if effort != "" {
					application.model.SettingsEffort = "Effort: " + effort
				}
				application.ApplySettings()
				waitFor(t, application, updates, 20*time.Second, func() bool { return !application.model.SettingsBusy })
				if application.model.SettingsOpen || application.model.ActiveRoute.Model != model || application.model.ActiveRoute.Effort != effort || application.model.Draft != "keep model draft" {
					t.Fatalf("model apply failed: %s", application.model.SettingsError)
				}
			}
			selectModel("route-probe", "high")
			application.Send("use selected model")
			waitFor(t, application, updates, 30*time.Second, func() bool {
				return application.model.Phase == view.PhaseIdle && hasAssistantText(application.model.Items, fmt.Sprintf("Selected %s/route-probe/high", provider))
			})
			application.model.Draft = "keep model draft"
			selectModel("route-probe", "")
			application.Send("clear inherited effort")
			waitFor(t, application, updates, 30*time.Second, func() bool {
				return application.model.Phase == view.PhaseIdle && hasAssistantText(application.model.Items, fmt.Sprintf("Selected %s/route-probe/default", provider))
			})
			application.model.Draft = "keep model draft"
			application.NewSession()
			waitFor(t, application, updates, 30*time.Second, func() bool { return !application.model.SessionBusy && application.sessionID != original })
			if application.model.ActiveRoute.Model != "echo" {
				t.Fatal("session selection changed launch defaults")
			}
			application.SwitchSession(original)
			waitFor(t, application, updates, 30*time.Second, func() bool { return !application.model.SessionBusy && application.sessionID == original })
			if application.model.ActiveRoute.Model != "route-probe" || application.model.ActiveRoute.Effort != "" {
				t.Fatal("switching lost the owned session model")
			}
			application.Send("continue selected model after switching")
			waitFor(t, application, updates, 30*time.Second, func() bool { return application.model.Phase == view.PhaseIdle && assistantAnswers(application) == 4 })
			if mode == "real" {
				// The mock adapter writes console.info while resolving this
				// model. Plugin diagnostics must stay off the JSON-RPC stdout
				// channel, including during model selection.
				application.model.Draft = "keep model draft"
				selectModel("stdout-probe", "")
			}
		})
	}
}
