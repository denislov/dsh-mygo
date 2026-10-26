package protocol

type SessionRenameParams struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"title"`
}

// SessionArchiveParams omits archived to let the Host apply its default
// (archive). An explicit false unarchives.
type SessionArchiveParams struct {
	SessionID string `json:"sessionId"`
	Archived  *bool  `json:"archived,omitempty"`
}
