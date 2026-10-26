package app

import "testing"

func TestLocalComponentOwnsDraftsSeparatelyFromHostSessions(t *testing.T) {
	var local localComponent
	local.setDraft("s1", "first")
	local.setDraft("s2", "second")
	if local.draft("s1") != "first" || local.draft("s2") != "second" || local.draft("missing") != "" {
		t.Fatal("session drafts were not isolated")
	}
}

func TestQuitCoordinatorIsSingleFlightAndRetryable(t *testing.T) {
	var quit quitCoordinator
	if !quit.begin() || quit.begin() {
		t.Fatal("quit was not single-flight")
	}
	quit.fail()
	if quit.isReady() || !quit.begin() {
		t.Fatal("failed quit did not become retryable")
	}
	quit.complete()
	if !quit.isReady() || quit.begin() {
		t.Fatal("completed quit accepted another request")
	}
}
