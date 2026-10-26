package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

func (a *App) canChooseSession() bool {
	return !a.options.SDKProtocol && !a.shuttingDown.Load() && a.model.CanChooseSession() && a.currentClient() != nil
}

func (a *App) OpenSessions() {
	if a.options.SDKProtocol || a.shuttingDown.Load() || a.currentClient() == nil || !a.model.CanRefreshSessions() {
		return
	}
	a.model.SessionsOpen = true
	a.model.ToolInspector = nil
	a.RefreshSessions()
}

func (a *App) CloseSessions() {
	if a.model.SessionSwitching || a.model.SessionPanel.Writing {
		return
	}
	a.cancelSessionOperation()
	a.model.SessionsOpen = false
	a.CancelSessionEdit()
}

func (a *App) cancelSessionOperation() {
	a.sessionNavigator.invalidate()
	a.model.SessionBusy = false
	a.model.SessionSwitching = false
	a.model.SessionPanel.Refreshing = false
	a.model.SessionPanel.Writing = false
	a.model.History.FinishRead()
}

func (a *App) beginSessionOperation() (context.Context, uint64, context.CancelFunc) {
	a.CloseSubagents()
	a.cancelSessionOperation()
	a.model.SessionBusy = true
	a.model.SessionsError = ""
	return a.sessionNavigator.begin(60 * time.Second)
}

func (a *App) currentSessionOperation(operation uint64) bool {
	return a.sessionSlot.valid(operation) && !a.shuttingDown.Load() && a.model.Phase != view.PhaseFailed
}

func (a *App) RefreshSessions() {
	a.refreshSessions()
}

func (a *App) refreshSessions() {
	if a.options.SDKProtocol || a.shuttingDown.Load() || !a.model.CanRefreshSessions() || a.currentClient() == nil {
		return
	}
	ctx, operation, cancel := a.beginSessionOperation()
	// Catalog refresh never owns the global navigation/input gate; a foreground
	// action supersedes it through the operation token.
	a.model.SessionBusy = false
	a.model.SessionPanel.Refreshing = true
	client := a.currentClient()
	includeArchived := a.model.SessionPanel.CanArchive
	needsHistory, id, minimumSeq, window := a.sessionNeedsHistory, a.sessionID, a.nextSeq, a.canHistoryWindow
	go func() {
		defer cancel()
		sessions, err := readSessionList(ctx, client, includeArchived)
		var transcript projection.Transcript
		var nextSeq uint64
		var history historySnapshot
		if err == nil && needsHistory {
			history, err = readOpenHistory(ctx, client, id, minimumSeq, window)
			transcript, nextSeq = history.Transcript, history.NextSeq
		}
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
			a.model.SessionPanel.Uncertain = false
			if needsHistory && nextSeq >= a.nextSeq {
				a.transcript = transcript
				a.nextSeq = nextSeq
				a.acceptHistoryWindow(history)
				a.publishTranscript()
			}
			a.sessionNeedsHistory = false
			a.updateActiveArchived()
		})
	}()
}

func (a *App) NewSession() { a.loadSession("") }

// NewProjectSession only accepts a directory from the Host's visible catalog.
func (a *App) NewProjectSession(cwd string) {
	for _, session := range a.model.Sessions {
		if session.CWD == cwd && cwd != "" {
			a.loadSessionInDirectory("", cwd)
			return
		}
	}
}

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
	cwd := a.model.SessionCWD
	if cwd == "" {
		cwd = a.options.CWD
	}
	a.loadSessionInDirectory(id, cwd)
}

