package app

import (
	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/protocol"
	"encoding/json"
	"errors"
	"slices"
	"time"
)

const notificationBatchFrames = 64
const notificationBatchBytes = 256 * 1024
const notificationBatchInterval = 16 * time.Millisecond

type hostNotification struct {
	host   *hostInstance
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
				a.processNotification(notification)
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
	a.model.Usage = a.transcript.Usage
	a.model.Context = a.transcript.Context
	a.model.Tasks = a.transcript.Tasks
	a.model.PlanActive = a.transcript.PlanActive
	a.model.Goal = a.transcript.Goal
	if items := a.stream.Items(); len(items) > 0 {
		a.model.Items = append(slices.Clone(a.model.Items), items...)
	}
	// Prune with the merged rows so a live-only activity group keeps its state.
	a.model.PruneTranscriptState(a.model.Items)
}

func (a *App) clearStream() {
	a.stream.Clear()
	a.publishTranscript()
}

func (a *App) handleStreamNotificationFrom(source *hostInstance, params json.RawMessage) {
	var notification protocol.AssistantStreamNotification
	if json.Unmarshal(params, &notification) != nil || !validSessionID(notification.SessionID) {
		a.setFailure(msgStreamInvalidNotify, errors.New("Invalid stream envelope"))
		go a.stopHostInstance(a.currentHost())
		return
	}
	if notification.SessionID != a.sessionID {
		return
	}
	if !a.model.CanStream {
		a.setFailure(msgStreamUnexpected, errors.New("Streaming capability was not negotiated"))
		go a.stopHostInstance(a.currentHost())
		return
	}
	if err := a.stream.Apply(notification.Frame); err != nil {
		a.setFailure(msgStreamInvalid, err)
		go a.stopHostInstance(a.currentHost())
		return
	}
	if a.canStreamBaseline && a.stream.NeedsResync() {
		a.requireResync(errors.New("Assistant stream sequence gap"))
		a.bufferResyncNotificationFrom(source, protocol.NotificationSessionAssistantStream, params)
		return
	}
	// A resync can already contain this attempt's commit before its buffered
	// process-local end frame is replayed.
	var settled struct {
		Type    string `json:"type"`
		Outcome struct {
			Kind      string `json:"kind"`
			EventType string `json:"eventType"`
			Seq       uint64 `json:"seq"`
		} `json:"outcome"`
	}
	if json.Unmarshal(notification.Frame, &settled) == nil && settled.Type == "end" && settled.Outcome.Kind == "committed" && settled.Outcome.Seq < a.nextSeq {
		a.stream.Commit(projection.Event{Type: settled.Outcome.EventType, Seq: settled.Outcome.Seq})
	}
	a.publishTranscript()
}
