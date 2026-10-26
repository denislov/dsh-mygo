package app

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

type blockedHostClose struct {
	fakeClient
	started chan struct{}
	release chan struct{}
}

func (c *blockedHostClose) Close() {
	close(c.started)
	<-c.release
	c.fakeClient.Close()
}

func TestHostStopWaitsForInFlightClose(t *testing.T) {
	client := &blockedHostClose{started: make(chan struct{}), release: make(chan struct{})}
	instance := &hostInstance{client: client}
	var release sync.Once
	unblock := func() { release.Do(func() { close(client.release) }) }
	defer unblock()
	firstDone := make(chan struct{})
	go func() { instance.stop(time.Second); close(firstDone) }()
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("first stop did not close the client")
	}
	secondStarted, secondDone := make(chan struct{}), make(chan struct{})
	go func() { close(secondStarted); instance.stop(time.Second); close(secondDone) }()
	<-secondStarted
	select {
	case <-secondDone:
		t.Fatal("repeated stop returned before the in-flight close finished")
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	for _, done := range []chan struct{}{firstDone, secondDone} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("stop did not finish after close was released")
		}
	}
	if client.closeCount != 1 {
		t.Fatalf("close count = %d", client.closeCount)
	}
}

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

func TestDetachedHostRemainsAvailableToWaitForCleanup(t *testing.T) {
	application := New(Options{})
	instance := &hostInstance{generation: 1, client: &fakeClient{}}
	application.installHost(instance)
	if application.detachHost() != instance || application.currentHost() != nil {
		t.Fatal("detach did not remove the active host")
	}
	if application.detachHost() != instance {
		t.Fatal("restart lost the host whose cleanup it must wait for")
	}
	application.stopHostInstance(instance)
	newer := &hostInstance{generation: 2, client: &fakeClient{}}
	application.installHost(newer)
	application.detachHost()
	application.stopHostInstance(instance)
	if application.detachHost() != newer {
		t.Fatal("late old cleanup replaced the newer cleanup target")
	}
	application.stopHostInstance(newer)
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

func TestNotificationCommitBoundaryRejectsOldHostAndKeepsLocalCallbacks(t *testing.T) {
	application := New(Options{Post: func(fn func()) { fn() }})
	application.sessionID = "s1"
	application.model.SessionID = "s1"
	application.model.Phase = view.PhaseIdle
	application.hostGeneration.Store(2)
	old := &hostInstance{generation: 1, client: &fakeClient{}}
	newer := &hostInstance{generation: 2, client: &fakeClient{}}
	if !application.installHost(newer) {
		t.Fatal("new host was not installed")
	}
	status := json.RawMessage(`{"sessionId":"s1","status":"running"}`)
	application.processNotification(hostNotification{host: old, method: protocol.NotificationSessionStatus, params: status})
	if application.hostStatus != "" || application.model.Phase != view.PhaseIdle {
		t.Fatal("old host status was committed")
	}
	application.processNotification(hostNotification{host: newer, method: protocol.NotificationSessionStatus, params: status})
	if application.hostStatus != "running" || application.model.Phase != view.PhaseRunning {
		t.Fatal("current host status was not committed")
	}
	application.stopHost()
	applied := 0
	application.processNotification(hostNotification{apply: func() { applied++ }})
	if applied != 1 {
		t.Fatal("local callback was dropped with the host generation")
	}
}

func TestFirstHostFailureReasonWins(t *testing.T) {
	updates := make(chan func(), 4)
	application := New(Options{Post: func(fn func()) { updates <- fn }})
	application.model.Phase = view.PhaseIdle
	application.hostGeneration.Store(1)
	instance := &hostInstance{generation: 1, client: &fakeClient{}}
	if !application.installHost(instance) {
		t.Fatal("host was not installed")
	}
	application.failHost(instance, "first failure", errors.New("one"))
	(<-updates)()
	application.failHost(instance, "late failure", errors.New("two"))
	(<-updates)()
	if application.model.Error != "first failure" || application.model.ErrorDetail != "one" {
		t.Fatalf("failure was overwritten: %+v", application.model)
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
