package view

import (
	"testing"

	"dsh-mygo/internal/i18n"
)

func TestNamedAppMessagesRenderInBothLocales(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	model.Error = i18n.MsgRuntimeProbeFailed
	tester := tester(model)
	if !tester.HasText("The selected dsh command could not be used. Check Node.js 24+ and the DSH version.") {
		t.Fatal("English named message did not render")
	}
	model.Prefs.Value.Locale = "zh-CN"
	tester.Frame()
	if !tester.HasText("无法使用所选 dsh 命令，请检查 Node.js 24+ 与 DSH 版本。") {
		t.Fatalf("Chinese named message did not render: %v", tester.Texts())
	}
	model.Error = "technical detail from Host"
	tester.Frame()
	if !tester.HasText("technical detail from Host") {
		t.Fatal("technical detail should pass through untranslated")
	}
}
