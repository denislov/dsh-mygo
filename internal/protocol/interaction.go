package protocol

const (
	ApprovalDecisionAllow = "allow"
	ApprovalDecisionDeny  = "deny"
)

type ApprovalRespondParams struct {
	RequestID string `json:"requestId"`
	Decision  string `json:"decision"`
}

type ApprovalClosedParams struct {
	RequestID string `json:"requestId"`
	SessionID string `json:"sessionId"`
}

type QuestionAnswer struct {
	ID       string   `json:"id"`
	Selected []string `json:"selected"`
	Custom   string   `json:"custom,omitempty"`
}

type QuestionRespondParams struct {
	RequestID string           `json:"requestId"`
	Answers   []QuestionAnswer `json:"answers"`
}

type QuestionCancelParams struct {
	RequestID string `json:"requestId"`
}

// QuestionClosedParams keeps sessionId as a pointer: a missing field is
// invalid while an explicitly empty session id denotes a global question.
type QuestionClosedParams struct {
	RequestID string  `json:"requestId"`
	SessionID *string `json:"sessionId"`
}
