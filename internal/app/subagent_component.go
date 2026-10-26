package app

import (
	"context"
	"encoding/json"
	"time"

	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

// maxTrackedSubagents bounds the transient child status list; the newest child
// wins once the cap is reached.
const maxTrackedSubagents = 8

// subagentComponent separates the live status list from the read-only detail
// panel. The Host remains the only source of child history: the panel holds one
// page and never resumes or prompts a child.
type subagentComponent struct {
	read      operationSlot
	summaries []view.SubagentState
	sessionOf func() string
	session   string
	open      bool
	parent    string
	selected  string
	page      subagentPage
}

type subagentPage struct {
	start, inherited uint64
	hasMore, earlier bool
	endReason        string
}

type subagentRead struct {
	operation  uint64
	generation uint64
	parent     string
	child      string
	earlier    bool
}

func (s *subagentComponent) summariesView() []view.SubagentState { return s.summaries }

func (s *subagentComponent) currentSession() string {
	if s.sessionOf != nil {
		return s.sessionOf()
	}
	return s.session
}

// sessionChanged drops child state that belonged to another parent session.
func (s *subagentComponent) sessionChanged(session string) {
	if s.session == session {
		return
	}
	s.session = session
	s.summaries = nil
	s.close()
}

func (s *subagentComponent) reset(session string) {
	s.session = session
	s.summaries = nil
	s.close()
}

func (s *subagentComponent) hostChanged() {
	s.summaries = nil
	s.close()
}

// applyStatus validates and stores one live child status for the current
// parent. Foreign or stale parents are dropped.
func (s *subagentComponent) applyStatus(params json.RawMessage, finished bool) bool {
	var payload protocol.SubagentStatusParams
	if json.Unmarshal(params, &payload) != nil {
		return false
	}
	if payload.ParentSessionID == "" || payload.ParentSessionID != s.currentSession() || !safeSubagentID(payload.ChildSessionID) {
		return false
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
	for index := range s.summaries {
		if s.summaries[index].ID == state.ID {
			s.summaries[index] = state
			return true
		}
	}
	if len(s.summaries) >= maxTrackedSubagents {
		s.summaries = s.summaries[1:]
	}
	s.summaries = append(s.summaries, state)
	return true
}

func (s *subagentComponent) openFor(parent string) {
	s.close()
	s.open = true
	s.parent = parent
}

func (s *subagentComponent) close() {
	s.read.invalidate()
	s.open = false
	s.parent = ""
	s.selected = ""
	s.page = subagentPage{}
}

func (s *subagentComponent) selectChild(id string) {
	s.selected = id
	s.page = subagentPage{}
}

// before returns the cursor for the previous page of the selected child.
func (s *subagentComponent) before() (uint64, bool) {
	if !s.page.hasMore {
		return 0, false
	}
	return s.page.start, true
}

func (s *subagentComponent) beginRead(generation uint64, earlier bool) (context.Context, subagentRead, context.CancelFunc, bool) {
	if !s.open || s.parent != s.currentSession() {
		return nil, subagentRead{}, nil, false
	}
	if earlier && (s.selected == "" || !s.page.hasMore) {
		return nil, subagentRead{}, nil, false
	}
	s.read.invalidate()
	ctx, operation, cancel := s.read.begin(20 * time.Second)
	return ctx, subagentRead{
		operation: operation, generation: generation, parent: s.parent, child: s.selected, earlier: earlier,
	}, cancel, true
}

func (s *subagentComponent) acceptRead(read subagentRead, generation uint64) bool {
	return s.read.valid(read.operation) && s.open && read.generation == generation &&
		read.parent == s.currentSession() && read.parent == s.parent && read.child == s.selected
}

func (s *subagentComponent) finishRead() { s.read.invalidate() }

// commitPage applies one validated page and rejects a page whose inherited
// prefix no longer matches the first page.
func (s *subagentComponent) commitPage(read subagentRead, page subagentHistoryPage) (subagentPage, bool) {
	if read.earlier && s.page.inherited != page.Inherited {
		return subagentPage{}, false
	}
	endReason := page.EndReason
	if read.earlier {
		endReason = s.page.endReason
	}
	s.page = subagentPage{
		start: page.Start, inherited: page.Inherited, hasMore: page.HasMore,
		earlier: read.earlier, endReason: endReason,
	}
	return s.page, true
}
