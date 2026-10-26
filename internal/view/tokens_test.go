package view

import "testing"

func TestSemanticLayoutTokensKeepBaseline(t *testing.T) {
	if controlSize != 32 || compactActionSize != 24 || rowActionSize != 26 {
		t.Fatal("action sizes changed")
	}
	if panelHeaderHeight != 40 || settingsHeaderHeight != 48 {
		t.Fatal("header heights changed")
	}
	if iconSmall != 12 || iconDefault != 14 || iconControl != 17 || iconAction != 18 {
		t.Fatal("icon sizes changed")
	}
	if attachmentThumbW != 40 || attachmentThumbH != 32 {
		t.Fatal("attachment thumbnail size changed")
	}
}
