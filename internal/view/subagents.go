package view

import (
	"fmt"
	"strings"
)

// SubagentState is the transient live status of one child session owned by the
// current session. It never enters the transcript and is cleared on switch.
type SubagentState struct {
	ID       string
	Provider string
	Running  bool
	Reason   string
}

// subagentLabel summarizes live child sessions for the header. It stays empty
// when none were reported, so ordinary sessions keep their compact header.
func subagentLabel(model *Model) string {
	running, done := 0, 0
	for _, subagent := range model.Subagents {
		if subagent.Running {
			running++
		} else {
			done++
		}
	}
	if running == 0 && done == 0 {
		return ""
	}
	parts := []string{model.T("Subagents")}
	if running > 0 {
		parts = append(parts, fmt.Sprintf(model.T("%d running"), running))
	}
	if done > 0 {
		parts = append(parts, fmt.Sprintf(model.T("%d done"), done))
	}
	return strings.Join(parts, " · ")
}
