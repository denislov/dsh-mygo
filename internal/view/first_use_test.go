package view

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestComposerGuidesFolderWithoutRedundantConnectionEntry(t *testing.T) {
	m := sessionModel()
	m.SessionCWD, m.Draft = "C:/Projects", "keep draft"
	m.Setup.WorkspaceRequired = true
	m.CanConfigure, m.ConfigurationReady = true, true
	m.ActiveRoute.Provider = "deepseek-official"
	m.Configuration.Credential.Available = true
	sent, chosen, connected := 0, 0, 0
	m.Send = func(string) { sent++ }
	m.Setup.ChooseFolder = func() { chosen++ }
	m.Setup.UseFolder = func() { m.Setup.WorkspaceRequired = false }
	m.Setup.Connect = func() { connected++ }
	tt := tester(m)
	if err := tt.Click("Choose project folder"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("Connect DeepSeek") {
		t.Fatal("redundant connection entry returned")
	}
	_ = tt.Click("Send")
	if sent != 0 || chosen != 1 || connected != 0 || m.Draft != "keep draft" {
		t.Fatal("setup lost draft or allowed premature send")
	}
	_ = tt.Click("Use this folder")
	if !m.SetupBlocked() {
		t.Fatal("missing key did not block sending")
	}
	m.Configuration.Credential.Configured = true
	tt.Frame()
	if m.SetupBlocked() {
		t.Fatal("configured connection remained blocked")
	}
	_ = tt.Click("Send")
	if sent != 1 {
		t.Fatal("completed setup did not allow sending")
	}
}

func TestConnectionSetupReusesCredentialControlsAtMinimumSize(t *testing.T) {
	m := settingsModel()
	m.SettingsOpen = true
	m.CanConfigure, m.ConfigurationReady, m.SettingsConnection = true, true, true
	m.Configuration.Credential = CredentialStatus{Available: true, Writable: true, Reference: "DEEPSEEK_API_KEY", Source: "none"}
	m.RefreshSettings = func() {}
	tt := ui.NewTester(func(c *ui.Context) { View(c, m) }, 560, 420)
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("Connect DeepSeek") || tt.HasText("Provider route") {
		t.Fatal("connection setup did not focus on credentials")
	}
	if err := tt.Click("New API key"); err != nil {
		t.Fatal(err)
	}
	tt.Type("test-key")
	for _, label := range []string{"Save key", "Close settings", "Refresh"} {
		bounds, ok := tt.Find(label)
		if !ok || bounds.Y < 0 || bounds.Y+bounds.H > 420 {
			t.Fatalf("%s out of reach: %+v", label, bounds)
		}
	}
}

func TestFirstUseMinimumWindowKeepsFolderChoicesReachable(t *testing.T) {
	m := sessionModel()
	m.Setup.WorkspaceRequired = true
	m.SessionCWD = "C:/Projects"
	choices := 0
	m.Setup.ChooseFolder = func() { choices++ }
	m.Setup.UseFolder = func() { m.Setup.WorkspaceRequired = false }
	m.CanConfigure = true
	m.Setup.Connect = func() {}
	// Reserve space for Windows title-bar chrome within the minimum window.
	tt := ui.NewTester(func(c *ui.Context) { View(c, m) }, 560, 380)
	tt.Advance(300 * time.Millisecond)
	if tt.HasText("Start a conversation") {
		t.Fatal("decorative header consumed the compact setup area")
	}
	for _, label := range []string{"Choose project folder", "Use this folder", "Message"} {
		r, ok := tt.Find(label)
		if !ok || r.Y < 0 || r.Y+r.H > 380 {
			t.Fatalf("%s out of reach: %+v", label, r)
		}
	}
	message, _ := tt.Find("Message")
	for _, label := range []string{"Choose project folder", "Use this folder"} {
		r, _ := tt.Find(label)
		if r.Y < message.Y+message.H {
			t.Fatalf("%s should be below input: %+v / %+v", label, r, message)
		}
	}
	if err := tt.Click("Choose project folder"); err != nil {
		t.Fatal(err)
	}
	if choices != 1 {
		t.Fatal("folder button was clipped or covered")
	}
}
