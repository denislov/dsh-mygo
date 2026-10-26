package protocol

import "encoding/json"

// Error kinds the app consumes to change local state. Hosts may return other
// kinds; they remain opaque diagnostics.
const (
	ErrorKindSessionCancelTimeout = "session/cancel-timeout"
	ErrorKindApprovalStaleRequest = "approval/stale-request"
	ErrorKindQuestionStaleRequest = "question/stale-request"
)

type ErrorPayload struct {
	Kind string `json:"kind"`
}

// ErrorKind returns the structured kind from an RPC error payload, or "" when
// the payload is absent or malformed.
func ErrorKind(data json.RawMessage) string {
	var payload ErrorPayload
	if json.Unmarshal(data, &payload) != nil {
		return ""
	}
	return payload.Kind
}
