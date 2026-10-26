package app

import (
	"testing"
	"time"
)

func TestSettingsComponentScopesOperationsToTargetSession(t *testing.T) {
	var settings settingsComponent
	firstCtx, first, firstCancel := settings.begin(time.Second, "s1")
	if !settings.current(first, "s1") || settings.current(first, "s2") {
		t.Fatal("operation was not scoped to its target session")
	}
	_, second, secondCancel := settings.begin(time.Second, "s2")
	select {
	case <-firstCtx.Done():
	default:
		t.Fatal("new settings operation did not cancel the previous one")
	}
	if settings.current(first, "s1") || !settings.current(second, "s2") {
		t.Fatal("target session or operation identity was not replaced")
	}
	settings.hostChanged()
	if settings.current(second, "s2") {
		t.Fatal("host change kept a settings operation current")
	}
	firstCancel()
	secondCancel()
}
