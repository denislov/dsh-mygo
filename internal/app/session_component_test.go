package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSessionNavigatorBufferIsBoundedAndInvalidationDropsResults(t *testing.T) {
	var navigator sessionNavigator
	ctx, operation, cancel := navigator.sessionSlot.begin(time.Second)
	navigator.loadingSession = "s1"
	ok, overflow := navigator.bufferResponse(sessionNotificationSource(nil, "session/event", json.RawMessage(`{}`)))
	if !ok || overflow || len(navigator.sessionBuffer) != 1 {
		t.Fatal("notification was not buffered")
	}
	navigator.invalidate()
	if navigator.loadingSession != "" || navigator.sessionBuffer != nil || navigator.sessionBufferBytes != 0 || navigator.sessionSlot.valid(operation) {
		t.Fatal("invalidation kept navigation or buffer state")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("invalidation did not cancel the navigation context")
	}
	cancel()

	navigator = sessionNavigator{}
	for index := 0; index < maxSessionBufferFrames; index++ {
		ok, overflow := navigator.bufferResponse(sessionNotificationSource(nil, "session/status", json.RawMessage(`{}`)))
		if !ok || overflow {
			t.Fatalf("frame %d should fit", index)
		}
	}
	if ok, overflow := navigator.bufferResponse(sessionNotificationSource(nil, "session/status", json.RawMessage(`{}`))); ok || !overflow {
		t.Fatal("frame limit did not fail closed")
	}
	navigator = sessionNavigator{}
	if ok, overflow := navigator.bufferResponse(sessionNotificationSource(nil, "session/status", json.RawMessage(`"`+strings.Repeat("x", maxSessionBufferBytes)+`"`))); ok || !overflow {
		t.Fatal("byte limit did not fail closed")
	}
}

func TestSessionCatalogTracksTitleDirtyAndReset(t *testing.T) {
	var catalog sessionCatalog
	catalog.markDirty()
	if !catalog.takeDirty() || catalog.takeDirty() {
		t.Fatal("dirty hint was not consumed once")
	}
	catalog.setTitle(7, "Renamed")
	catalog.sessionNeedsHistory = true
	catalog.retiredSessions = map[string]bool{"old": true}
	if catalog.sessionTitle != "Renamed" || catalog.sessionTitleSeq != 7 {
		t.Fatal("title was not recorded")
	}
	catalog.reset()
	if catalog.sessionCatalogDirty || catalog.sessionNeedsHistory || catalog.retiredSessions != nil || catalog.sessionTitle != "" || catalog.sessionTitleSeq != 0 {
		t.Fatal("reset kept stale catalog hints")
	}
}
