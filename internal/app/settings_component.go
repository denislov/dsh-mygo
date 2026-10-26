package app

import (
	"context"
	"time"
)

// settingsComponent owns the settings-domain operation token and the session
// it targets. Host settings and device preferences remain separate; the App
// publishes accepted snapshots into the view model and never treats a
// negotiated capability as a committed value.
type settingsComponent struct {
	settingsSlot  operationSlot
	targetSession string
}

func (s *settingsComponent) begin(timeout time.Duration, sessionID string) (context.Context, uint64, context.CancelFunc) {
	ctx, operation, cancel := s.settingsSlot.begin(timeout)
	s.targetSession = sessionID
	return ctx, operation, cancel
}

func (s *settingsComponent) invalidate() {
	s.settingsSlot.invalidate()
	s.targetSession = ""
}

func (s *settingsComponent) current(operation uint64, activeSession string) bool {
	return s.settingsSlot.valid(operation) && s.targetSession == activeSession
}

// hostChanged drops any in-flight settings operation. Device preferences are
// not owned here and are not discarded with a Host generation.
func (s *settingsComponent) hostChanged() { s.invalidate() }
