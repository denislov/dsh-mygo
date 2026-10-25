package view

import (
	"testing"

	"dsh-mygo/internal/interaction"
	"dsh-mygo/internal/projection"
	// Link the translations the shipped app links in main.go, so the tests
	// cover MyGo's built-in controls in the Chinese locale too.
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/ui"
)

func TestPreferencesEntryWithoutHostAndShortcutGates(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	n := 0
	model.NewSession = func() { n++ }
	model.CanHistory = true
	tester := ui.NewTester(func(c *ui.Context) { View(c, model) }, 560, 420)
	tester.Frame()
	if err := clickToolbarMenu(tester, "Settings"); err != nil {
		t.Fatal(err)
	}
	if !model.SettingsOpen || model.SettingsSection != SettingsSectionGeneral || !tester.HasText("Theme") {
		t.Fatal("preferences depend on Host capabilities")
	}
	tester.Key(ui.Ctrl, ui.KeyN)
	if n != 0 {
		t.Fatal("shortcut navigated behind modal")
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	tester.Key(ui.Ctrl, ui.KeyN)
	if n != 1 {
		t.Fatal("new conversation command missing")
	}
}

func TestConfiguredApplicationCommandsAndStopGate(t *testing.T) {
	model := sessionModel()
	newCalls, stops := 0, 0
	model.NewSession = func() { newCalls++ }
	model.Cancel = func() { stops++ }
	model.CanCancel = true
	model.Prefs.Value.NewSession = "Ctrl+Shift+N"
	tt := tester(model)
	tt.Key(ui.Ctrl, ui.KeyN)
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyN)
	if newCalls != 1 {
		t.Fatal("new binding not applied or old binding survived")
	}
	model.Phase = PhaseRunning
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyPeriod)
	if stops != 1 {
		t.Fatal("stop command missing")
	}
	model.CancelPending = true
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyPeriod)
	if stops != 1 {
		t.Fatal("pending stop repeated")
	}
	model.Phase = PhaseIdle
	model.CancelPending = false
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyComma)
	if !model.SettingsOpen || model.SettingsSection != SettingsSectionGeneral {
		t.Fatal("preferences command missing")
	}
	model.SettingsOpen = false
	model.Prefs.Value.Preferences = "None"
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyComma)
	if model.SettingsOpen {
		t.Fatal("disabled command remained registered")
	}
}

func TestChinesePreferencesKeepConversationAndInteractionPayloads(t *testing.T) {
	model := sessionModel()
	model.Prefs.Value.Locale = "zh-CN"
	model.Items = []projection.Item{{ID: "u", Kind: projection.KindUser, Title: "You", Text: "Send"}, {ID: "a", Kind: projection.KindAssistant, Title: "Assistant", Text: "Allow once"}}
	model.Sessions[0].Title = "Preferences"
	tt := tester(model)
	for _, text := range []string{"更多", "切换会话栏", "就绪", "Send", "Allow once", "Current · Preferences"} {
		if text == "Current · Preferences" {
			text = "当前 · Preferences"
		}
		if !tt.HasText(text) {
			t.Fatalf("missing %q in %v", text, tt.Texts())
		}
	}
	model.CanQuestion = true
	model.QuestionOpen = true
	model.Questions = []*interaction.QuestionRequest{{RequestID: "q", SessionID: "one", Questions: []interaction.Question{{ID: "question", Question: "Send", Options: []interaction.QuestionOption{{Label: "Stop"}}}}, Drafts: []interaction.QuestionDraft{{Selected: []bool{false}}}}}
	tt.Frame()
	if !tt.HasText("用户提问") || !tt.HasText("Stop") || !tt.HasText("Send") {
		t.Fatal("question payload was translated or chrome was not")
	}
}

func TestChineseBuiltinCodeCopyKeepsSource(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	model.Prefs.Value.Locale = "zh-CN"
	model.Items = []projection.Item{{ID: "code", Kind: projection.KindAssistant, Title: "Assistant", Text: "```text\nSend\n```\n"}}
	tt := tester(model)
	tt.SetLocale("zh-CN")
	if err := tt.Click("复制代码"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != "Send\n" {
		t.Fatalf("translated code: %q", tt.Clipboard())
	}
}

func TestPreferenceSelectorsBindStableValuesAndThemeFrames(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	var dark bool
	tt := ui.NewTester(func(c *ui.Context) { View(c, model); dark = c.Theme().Dark }, 560, 420)
	if err := clickToolbarMenu(tt, "Settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Light theme"); err != nil {
		t.Fatal(err)
	}
	if model.Prefs.Value.Theme != "light" || !model.SettingsOpen {
		t.Fatal("theme was not saved immediately")
	}
	if err := tt.Click("Language selection"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if model.Prefs.Value.Locale != "zh-CN" || dark {
		t.Fatal("locale binding or theme did not survive notice frame")
	}
	if err := tt.Click("深色主题"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !dark {
		t.Fatal("dark theme not applied")
	}
	model.Prefs.Value.Theme = "system"
	tt.Frame()
	if dark {
		t.Fatal("system theme retained previous override")
	}
}

func TestPreferenceFormValidationAndClosePreserveSavedValues(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	model.OpenPreferences()
	tt := tester(model)
	model.Prefs.Draft.Stop = model.Prefs.Draft.NewSession
	model.ApplyPreferences()
	tt.Frame()
	if model.Prefs.Error == "" || model.Prefs.Value.Stop == model.Prefs.Value.NewSession {
		t.Fatal("conflicting shortcuts accepted")
	}
	if err := tt.Click("Close settings"); err != nil {
		t.Fatal(err)
	}
	if err := clickToolbarMenu(tt, "Settings"); err != nil {
		t.Fatal(err)
	}
	if model.Prefs.Draft != model.Prefs.Value {
		t.Fatal("invalid edit survived reopening")
	}
	if err := tt.Click("Dark theme"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Close settings"); err != nil {
		t.Fatal(err)
	}
	if model.Prefs.Value.Theme != "dark" {
		t.Fatal("close rolled back an already saved setting")
	}
}

func TestConfiguredComposerShortcutPreservesEnterNewline(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	model.Prefs.Value.Send = "Ctrl+Enter"
	sent := ""
	model.Send = func(text string) { sent = text }
	tester := tester(model)
	if err := tester.Click("Message"); err != nil {
		t.Fatal(err)
	}
	tester.Type("first")
	tester.Key(0, ui.KeyEnter)
	tester.Type("second")
	if sent != "" || model.Draft != "first\nsecond" {
		t.Fatalf("newline: %q %q", model.Draft, sent)
	}
	tester.Key(ui.Ctrl, ui.KeyEnter)
	if sent != "first\nsecond" {
		t.Fatalf("send: %q", sent)
	}
}
