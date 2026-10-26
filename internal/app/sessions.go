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
	return a.protocol.richUI() && !a.shuttingDown.Load() && a.model.CanChooseSession() && a.currentClient() != nil
}

func (a *App) OpenSessions() {
	if a.protocol.enabled() || a.shuttingDown.Load() || a.currentClient() == nil || !a.model.CanRefreshSessions() {
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
	if a.protocol.enabled() || a.shuttingDown.Load() || !a.model.CanRefreshSessions() || a.currentClient() == nil {
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
			err = errors.New(msgSessionOpenFailed)
		} else {
			var result protocol.SessionResult
			if json.Unmarshal(raw, &result) != nil {
				err = errors.New(msgSessionInvalidResponse)
			} else {
				summary, err = decodeSessionSummary(result.Session)
				if err == nil && (summary.Status != "idle" || summary.NextSeq == nil || id != "" && summary.ID != id || summary.Provider == "" || summary.Model == "" || summary.CWD == "") {
					err = errors.New(msgSessionInvalidRestore)
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
				err = errors.New(msgSessionPermissionBoundary)
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
					a.setFailure(msgSessionChanged, errors.New("Restart the host to recover the session safely"))
					go a.stopHost()
				}
				return
			}
			if a.model.Phase != view.PhaseIdle || len(a.model.Approvals) > 0 || len(a.model.Questions) > 0 {
				a.model.SessionsError = msgSessionFinishInteraction
				if len(buffer) > 0 {
					a.setFailure(msgSessionConcurrent, errors.New("Restart the host to recover from the durable logs"))
					go a.stopHost()
				}
				return
			}
			a.sessionChanged(sessionSwitchResult{
				summary: summary, transcript: transcript, nextSeq: nextSeq,
				history: history, permissions: permissions, canPermissions: canPermissions,
				workspaceRequired: workspaceRequired, buffer: buffer, previousID: currentID,
			})
		})
	}()
}

// sessionSwitchResult is the complete validated replacement for the visible
// conversation. Notifications buffered during the read are replayed after the
// new identity is installed.
type sessionSwitchResult struct {
	summary           protocol.SessionSummary
	transcript        projection.Transcript
	nextSeq           uint64
	history           historySnapshot
	permissions       view.PermissionSnapshot
	canPermissions    bool
	workspaceRequired bool
	buffer            []hostNotification
	previousID        string
}

// sessionChanged publishes one session replacement. Order matters: save the
// outgoing draft, replace identity, reset conversation-scoped resources and
// projection, then expose the new route/session state and replay buffered
// notifications.
func (a *App) sessionChanged(result sessionSwitchResult) {
	a.localComponent.setDraft(a.sessionID, a.model.Draft)
	a.sessionID = result.summary.ID
	a.retireSession(result.previousID)
	delete(a.retiredSessions, result.summary.ID)
	a.model.SessionID = result.summary.ID
	a.subagents.sessionChanged(result.summary.ID)
	a.model.Subagents = a.subagents.summariesView()
	// Staged uploads are conversation-local and never follow a switch.
	a.model.Attachments.Clear()
	a.model.SessionCWD = result.summary.CWD
	a.syncWorkspace()
	a.model.Setup.WorkspaceRequired = result.workspaceRequired
	a.model.Setup.Error = ""
	if result.canPermissions {
		a.model.Permission.Accept(result.permissions)
	}
	a.model.SessionPanel.ActiveArchived = result.summary.Archived
	a.upsertSession(sessionSummaryView(result.summary))
	a.nextSeq = result.nextSeq
	a.statusRevision = 0
	a.hostStatus = ""
	a.sessionCatalog.clearTitle()
	a.turn.reset()
	a.model.CancelPending = false
	a.transcript = result.transcript
	a.stream = projection.AssistantStream{}
	a.model.ResetTranscript(result.transcript.Items())
	a.acceptHistoryWindow(result.history)
	a.model.Draft = a.localComponent.draft(result.summary.ID)
	if a.local != nil {
		a.local.lastSession = result.summary.ID
	}
	a.persistLocalState()
	a.model.SetActiveRoute(view.ModelRoute{Provider: result.summary.Provider, Model: result.summary.Model, Effort: result.summary.Effort})
	a.model.Error = ""
	a.model.ErrorDetail = ""
	a.model.SessionsOpen = false
	for _, notification := range result.buffer {
		if notification.method != protocol.NotificationSessionAssistantStream {
			a.processNotification(notification)
		}
	}
	a.publishTranscript()
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
			a.setFailure(msgSessionTooManyUpdates, errors.New("Restart the host to recover from the durable log"))
			go a.stopHost()
		}
		return true
	}
	return true
}
