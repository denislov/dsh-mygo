package app

import (
	"encoding/json"
	"errors"
	"slices"
	"time"
)

const notificationBatchFrames = 64
const notificationBatchBytes = 256 * 1024
const notificationBatchInterval = 16 * time.Millisecond

type hostNotification struct {
	method string
	params json.RawMessage
	apply  func()
}

func (a *App) runNotifications(incoming <-chan hostNotification) {
	for {
		var first hostNotification
		select {
		case <-a.stopped:
			return
		case notification, open := <-incoming:
			if !open {
				return
			}
			first = notification
		}
		batch := []hostNotification{first}
		bytes := len(first.params)
		closed := false
		timer := time.NewTimer(notificationBatchInterval)
	collect:
		for len(batch) < notificationBatchFrames && bytes < notificationBatchBytes {
			select {
			case <-a.stopped:
				timer.Stop()
				return
			case <-timer.C:
				break collect
			case notification, open := <-incoming:
				if !open {
					closed = true
					break collect
				}
				batch = append(batch, notification)
				bytes += len(notification.params)
			}
		}
		timer.Stop()
		select {
		case <-a.stopped:
			return
		case a.pendingUI <- struct{}{}:
		}
		a.options.Post(func() {
			defer func() { <-a.pendingUI }()
			a.batchUpdating = true
			for _, notification := range batch {
				if notification.apply != nil {
					notification.apply()
				} else {
					a.handleNotification(notification.method, notification.params)
				}
			}
			a.batchUpdating = false
			if a.transcriptDirty {
				a.publishTranscript()
			}
		})
		if closed {
			return
		}
	}
}

func (a *App) publishTranscript() {
	if a.batchUpdating {
		a.transcriptDirty = true
		return
	}
	a.transcriptDirty = false
	a.model.Items = a.transcript.Items()
	if item, visible := a.stream.Item(); visible {
		a.model.Items = append(slices.Clone(a.model.Items), item)
	}
}

func (a *App) clearStream() {
	a.stream.Clear()
	a.publishTranscript()
}

func (a *App) handleStreamNotification(params json.RawMessage) {
	var notification struct {
		SessionID string          `json:"sessionId"`
		Frame     json.RawMessage `json:"frame"`
	}
	if json.Unmarshal(params, &notification) != nil || !validSessionID(notification.SessionID) {
		a.setFailure("Invalid stream notification", errors.New("Invalid stream envelope"))
		go a.stopHost()
		return
	}
	if notification.SessionID != a.sessionID {
		return
	}
	if !a.model.CanStream {
		a.setFailure("Unexpected assistant stream", errors.New("Streaming capability was not negotiated"))
		go a.stopHost()
		return
	}
	if err := a.stream.Apply(notification.Frame); err != nil {
		a.setFailure("Invalid assistant stream", err)
		go a.stopHost()
		return
	}
	a.publishTranscript()
}
