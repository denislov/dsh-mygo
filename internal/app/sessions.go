package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/view"
)

func (a *App) canChooseSession() bool {
	return !a.options.SDKProtocol && !a.shuttingDown.Load() && a.model.CanChooseSession() && a.currentClient() != nil
}

func (a *App) OpenSessions() {
	if !a.canChooseSession() {
		return
	}
	a.model.SessionsOpen = true
	a.model.ToolInspector = nil
	a.RefreshSessions()
}

func (a *App) CloseSessions() {
	if a.model.SessionSwitching {
		return
	}
	a.cancelSessionOperation()
	a.model.SessionsOpen = false
}

func (a *App) cancelSessionOperation() {
	a.sessionOperation++
	if a.sessionCancel != nil {
		a.sessionCancel()
		a.sessionCancel = nil
	}
	a.model.SessionBusy = false
	a.model.SessionSwitching = false
	a.loadingSession = ""
	a.sessionBuffer = nil
	a.sessionBufferBytes = 0
}

func (a *App) beginSessionOperation() (context.Context, uint64) {
	a.cancelSessionOperation()
	a.model.SessionBusy = true
	a.model.SessionsError = ""
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	a.sessionCancel = cancel
	return ctx, a.sessionOperation
}

func (a *App) currentSessionOperation(operation uint64) bool {
	return operation == a.sessionOperation && !a.shuttingDown.Load() && a.model.Phase != view.PhaseFailed
}

func (a *App) RefreshSessions() {
	if !a.canChooseSession() {
		return
	}
	ctx, operation := a.beginSessionOperation()
	client := a.currentClient()
	cancel := a.sessionCancel
	go func() {
		defer cancel()
		sessions, err := readSessionList(ctx, client)
		a.post(func() {
			if !a.currentSessionOperation(operation) {
				return
			}
			a.cancelSessionOperation()
			if err != nil {
				a.model.SessionsError = err.Error()
				return
			}
			a.model.Sessions = sessions
		})
	}()
}

func (a *App) NewSession() { a.loadSession("") }

func (a *App) SwitchSession(id string) {
	if !validSessionID(id) || !a.canChooseSession() {
		return
	}
	if id == a.sessionID {
		a.CloseSessions()
		return
	}
	a.loadSession(id)
}

func (a *App) loadSession(id string) {
	if !a.canChooseSession() {
		return
	}
	ctx, operation := a.beginSessionOperation()
	a.model.SessionSwitching = true
	a.loadingSession = id
	client := a.currentClient()
	cancel := a.sessionCancel
	params := map[string]any{"provider": a.options.Provider, "model": a.options.Model}
	if a.options.ReasoningEffort != "" {
		params["effort"] = a.options.ReasoningEffort
	}
	method := "session/resume"
	if id == "" {
		method = "session/create"
		params["cwd"] = a.options.CWD
	} else {
		params["sessionId"] = id
	}
	go func() {
		defer cancel()
		var summary sessionSummary
		var transcript projection.Transcript
		var nextSeq uint64
		raw, err := client.Call(ctx, method, params)
		if err != nil {
			err = errors.New("Could not open the session. Check ownership, working directory and model route, then retry")
		} else {
			var result struct {
				Session json.RawMessage `json:"session"`
			}
			if json.Unmarshal(raw, &result) != nil {
				err = errors.New("Invalid session response")
			} else {
				summary, err = decodeSessionSummary(result.Session)
				if err == nil && (summary.Status != "idle" || summary.NextSeq == nil || id != "" && summary.ID != id || summary.Provider == "" || summary.Model == "" || summary.CWD == "") {
					err = errors.New("Invalid restored session identity or route")
				}
			}
		}
		if err == nil {
			a.post(func() {
				if a.currentSessionOperation(operation) {
					a.loadingSession = summary.ID
				}
			})
			transcript, nextSeq, err = readSessionHistory(ctx, client, summary.ID, *summary.NextSeq)
		}
		a.post(func() {
			if !a.currentSessionOperation(operation) {
				return
			}
			buffer := a.sessionBuffer
			a.cancelSessionOperation()
			if err != nil {
				a.model.SessionsError = err.Error()
				if len(buffer) > 0 {
					a.setFailure("The session changed while history was loading", errors.New("Restart the host to recover the session safely"))
					go a.stopHost()
				}
				return
			}
			if a.model.Phase != view.PhaseIdle || len(a.model.Approvals) > 0 || len(a.model.Questions) > 0 {
				a.model.SessionsError = "Finish the current interaction before switching sessions"
				if len(buffer) > 0 {
					a.setFailure("Concurrent session activity is not supported", errors.New("Restart the host to recover from the durable logs"))
					go a.stopHost()
				}
				return
			}
			if a.drafts == nil {
				a.drafts = make(map[string]string)
			}
			a.drafts[a.sessionID] = a.model.Draft
			a.sessionID = summary.ID
			a.model.SessionID = summary.ID
			a.model.SessionCWD = summary.CWD
			a.nextSeq = nextSeq
			a.promptID++
			a.promptDone = nil
			a.awaitingTurn = false
			a.sawActivity = false
			a.cancelIdle = false
			a.model.CancelPending = false
			a.transcript = transcript
			a.stream = projection.AssistantStream{}
			a.model.ResetTranscript(transcript.Items())
			a.model.Draft = a.drafts[summary.ID]
			a.model.SetActiveRoute(view.ModelRoute{Provider: summary.Provider, Model: summary.Model, Effort: summary.Effort})
			a.model.Error = ""
			a.model.ErrorDetail = ""
			a.model.SessionsOpen = false
			for _, notification := range buffer {
				if notification.method != "session/assistant-stream" {
					a.handleNotification(notification.method, notification.params)
				}
			}
			a.publishTranscript()
		})
	}()
}

func (a *App) bufferSessionNotification(method string, params json.RawMessage) bool {
	if !a.model.SessionSwitching || a.loadingSession == "" {
		return false
	}
	switch method {
	case "session/event", "session/status", "session/assistant-stream", "approval/request", "approval/closed", "question/request", "question/closed":
	default:
		return false
	}
	var envelope struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(params, &envelope) != nil || envelope.SessionID != a.loadingSession {
		return false
	}
	if method == "session/assistant-stream" {
		return true
	}
	if len(a.sessionBuffer) >= 1024 || a.sessionBufferBytes+len(params) > 8*1024*1024 {
		a.setFailure("Too many session updates while loading history", errors.New("Restart the host to recover from the durable log"))
		go a.stopHost()
		return true
	}
	a.sessionBuffer = append(a.sessionBuffer, hostNotification{method: method, params: params})
	a.sessionBufferBytes += len(params)
	return true
}
