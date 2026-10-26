package app

import (
	"encoding/json"
	"testing"

	"dsh-mygo/internal/view"
)

func TestHostInstancesStopOnlyTheirOwnGeneration(t *testing.T) {
	application := New(Options{})
	oldClient := &fakeClient{}
	old := &hostInstance{generation: 1, client: oldClient}
	if !application.installHost(old) || application.currentHost() != old {
		t.Fatal("first host was not installed")
	}
	newClient := &fakeClient{}
	newer := &hostInstance{generation: 2, client: newClient}
	if !application.installHost(newer) || application.currentHost() != newer {
		t.Fatal("newer host was not installed")
	}
	application.stopHostInstance(old)
	if !oldClient.closed || oldClient.closeCount != 1 || newClient.closed || application.currentHost() != newer {
		t.Fatalf("old stop closed the wrong host: old=%t/%d new=%t", oldClient.closed, oldClient.closeCount, newClient.closed)
	}
	application.stopHostInstance(old)
	if oldClient.closeCount != 1 {
		t.Fatal("repeated stop closed the old client twice")
	}
	application.stopHost()
	if !newClient.closed || newClient.closeCount != 1 || application.currentHost() != nil {
		t.Fatal("current host was not stopped exactly once")
	}
}

func TestOldHostNotificationsAndFailuresCannotTouchNewGeneration(t *testing.T) {
	updates := make(chan func(), 8)
	application := New(Options{Post: func(fn func()) { updates <- fn }})
	application.hostGeneration.Store(2)
	newer := &hostInstance{generation: 2, client: &fakeClient{}}
	old := &hostInstance{generation: 1, client: &fakeClient{}}
	if !application.installHost(newer) {
		t.Fatal("new host was not installed")
	}
	incoming := make(chan hostNotification, 1)
	application.forwardHostNotification(old, incoming, "session/status", json.RawMessage(`{}`))
	select {
	case notification := <-incoming:
		t.Fatalf("old notification was forwarded: %+v", notification)
	default:
	}
	application.forwardHostNotification(newer, incoming, "session/status", json.RawMessage(`{}`))
	select {
	case notification := <-incoming:
		if notification.method != "session/status" {
			t.Fatalf("notification = %+v", notification)
		}
	default:
		t.Fatal("current notification was dropped")
	}
	application.failHost(old, "old host", nil)
	(<-updates)()
	if application.model.Phase == view.PhaseFailed {
		t.Fatal("old host failure reached the new generation")
	}
	application.failHost(newer, "current host", nil)
	(<-updates)()
	if application.model.Phase != view.PhaseFailed {
		t.Fatal("current host failure was suppressed")
	}
}

func TestStoppedLastHostStillReportsItsFailureAndRejectsNewInstallAfterShutdown(t *testing.T) {
	updates := make(chan func(), 4)
	application := New(Options{Post: func(fn func()) { updates <- fn }})
	application.hostGeneration.Store(3)
	instance := &hostInstance{generation: 3, client: &fakeClient{}}
	if !application.installHost(instance) {
		t.Fatal("host was not installed")
	}
	application.stopHost()
	application.failHost(instance, "host exited", nil)
	(<-updates)()
	if application.model.Phase != view.PhaseFailed {
		t.Fatal("failure from the just-stopped generation was suppressed")
	}

	closed := New(Options{})
	closed.shuttingDown.Store(true)
	if closed.installHost(&hostInstance{generation: 4, client: &fakeClient{}}) {
		t.Fatal("shutdown accepted a new host")
	}
}
