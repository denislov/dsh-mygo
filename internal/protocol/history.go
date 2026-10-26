package protocol

import "encoding/json"

const (
	HistoryDirectionForward  = "forward"
	HistoryDirectionBackward = "backward"
)

type SessionListParams struct {
	Cursor          string `json:"cursor,omitempty"`
	Limit           int    `json:"limit"`
	IncludeArchived bool   `json:"includeArchived,omitempty"`
}

// SessionListResult keeps entries raw because the app validates summary
// presence and types before projecting them into view data.
type SessionListResult struct {
	Sessions   []json.RawMessage `json:"sessions"`
	NextCursor json.RawMessage   `json:"nextCursor"`
}

// SessionHistoryParams carries the shared history wire fields. Each read flow
// sets only its own subset; direction, cursor, live and baseline context stay
// the caller's validation responsibility.
type SessionHistoryParams struct {
	SessionID       string  `json:"sessionId"`
	Cursor          string  `json:"cursor,omitempty"`
	Limit           int     `json:"limit"`
	Direction       string  `json:"direction,omitempty"`
	BeforeSeq       *uint64 `json:"beforeSeq,omitempty"`
	Live            bool    `json:"live,omitempty"`
	AssistantStream bool    `json:"assistantStream,omitempty"`
}

// SessionHistoryResult leaves events, cursor and stream baseline raw because
// projection events and stream frames have their own verified decoders.
type SessionHistoryResult struct {
	Events          []json.RawMessage `json:"events"`
	StartSeq        *uint64           `json:"startSeq"`
	NextCursor      json.RawMessage   `json:"nextCursor"`
	HasMore         *bool             `json:"hasMore"`
	NextSeq         *uint64           `json:"nextSeq"`
	Status          string            `json:"status"`
	StatusRevision  *uint64           `json:"statusRevision"`
	AssistantStream json.RawMessage   `json:"assistantStream"`
}

type SessionReleaseParams struct {
	SessionID string `json:"sessionId"`
}