func (a *App) loadSessionInDirectory(id, cwd string) {
	if !a.canChooseSession() {
		return
	}
	workspaceRequired := a.model.Setup.WorkspaceRequired && id == "" && cwd == a.model.SessionCWD
	ctx, operation, cancel := a.beginSessionOperation()
	a.model.SessionSwitching = true
	a.loadingSession = id
	client := a.currentClient()
	route := a.sessionRoute(a.model.CanConfigure)
	method := protocol.MethodSessionResume
	var params any
	if id == "" {
		method = protocol.MethodSessionCreate
		params = protocol.SessionCreateParams{CWD: cwd, SessionRoute: route}
	} else {
		params = protocol.SessionResumeParams{SessionID: id, SessionRoute: route}
	}
	canPermissions := a.model.Permission.Available
	window, retired, currentID := a.canHistoryWindow, a.retiredSessionIDs(id), a.sessionID
	go func() {
		defer cancel()
		var summary protocol.SessionSummary
		var transcript projection.Transcript
		var nextSeq uint64
		var permissions view.PermissionSnapshot
		err := releaseSessions(ctx, client, retired)
		released := err == nil
		var raw json.RawMessage
		if err == nil {
			raw, err = client.Call(ctx, method, params)
		}
		opened := err == nil
		var history historySnapshot
		if err != nil {
			err = errors.New("Could not open the session. Check ownership, working directory and model route, then retry")
		} else {
			var result protocol.SessionResult
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
			history, err = readOpenHistory(ctx, client, summary.ID, *summary.NextSeq, window)
			transcript, nextSeq = history.Transcript, history.NextSeq
		}
		if err == nil && canPermissions {
			permissions, err = readPermissions(ctx, client, summary.ID)
			if err == nil && permissions.NextSeq != nextSeq {
				err = errors.New("Permission boundary changed while loading the session")
			}
		}
		a.post(func() {
			if !a.currentSessionOperation(operation) {
				return
			}
			buffer := a.sessionBuffer
			a.cancelSessionOperation()
			if released {
				for _, id := range retired {
					delete(a.retiredSessions, id)
				}
			}
			if err != nil {
				if opened {
					a.retireSession(summary.ID)
				}
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
			a.retireSession(currentID)
			delete(a.retiredSessions, summary.ID)
			a.model.SessionID = summary.ID
			a.model.Subagents = nil
			a.model.SessionCWD = summary.CWD
			a.syncWorkspace()
			a.model.Setup.WorkspaceRequired = workspaceRequired
			a.model.Setup.Error = ""
			if canPermissions {
				a.model.Permission.Accept(permissions)
			}
			a.model.SessionPanel.ActiveArchived = summary.Archived
			a.upsertSession(sessionSummaryView(summary))
			a.nextSeq = nextSeq
			a.statusRevision = 0
			a.hostStatus = ""
			a.sessionCatalog.clearTitle()
			a.turn.reset()
			a.model.CancelPending = false
			a.transcript = transcript
			a.stream = projection.AssistantStream{}
			a.model.ResetTranscript(transcript.Items())
			a.acceptHistoryWindow(history)
			a.model.Draft = a.drafts[summary.ID]
			if a.local != nil {
				a.local.lastSession = summary.ID
			}
			a.persistLocalState()
			a.model.SetActiveRoute(view.ModelRoute{Provider: summary.Provider, Model: summary.Model, Effort: summary.Effort})
			a.model.Error = ""
			a.model.ErrorDetail = ""
			a.model.SessionsOpen = false
			for _, notification := range buffer {
				if notification.method != protocol.NotificationSessionAssistantStream {
					a.processNotification(notification)
				}
			}
			a.publishTranscript()
		})
	}()
}

func (a *App) bufferSessionNotificationFrom(source *hostInstance, method string, params json.RawMessage) bool {
	if !a.model.SessionSwitching || a.loadingSession == "" {
		return false
	}
	switch method {
	case protocol.NotificationSessionEvent, protocol.NotificationSessionStatus, protocol.NotificationSessionAssistantStream,
		protocol.NotificationApprovalRequest, protocol.NotificationApprovalClosed,
		protocol.NotificationQuestionRequest, protocol.NotificationQuestionClosed:
	default:
		return false
	}
	var envelope struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(params, &envelope) != nil || envelope.SessionID != a.loadingSession {
		return false
	}
	if method == protocol.NotificationSessionAssistantStream {
		return true
	}
	ok, overflow := a.sessionNavigator.bufferResponse(hostNotification{host: source, method: method, params: params})
	if !ok {
		if overflow {
			a.setFailure("Too many session updates while loading history", errors.New("Restart the host to recover from the durable log"))
			go a.stopHost()
		}
		return true
	}
	return true
}
