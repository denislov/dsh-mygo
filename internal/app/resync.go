package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/view"
)

const maxResyncFrames = 1024
const maxResyncBytes = 8 * 1024 * 1024

type resyncState struct {
	ctx      context.Context
	cancel   context.CancelFunc
	id       string
	buffer   []hostNotification
	bytes    int
	attempts int
}

func (a *App) cancelResync() {
	if a.resync != nil {
		a.resync.cancel()
		a.resync = nil
	}
}

func (a *App) startResync() {
	a.CloseSubagents()
	if a.model.SettingsApplying || a.model.SettingsWritingConfiguration || a.model.SessionPanel.Writing {
		a.failResync()
		return
	}
	a.cancelSessionOperation()
	a.cancelSettingsOperation()
	a.model.SessionsOpen = false
	a.model.SettingsOpen = false
	a.model.ToolInspector = nil
	a.model.Phase = view.PhaseResyncing
	a.model.RecoveryOpen = false
	a.model.Error = ""
	a.model.ErrorDetail = ""
	a.model.Permission.Ready = false
	a.model.Permission.Confirm = false
	a.clearStream()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	job := &resyncState{ctx: ctx, cancel: cancel, id: a.sessionID}
	a.resync = job
	a.logf("history resync started")
	a.readResync(job)
}

func (a *App) failResync() {
	a.cancelResync()
	a.setFailure("Could not synchronize session history", errors.New("Restart the host to recover the session from its durable log"))
	go a.stopHost()
}

func (a *App) queueResync(notification hostNotification) {
	job := a.resync
	if job == nil {
		return
	}
	if len(job.buffer) >= maxResyncFrames || job.bytes+len(notification.params) > maxResyncBytes {
		a.failResync()
		return
	}
	job.buffer = append(job.buffer, notification)
	job.bytes += len(notification.params)
}

func (a *App) bufferResyncNotification(method string, params json.RawMessage) bool {
	switch method {
	case "session/event", "session/status", "session/assistant-stream", "approval/request", "approval/closed", "question/request", "question/closed":
	default:
		return false
	}
	var envelope struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(params, &envelope) != nil {
		a.failResync()
		return true
	}
	if envelope.SessionID != a.sessionID && !(envelope.SessionID == "" && (method == "question/request" || method == "question/closed")) {
		return true
	}
	a.queueResync(hostNotification{method: method, params: params})
	return true
}

// A pending cancel acknowledgement must not disappear while history is loading.
func (a *App) postAfterResync(apply func()) {
	a.post(func() {
		if a.resync != nil {
			a.queueResync(hostNotification{apply: apply})
			return
		}
		apply()
	})
}

func resyncBufferContinuous(buffer []hostNotification, next uint64) (bool, error) {
	for _, notification := range buffer {
		if notification.method != "session/event" {
			continue
		}
		event, err := projection.DecodeNotification(notification.params)
		var required struct {
			Seq   *uint64 `json:"seq"`
			Event struct {
				Seq *uint64 `json:"seq"`
			} `json:"event"`
		}
		if err != nil || json.Unmarshal(notification.params, &required) != nil || required.Seq == nil || required.Event.Seq == nil || *required.Seq != event.Event.Seq || *required.Event.Seq != event.Event.Seq || event.Event.Seq > maxSafeInteger {
			return false, errors.New("Invalid buffered session sequence")
		}
		if event.Event.Seq < next {
			continue
		}
		if event.Event.Seq > next {
			return false, nil
		}
		next++
	}
	return true, nil
}

func resyncStreamContinuous(buffer []hostNotification, snapshot *historySnapshot) (bool, error) {
	var stream projection.AssistantStream
	if err := stream.Restore(snapshot.Baseline); err != nil {
		return false, err
	}
	for _, notification := range buffer {
		switch notification.method {
		case "session/assistant-stream":
			var envelope struct {
				Frame json.RawMessage `json:"frame"`
			}
			if json.Unmarshal(notification.params, &envelope) != nil {
				return false, errors.New("Invalid buffered stream")
			}
			if err := stream.Apply(envelope.Frame); err != nil {
				return false, err
			}
			if stream.NeedsResync() {
				return false, nil
			}
		case "session/event":
			event, err := projection.DecodeNotification(notification.params)
			if err != nil {
				return false, err
			}
			if event.Event.Seq >= snapshot.NextSeq {
				stream.Commit(event.Event)
			}
		}
	}
	return true, nil
}

func (a *App) readResync(job *resyncState) {
	job.attempts++
	client, minimumSeq, wantStream := a.currentClient(), a.nextSeq, a.canStreamBaseline
	window := a.canHistoryWindow
	minimumRevision := a.stream.Revision()
	go func() {
		var snapshot historySnapshot
		var err error
		if window {
			snapshot, err = readHistoryWindow(job.ctx, client, job.id, minimumSeq, nil, true, wantStream)
		} else {
			snapshot, err = readHistorySnapshot(job.ctx, client, job.id, minimumSeq, true, wantStream)
		}
		a.post(func() {
			if a.resync != job || a.shuttingDown.Load() || a.sessionID != job.id || a.model.Phase == view.PhaseFailed {
				return
			}
			if err != nil || snapshot.StatusRevision < a.statusRevision || wantStream && snapshot.Stream.Revision() < minimumRevision {
				a.failResync()
				return
			}
			continuous, invalid := resyncBufferContinuous(job.buffer, snapshot.NextSeq)
			if continuous && invalid == nil && wantStream {
				continuous, invalid = resyncStreamContinuous(job.buffer, &snapshot)
			}
			if invalid != nil {
				a.failResync()
				return
			}
			if !continuous {
				if job.attempts >= 3 {
					a.failResync()
					return
				}
				a.readResync(job)
				return
			}
			a.transcript = snapshot.Transcript
			a.acceptHistoryWindow(snapshot)
			if wantStream {
				a.stream = snapshot.Stream
			}
			a.nextSeq = snapshot.NextSeq
			a.statusRevision = snapshot.StatusRevision
			a.hostStatus = snapshot.Status
			a.model.CopiedMessageID = ""
			a.model.CopiedMessageText = ""
			a.model.Phase = view.PhaseRunning
			if snapshot.Status == "idle" {
				a.awaitingTurn = false
				a.sawActivity = false
			} else {
				a.sawActivity = true
			}
			a.applyStatus(snapshot.Status)
			buffer := job.buffer
			a.cancelResync()
			for _, notification := range buffer {
				if notification.apply != nil {
					notification.apply()
				} else {
					a.handleNotification(notification.method, notification.params)
				}
				if a.model.Phase == view.PhaseFailed || a.resync != nil {
					break
				}
			}
			if a.model.Phase == view.PhaseFailed || a.resync != nil {
				return
			}
			// RPC callbacks may predate the snapshot. The newest versioned Host
			// status still decides whether work is running after they settle.
			if a.hostStatus == "running" {
				a.model.Phase = view.PhaseRunning
			} else if !a.model.CancelPending {
				a.model.Phase = view.PhaseIdle
			}
			if a.model.Phase == view.PhaseIdle {
				a.clearStream()
			}
			a.publishTranscript()
			a.logf("history resync completed")
		})
	}()
}
