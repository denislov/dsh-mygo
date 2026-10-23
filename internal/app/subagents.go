package app

import (
	"encoding/json"

	"dsh-mygo/internal/view"
)

// maxTrackedSubagents bounds the transient child status list; the newest child
// wins once the cap is reached.
const maxTrackedSubagents = 8

var subagentReasons = map[string]bool{
	"completed": true, "aborted": true, "error": true, "max-tokens": true, "refusal": true,
}

// handleSubagentNotification applies one live child status from the Host. Only
// children of the current session are tracked; stale or foreign parents are
// dropped so a notification window cannot leak between sessions.
func (a *App) handleSubagentNotification(params json.RawMessage, finished bool) {
	var payload struct {
		ParentSessionID string `json:"parentSessionId"`
		ChildSessionID  string `json:"childSessionId"`
		Provider        string `json:"provider"`
		Reason          string `json:"reason"`
	}
	if json.Unmarshal(params, &payload) != nil {
		return
	}
	if payload.ParentSessionID == "" || payload.ParentSessionID != a.model.SessionID {
		return
	}
	if !safeSubagentID(payload.ChildSessionID) {
		return
	}
	state := view.SubagentState{ID: payload.ChildSessionID, Running: !finished}
	if safeSubagentID(payload.Provider) {
		state.Provider = payload.Provider
	}
	if finished {
		if subagentReasons[payload.Reason] {
			state.Reason = payload.Reason
		} else {
			state.Reason = "error"
		}
	}
	for index := range a.model.Subagents {
		if a.model.Subagents[index].ID == state.ID {
			a.model.Subagents[index] = state
			return
		}
	}
	if len(a.model.Subagents) >= maxTrackedSubagents {
		a.model.Subagents = a.model.Subagents[1:]
	}
	a.model.Subagents = append(a.model.Subagents, state)
}

// safeSubagentID mirrors the Host's identifier bound before display.
func safeSubagentID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '.', character == '_', character == ':', character == '-':
		default:
			return false
		}
	}
	return true
}
