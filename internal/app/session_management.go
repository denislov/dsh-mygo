package app

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

func (a *App) CancelSessionEdit() {
	if a.model.SessionPanel.Writing {
		return
	}
	a.model.SessionPanel.EditingID = ""
	a.model.SessionPanel.Title = ""
	a.model.SessionPanel.ArchiveID = ""
}

func (a *App) OpenSessionRename(id string) {
	if !a.canChooseSession() || !a.model.SessionPanel.CanRename || !validSessionID(id) {
		return
	}
	a.model.ToolInspector = nil
	a.model.SessionPanel.EditingID = id
	a.model.SessionPanel.Title = ""
	a.model.SessionsError = ""
	for _, session := range a.model.Sessions {
		if session.ID == id {
			a.model.SessionPanel.Title = session.Title
			break
		}
	}
}

func (a *App) RenameSession() {
	panel := &a.model.SessionPanel
	if !panel.CanRename || !validSessionID(panel.EditingID) || !a.canWriteSession() {
		return
	}
	title := panel.Title
	if strings.TrimSpace(title) == "" || len(title) > 1024 || strings.ContainsFunc(title, unicode.IsControl) {
		a.model.SessionsError = msgSessionTitleInvalid
		return
	}
	a.writeSession(panel.EditingID, title, nil)
}

func (a *App) SetSessionArchived(id string, archived bool) {
	if !a.model.SessionPanel.CanArchive || !validSessionID(id) || !a.canWriteSession() {
		return
	}
	a.writeSession(id, "", &archived)
}

func (a *App) canWriteSession() bool {
	return !a.options.SDKProtocol && !a.shuttingDown.Load() && a.currentClient() != nil && a.model.CanRefreshSessions() && !a.model.SessionPanel.Uncertain
}

func decodeManagedSession(raw json.RawMessage, id string) (protocol.SessionSummary, error) {
	var response protocol.SessionResult
	if json.Unmarshal(raw, &response) != nil {
		return protocol.SessionSummary{}, errors.New("Invalid session response")
	}
	summary, err := decodeSessionSummary(response.Session)
	if err == nil && (summary.ID != id || summary.Status != "idle" || summary.NextSeq == nil || summary.Archived || summary.Provider == "" || summary.Model == "") {
		err = errors.New("Invalid managed session identity or state")
	}
	return summary, err
}

func (a *App) writeSession(id, title string, archived *bool) {
	ctx, operation, cancel := a.beginSessionOperation()
	a.model.SessionPanel.Writing = true
	client, currentID, startSeq := a.currentClient(), a.sessionID, a.nextSeq
	canArchive := a.model.SessionPanel.CanArchive
	route := a.sessionRoute(a.model.CanConfigure)
	retired := a.retiredSessionIDs(id)
	go func() {
		defer cancel()
		var summary protocol.SessionSummary
		err := releaseSessions(ctx, client, retired)
		opened := false
		if err == nil && archived == nil {
			// Cold rename takes ownership through the same public resume route as navigation.
			if id != currentID {
				raw, resumeErr := client.Call(ctx, protocol.MethodSessionResume, protocol.SessionResumeParams{SessionID: id, SessionRoute: route})
				err = resumeErr
				opened = err == nil
				if err == nil {
					_, err = decodeManagedSession(raw, id)
				}
			}
			if err == nil {
				raw, renameErr := client.Call(ctx, protocol.MethodSessionRename, protocol.SessionRenameParams{SessionID: id, Title: title})
				err = renameErr
				if err == nil {
					summary, err = decodeManagedSession(raw, id)
				}
			}
		} else if err == nil {
			raw, archiveErr := client.Call(ctx, protocol.MethodSessionArchive, protocol.SessionArchiveParams{SessionID: id, Archived: archived})
			err = archiveErr
			var ack map[string]json.RawMessage
			if err == nil && (json.Unmarshal(raw, &ack) != nil || ack == nil || len(ack) != 0) {
				err = errors.New("Invalid archive response")
			}
		}
		var sessions []view.SessionSummary
		if err == nil {
			sessions, err = readSessionList(ctx, client, canArchive)
			if err == nil {
				confirmed := false
				for _, session := range sessions {
					if session.ID == id {
						confirmed = archived != nil && session.Archived == *archived || archived == nil && !session.Archived && session.Title == summary.Title
					}
				}
				if !confirmed {
					err = errors.New("Session mutation was not confirmed by the catalog")
				}
			}
		}
		a.post(func() {
			if !a.currentSessionOperation(operation) || a.sessionID != currentID {
				return
			}
			a.cancelSessionOperation()
			if err == nil {
				for _, released := range retired {
					delete(a.retiredSessions, released)
				}
			}
			if opened {
				a.retireSession(id)
			}
			if err == nil && archived == nil && id == currentID && (*summary.NextSeq <= startSeq || *summary.NextSeq > a.nextSeq || a.sessionTitleSeq <= startSeq || a.sessionTitle != summary.Title) {
				err = errors.New("The committed title event was not confirmed")
			}
			if err != nil {
				a.model.SessionPanel.Uncertain = true
				a.sessionNeedsHistory = archived == nil && id == currentID
				a.model.SessionsError = msgSessionConfirmFailed
				return
			}
			a.model.Sessions = sessions
			a.model.SessionPanel.Uncertain = false
			a.CancelSessionEdit()
			a.updateActiveArchived()
		})
	}()
}

func (a *App) upsertSession(summary view.SessionSummary) {
	for index, session := range a.model.Sessions {
		if session.ID == summary.ID {
			a.model.Sessions[index] = summary
			return
		}
	}
	a.model.Sessions = append([]view.SessionSummary{summary}, a.model.Sessions...)
}

func (a *App) updateActiveArchived() {
	for _, session := range a.model.Sessions {
		if session.ID == a.sessionID {
			a.model.SessionPanel.ActiveArchived = session.Archived
			return
		}
	}
}
