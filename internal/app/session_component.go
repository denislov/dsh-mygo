package app

import (
	"context"
	"encoding/json"
	"time"
)

const (
	maxSessionBufferFrames = 1024
	maxSessionBufferBytes  = 8 * 1024 * 1024
)

// sessionNavigator owns the in-flight session load identity, its cancellation
// token and the bounded notification buffer. It does not own the transcript or
// the durable catalog.
type sessionNavigator struct {
	sessionSlot        operationSlot
	loadingSession     string
	sessionBuffer      []hostNotification
	sessionBufferBytes int
}

// sessionCatalog owns catalog hints that are not session truth: dirty state,
// committed title, history-needed flag and retired release handles.
type sessionCatalog struct {
	sessionTitleSeq     uint64
	sessionTitle        string
	sessionNeedsHistory bool
	sessionCatalogDirty bool
	retiredSessions     map[string]bool
}

type sessionComponent struct {
	sessionNavigator
	sessionCatalog
}

func (n *sessionNavigator) begin(timeout time.Duration) (context.Context, uint64, context.CancelFunc) {
	return n.sessionSlot.begin(timeout)
}

func (n *sessionNavigator) invalidate() {
	n.sessionSlot.invalidate()
	n.loadingSession = ""
	n.sessionBuffer = nil
	n.sessionBufferBytes = 0
}

// bufferResponse stores one notification for a loading target session. It
// reports overflow instead of silently dropping durable state.
func (n *sessionNavigator) bufferResponse(notification hostNotification) (bool, bool) {
	if len(n.sessionBuffer) >= maxSessionBufferFrames || n.sessionBufferBytes+len(notification.params) > maxSessionBufferBytes {
		return false, true
	}
	n.sessionBuffer = append(n.sessionBuffer, notification)
	n.sessionBufferBytes += len(notification.params)
	return true, false
}

func (c *sessionCatalog) markDirty() { c.sessionCatalogDirty = true }

func (c *sessionCatalog) takeDirty() bool {
	dirty := c.sessionCatalogDirty
	c.sessionCatalogDirty = false
	return dirty
}

func (c *sessionCatalog) setTitle(seq uint64, title string) {
	c.sessionTitleSeq = seq
	c.sessionTitle = title
}

func (c *sessionCatalog) clearTitle() {
	c.sessionTitleSeq = 0
	c.sessionTitle = ""
}

func (c *sessionCatalog) reset() {
	c.clearTitle()
	c.sessionNeedsHistory = false
	c.sessionCatalogDirty = false
	c.retiredSessions = nil
}

// hostChanged keeps catalog hints out of a new Host generation. Navigation
// state is invalidated separately by the App's HostReady sequence.
func (c *sessionCatalog) hostChanged() {
	c.sessionCatalogDirty = false
	c.sessionNeedsHistory = false
	c.retiredSessions = nil
}

func sessionNotificationSource(source *hostInstance, method string, params json.RawMessage) hostNotification {
	return hostNotification{host: source, method: method, params: params}
}
