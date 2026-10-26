package protocol

import "encoding/json"

const (
	NotificationSessionEvent           = "session/event"
	NotificationSessionStatus          = "session/status"
	NotificationSessionAssistantStream = "session/assistant-stream"
	NotificationApprovalRequest        = "approval/request"
	NotificationApprovalClosed         = "approval/closed"
	NotificationQuestionRequest        = "question/request"
	NotificationQuestionClosed         = "question/closed"
	NotificationSubagentStarted        = "subagent/started"
	NotificationSubagentFinished       = "subagent/finished"
	NotificationHostFatal              = "host/fatal"

	// These dotted names belong only to explicit SDK regression mode.
	NotificationSDKSessionEvent  = "session.event"
	NotificationSDKSessionStatus = "session.status"
)

// SessionEventNotification is the outer session/event envelope. The committed
// event stays raw for projection's event decoder.
type SessionEventNotification struct {
	SessionID string          `json:"sessionId"`
	Seq       *uint64         `json:"seq"`
	Event     json.RawMessage `json:"event"`
}

type SessionStatusNotification struct {
	SessionID      string  `json:"sessionId"`
	Status         string  `json:"status"`
	StatusRevision *uint64 `json:"statusRevision"`
}

// AssistantStreamNotification is the outer stream envelope. The frame stays
// raw for projection.AssistantStream.
type AssistantStreamNotification struct {
	SessionID string          `json:"sessionId"`
	Frame     json.RawMessage `json:"frame"`
}
