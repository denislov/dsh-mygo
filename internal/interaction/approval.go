package interaction

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

const MaxPendingApprovals = 64
const maxApprovalIdentities = 65536

type Approval struct {
	RequestID  string `json:"requestId"`
	SessionID  string `json:"sessionId"`
	ToolName   string `json:"toolName"`
	Reason     string `json:"reason"`
	CallID     string `json:"callId"`
	Arguments  string `json:"arguments"`
	External   bool   `json:"external"`
	Responding bool   `json:"-"`
	Error      string `json:"-"`
}

func validID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character != '-' && character != '_' && !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}

func DecodeApproval(params json.RawMessage) (Approval, error) {
	var request Approval
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &request) != nil || json.Unmarshal(params, &fields) != nil || !validID(request.RequestID) || !validID(request.SessionID) ||
		len(request.ToolName) == 0 || len(request.ToolName) > 256 || len(request.Reason) > 4096 || len(request.CallID) > 256 || len(request.Arguments) > 65536 {
		return Approval{}, errors.New("invalid approval request")
	}
	external := bytes.TrimSpace(fields["external"])
	if !bytes.Equal(external, []byte("true")) && !bytes.Equal(external, []byte("false")) || !request.External && (request.CallID == "" || request.Arguments == "") {
		return Approval{}, errors.New("invalid approval tool association")
	}
	for _, field := range []string{"reason", "callId", "arguments"} {
		if raw, present := fields[field]; present && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return Approval{}, errors.New("invalid approval field type")
		}
	}
	for _, value := range []string{request.ToolName, request.Reason, request.CallID, request.Arguments} {
		if strings.ContainsRune(value, '\x00') {
			return Approval{}, errors.New("invalid approval text")
		}
	}
	return request, nil
}

func DecodeApprovalClosed(params json.RawMessage) (string, string, error) {
	var closed struct {
		RequestID string `json:"requestId"`
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(params, &closed) != nil || !validID(closed.RequestID) || !validID(closed.SessionID) {
		return "", "", errors.New("invalid approval close notification")
	}
	return closed.RequestID, closed.SessionID, nil
}

type ApprovalQueue struct {
	items []Approval
	seen  map[string]struct{}
}

func (queue *ApprovalQueue) Items() []Approval { return queue.items }

func (queue *ApprovalQueue) Add(request Approval) error {
	for _, existing := range queue.items {
		if existing.RequestID != request.RequestID {
			continue
		}
		existing.Responding = false
		existing.Error = ""
		if existing != request {
			return errors.New("approval identity changed")
		}
		return nil
	}
	if _, reused := queue.seen[request.RequestID]; reused {
		return errors.New("approval identity reused")
	}
	if len(queue.items) >= MaxPendingApprovals || len(queue.seen) >= maxApprovalIdentities {
		return errors.New("approval queue limit exceeded")
	}
	if queue.seen == nil {
		queue.seen = make(map[string]struct{})
	}
	queue.seen[request.RequestID] = struct{}{}
	queue.items = append(slices.Clone(queue.items), request)
	return nil
}

func (queue *ApprovalQueue) Begin(requestID string) bool {
	if len(queue.items) == 0 || queue.items[0].RequestID != requestID || queue.items[0].Responding {
		return false
	}
	queue.items = slices.Clone(queue.items)
	queue.items[0].Responding = true
	queue.items[0].Error = ""
	return true
}

func (queue *ApprovalQueue) Fail(requestID, message string) {
	for index, request := range queue.items {
		if request.RequestID == requestID {
			queue.items = slices.Clone(queue.items)
			queue.items[index].Responding = false
			queue.items[index].Error = message
			return
		}
	}
}

func (queue *ApprovalQueue) Remove(requestID, sessionID string) bool {
	for index, request := range queue.items {
		if request.RequestID == requestID && request.SessionID == sessionID {
			queue.items = slices.Delete(slices.Clone(queue.items), index, index+1)
			return true
		}
	}
	return false
}

func (queue *ApprovalQueue) Clear() { queue.items = nil }
