package interaction

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"dsh-mygo/internal/protocol"
)

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type Question struct {
	ID          string           `json:"id"`
	Question    string           `json:"question"`
	Header      string           `json:"header,omitempty"`
	Detail      string           `json:"detail,omitempty"`
	Options     []QuestionOption `json:"options,omitempty"`
	MultiSelect bool             `json:"multiSelect,omitempty"`
}

type QuestionAnswer struct {
	ID       string   `json:"id"`
	Selected []string `json:"selected"`
	Custom   string   `json:"custom,omitempty"`
}

type QuestionDraft struct {
	Selected []bool
	Custom   string
}

type QuestionRequest struct {
	RequestID  string          `json:"requestId"`
	SessionID  string          `json:"sessionId"`
	Questions  []Question      `json:"questions"`
	Drafts     []QuestionDraft `json:"-"`
	Responding bool            `json:"-"`
	Error      string          `json:"-"`
}

func validQuestionText(value string, limit int, required bool) bool {
	return len(value) <= limit && !strings.ContainsRune(value, '\x00') && (!required || strings.TrimSpace(value) != "")
}

func DecodeQuestion(params json.RawMessage) (*QuestionRequest, error) {
	var request QuestionRequest
	var fields map[string]json.RawMessage
	invalid := errors.New("invalid question request")
	if len(params) > 131072 || json.Unmarshal(params, &request) != nil || json.Unmarshal(params, &fields) != nil || !validID(request.RequestID) || request.SessionID != "" && !validID(request.SessionID) || len(request.Questions) == 0 || len(request.Questions) > 8 {
		return nil, invalid
	}
	if raw := bytes.TrimSpace(fields["sessionId"]); len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, invalid
	}
	var rawQuestions []map[string]json.RawMessage
	if json.Unmarshal(fields["questions"], &rawQuestions) != nil {
		return nil, invalid
	}
	ids := make(map[string]bool)
	for index, question := range request.Questions {
		if !validQuestionText(question.ID, 128, true) || ids[question.ID] || !validQuestionText(question.Question, 4096, true) || !validQuestionText(question.Header, 256, false) || !validQuestionText(question.Detail, 32768, false) || len(question.Options) > 16 {
			return nil, invalid
		}
		ids[question.ID] = true
		for _, field := range []string{"header", "detail", "options", "multiSelect"} {
			if raw, present := rawQuestions[index][field]; present && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return nil, invalid
			}
		}
		labels := make(map[string]bool)
		for _, option := range question.Options {
			if !validQuestionText(option.Label, 256, true) || labels[option.Label] || !validQuestionText(option.Description, 2048, false) {
				return nil, invalid
			}
			labels[option.Label] = true
		}
		request.Drafts = append(request.Drafts, QuestionDraft{Selected: make([]bool, len(question.Options))})
	}
	encoded, _ := json.Marshal(request.Questions)
	if len(encoded) > 65536 {
		return nil, invalid
	}
	return &request, nil
}

func DecodeQuestionClosed(params json.RawMessage) (string, string, error) {
	var closed protocol.QuestionClosedParams
	if json.Unmarshal(params, &closed) != nil || !validID(closed.RequestID) || closed.SessionID == nil || *closed.SessionID != "" && !validID(*closed.SessionID) {
		return "", "", errors.New("invalid question close notification")
	}
	return closed.RequestID, *closed.SessionID, nil
}

func (request *QuestionRequest) Answers() ([]QuestionAnswer, error) {
	invalid := errors.New("Answer every question; choose one option or enter text for a single-choice question.")
	if len(request.Drafts) != len(request.Questions) {
		return nil, invalid
	}
	answers := make([]QuestionAnswer, 0, len(request.Questions))
	for index, question := range request.Questions {
		draft := request.Drafts[index]
		if len(draft.Selected) != len(question.Options) || !validQuestionText(draft.Custom, 8192, false) {
			return nil, invalid
		}
		answer := QuestionAnswer{ID: question.ID, Selected: []string{}, Custom: draft.Custom}
		for optionIndex, selected := range draft.Selected {
			if selected {
				answer.Selected = append(answer.Selected, question.Options[optionIndex].Label)
			}
		}
		count := len(answer.Selected)
		if strings.TrimSpace(answer.Custom) != "" {
			count++
		}
		if count == 0 || !question.MultiSelect && count != 1 {
			return nil, invalid
		}
		answers = append(answers, answer)
	}
	encoded, _ := json.Marshal(answers)
	if len(encoded) > 65536 {
		return nil, invalid
	}
	return answers, nil
}

type QuestionQueue struct {
	items []*QuestionRequest
	seen  map[string][32]byte
}

func (queue *QuestionQueue) Items() []*QuestionRequest { return queue.items }

func (queue *QuestionQueue) Add(request *QuestionRequest) error {
	encoded, _ := json.Marshal(request)
	fingerprint := sha256.Sum256(encoded)
	if previous, seen := queue.seen[request.RequestID]; seen {
		for _, existing := range queue.items {
			if existing.RequestID == request.RequestID && previous == fingerprint {
				return nil
			}
		}
		return errors.New("question identity reused or changed")
	}
	if len(queue.items) >= 64 || len(queue.seen) >= 65536 {
		return errors.New("question queue limit exceeded")
	}
	if queue.seen == nil {
		queue.seen = make(map[string][32]byte)
	}
	queue.seen[request.RequestID] = fingerprint
	queue.items = append(slices.Clone(queue.items), request)
	return nil
}

func (queue *QuestionQueue) Begin(requestID string) bool {
	if len(queue.items) == 0 || queue.items[0].RequestID != requestID || queue.items[0].Responding {
		return false
	}
	queue.items[0].Responding = true
	queue.items[0].Error = ""
	return true
}

func (queue *QuestionQueue) Fail(requestID, message string) {
	for _, request := range queue.items {
		if request.RequestID == requestID {
			request.Responding = false
			request.Error = message
			return
		}
	}
}

func (queue *QuestionQueue) Remove(requestID, sessionID string) {
	queue.items = slices.DeleteFunc(slices.Clone(queue.items), func(request *QuestionRequest) bool {
		return request.RequestID == requestID && request.SessionID == sessionID
	})
}

func (queue *QuestionQueue) RemoveSession(sessionID string) {
	queue.items = slices.DeleteFunc(slices.Clone(queue.items), func(request *QuestionRequest) bool { return request.SessionID == sessionID })
}

func (queue *QuestionQueue) Clear() { queue.items = nil }
