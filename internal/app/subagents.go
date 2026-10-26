package app

import (
	"encoding/json"

	"dsh-mygo/internal/protocol"
)

var subagentReasons = map[string]bool{
	protocol.SubagentReasonCompleted: true, protocol.SubagentReasonAborted: true, protocol.SubagentReasonError: true,
	protocol.SubagentReasonMaxTokens: true, protocol.SubagentReasonRefusal: true,
}

// handleSubagentNotification applies one live child status from the Host. Only
// children of the current session are tracked; stale or foreign parents are
// dropped so a notification window cannot leak between sessions.
func (a *App) handleSubagentNotification(params json.RawMessage, finished bool) {
	if a.subagents.applyStatus(params, finished) {
		a.model.Subagents = a.subagents.summariesView()
	}
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
