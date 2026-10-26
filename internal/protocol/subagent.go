package protocol

import "encoding/json"

const (
	SubagentModeOneShot     = "one-shot"
	SubagentModeContinuable = "continuable"
	SubagentModeExternal    = "external"
	SubagentModeUnknown     = "unknown"
)

const (
	SubagentStatusStored   = "stored"
	SubagentStatusIdle     = "idle"
	SubagentStatusRunning  = "running"
	SubagentStatusExternal = "external"
)

const (
	SubagentReasonCompleted = "completed"
	SubagentReasonAborted   = "aborted"
	SubagentReasonError     = "error"
	SubagentReasonMaxTokens = "max-tokens"
	SubagentReasonRefusal   = "refusal"
)

type SubagentListParams struct {
	ParentSessionID string `json:"parentSessionId"`
}

// SubagentSummary keeps optional catalog fields as pointers so a missing
// label or timestamp stays distinguishable from an empty value.
type SubagentSummary struct {
	ID        string  `json:"id"`
	Label     *string `json:"label"`
	Mode      string  `json:"mode"`
	Status    string  `json:"status"`
	CreatedAt *int64  `json:"createdAt"`
}

type SubagentListResult struct {
	ParentSessionID string            `json:"parentSessionId"`
	Children        []SubagentSummary `json:"children"`
}

type SubagentHistoryParams struct {
	ParentSessionID string  `json:"parentSessionId"`
	ChildSessionID  string  `json:"childSessionId"`
	BeforeSeq       *uint64 `json:"beforeSeq,omitempty"`
}

// SubagentHistoryResult leaves events raw because the app's child projection
// verifies each event's sequence, time and display whitelist.
type SubagentHistoryResult struct {
	ParentSessionID     string            `json:"parentSessionId"`
	ChildSessionID      string            `json:"childSessionId"`
	StartSeq            *uint64           `json:"startSeq"`
	EndSeq              *uint64           `json:"endSeq"`
	NextSeq             *uint64           `json:"nextSeq"`
	InheritedEventCount *uint64           `json:"inheritedEventCount"`
	HasMore             *bool             `json:"hasMore"`
	Events              []json.RawMessage `json:"events"`
}

// SubagentStatusParams is shared by subagent/started and subagent/finished;
// reason is only meaningful when the child has finished.
type SubagentStatusParams struct {
	ParentSessionID string `json:"parentSessionId"`
	ChildSessionID  string `json:"childSessionId"`
	Provider        string `json:"provider,omitempty"`
	Reason          string `json:"reason,omitempty"`
}
