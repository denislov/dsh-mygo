// Package projection turns committed DeepSeek Harness session events and
// temporary assistant frames into native UI rows. Transcript reads the durable
// session log; AssistantStream never persists its preview or writes to the host.
package projection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kind classifies one transcript row.
type Kind int

const (
	// KindUser is a direct human prompt.
	KindUser Kind = iota
	// KindAssistant is assistant text, either committed or a temporary preview.
	KindAssistant
	// KindTool is one tool call, updated when its result arrives.
	KindTool
	// KindNotice is a turn-level message such as a failure or cancellation.
	KindNotice
)

// Item is one row the transcript list renders. ID is stable for the lifetime
// of the row so the list keeps its scroll position and row state.
type Item struct {
	Seq         uint64
	ID          string
	Kind        Kind
	Title       string
	Text        string
	Detail      string
	State       string
	IsError     bool
	Tool        *ToolDetails
	ToolEvicted bool
	Truncated   bool
	// Attachments lists durable uploads referenced by this message, in wire
	// order. Content bytes never enter the projection.
	Attachments []Attachment
}

// Attachment is the display-safe metadata of one durable upload. The ID is an
// opaque storage reference; it is never a path or a bearer URL.
type Attachment struct {
	ID        string
	MediaType string
	Bytes     int64
	Width     int
	Height    int
	Name      string
}

// Event is one session-log event exactly as the SDK forwards it.
type Event struct {
	Type string          `json:"type"`
	Seq  uint64          `json:"seq"`
	Time int64           `json:"time"`
	Data json.RawMessage `json:"data"`
}

// Notification is the session.event notification envelope.
type Notification struct {
	SessionID string `json:"sessionId"`
	Event     Event  `json:"event"`
}

// DecodeNotification parses a session.event notification envelope.
func DecodeNotification(params json.RawMessage) (Notification, error) {
	var notification Notification
	if err := json.Unmarshal(params, &notification); err != nil {
		return Notification{}, fmt.Errorf("decode session.event: %w", err)
	}
	if notification.SessionID == "" {
		return Notification{}, fmt.Errorf("decode session.event: missing sessionId")
	}
	if notification.Event.Type == "" {
		return Notification{}, fmt.Errorf("decode session.event: missing event type")
	}
	return notification, nil
}

// Transcript is the ordered set of rows projected from one session's log.
// The zero value is ready to use.
type Transcript struct {
	items           []Item
	seen            []sequenceRange
	toolIndex       map[string]int
	toolDetailBytes int
}

// Items returns the rows in session-log order. Callers must treat the slice
// as read-only; Apply replaces its backing array.
func (t *Transcript) Items() []Item { return t.items }

// Apply projects one committed event. It reports whether the transcript
// changed, so the caller can skip redrawing for events the UI does not show.
func (t *Transcript) Apply(event Event) bool {
	if !t.markSeen(event.Seq, event.Seq) {
		return false
	}

	switch event.Type {
	case "user/message":
		return t.applyUser(event)
	case "assistant/message":
		return t.applyAssistant(event)
	case "tool/call":
		return t.applyToolCall(event)
	case "tool/result":
		return t.applyToolResult(event)
	case "turn/end":
		return t.applyTurnEnd(event)
	default:
		return false
	}
}

func (t *Transcript) applyUser(event Event) bool {
	var data struct {
		Source struct {
			Kind string `json:"kind"`
		} `json:"source"`
		Content []contentBlock `json:"content"`
	}
	if json.Unmarshal(event.Data, &data) != nil {
		return false
	}
	// Synthetic user-role context (runtime context, skills, reminders) is
	// model-visible but not part of the human conversation.
	if data.Source.Kind != "user" {
		return false
	}
	text := userText(data.Content, maxMessageRunes)
	attachments := itemAttachments(data.Content)
	if text == "" && len(attachments) == 0 {
		return false
	}
	t.add(Item{Seq: event.Seq, ID: eventID(event), Kind: KindUser, Title: "You", Text: text, Truncated: utf8.RuneCountInString(text) > maxMessageRunes, Attachments: attachments})
	return true
}

func (t *Transcript) applyAssistant(event Event) bool {
	var data struct {
		Message struct {
			Content []contentBlock `json:"content"`
		} `json:"message"`
		Interrupted bool `json:"interrupted"`
	}
	if json.Unmarshal(event.Data, &data) != nil {
		return false
	}
	text, truncated := assistantSource(data.Message.Content)
	if text == "" {
		return false
	}
	item := Item{Seq: event.Seq, ID: eventID(event), Kind: KindAssistant, Title: "Assistant", Text: text, Truncated: truncated}
	if data.Interrupted {
		item.Detail = "Interrupted"
		item.State = "interrupted"
	}
	t.add(item)
	return true
}

func (t *Transcript) applyToolCall(event Event) bool {
	var data struct {
		CallID    string `json:"callId"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if json.Unmarshal(event.Data, &data) != nil {
		return false
	}
	if data.Name == "" {
		return false
	}
	id := "tool:" + data.CallID
	if data.CallID == "" {
		id = eventID(event)
	}
	details := &ToolDetails{}
	details.Arguments, details.ArgumentsTruncated = boundedToolText(data.Arguments, MaxToolDetailBytes)
	item := Item{
		Seq:   event.Seq,
		ID:    id,
		Kind:  KindTool,
		Title: "Tool · " + data.Name,
		Text:  prettyJSON(details.Arguments, maxToolRunes),
		State: "running",
	}
	if index, exists := t.toolIndex[data.CallID]; exists && data.CallID != "" {
		if previous := t.items[index].Tool; previous != nil {
			*details = *previous
			details.Arguments, details.ArgumentsTruncated = boundedToolText(data.Arguments, MaxToolDetailBytes)
		} else {
			details.Notice = "Earlier tool details were evicted."
		}
		t.setToolDetails(index, details)
		t.items[index].Title = item.Title
		t.items[index].Text = item.Text
		return true
	}
	t.add(item)
	t.setToolDetails(len(t.items)-1, details)
	if data.CallID != "" {
		if t.toolIndex == nil {
			t.toolIndex = make(map[string]int)
		}
		t.toolIndex[data.CallID] = len(t.items) - 1
	}
	return true
}

func (t *Transcript) applyToolResult(event Event) bool {
	var data struct {
		Message struct {
			ToolCallID string         `json:"toolCallId"`
			Content    []contentBlock `json:"content"`
			IsError    bool           `json:"isError"`
		} `json:"message"`
		Error *struct {
			Reason string `json:"reason"`
		} `json:"error"`
		Meta json.RawMessage `json:"meta"`
	}
	if json.Unmarshal(event.Data, &data) != nil {
		return false
	}
	full := &ToolDetails{}
	full.Result, full.ResultTruncated = toolResultText(data.Message.Content)
	full.Diffs, full.DiffNotice = projectDiffs(data.Meta)
	detail := clip(full.Result, maxToolRunes)
	if data.Error != nil && data.Error.Reason != "" {
		reason, _ := boundedToolText(data.Error.Reason, MaxToolDetailBytes)
		detail = clip(reason, maxToolRunes)
	}
	isError := data.Message.IsError || data.Error != nil
	state := "done"
	if isError {
		state = "error"
	}
	callID := data.Message.ToolCallID
	if index, ok := t.toolIndex[callID]; ok && callID != "" {
		if previous := t.items[index].Tool; previous != nil {
			full.Arguments, full.ArgumentsTruncated = previous.Arguments, previous.ArgumentsTruncated
		} else {
			full.Notice = "Earlier tool details were evicted."
		}
		t.setToolDetails(index, full)
		item := &t.items[index]
		item.Detail = detail
		item.State = state
		item.IsError = isError
		return true
	}
	id := "tool:" + callID
	if callID == "" {
		id = eventID(event)
	}
	t.add(Item{
		Seq:     event.Seq,
		ID:      id,
		Kind:    KindTool,
		Title:   "Tool result",
		Detail:  detail,
		State:   state,
		IsError: isError,
	})
	t.setToolDetails(len(t.items)-1, full)
	if callID != "" {
		if t.toolIndex == nil {
			t.toolIndex = make(map[string]int)
		}
		t.toolIndex[callID] = len(t.items) - 1
	}
	return true
}

func (t *Transcript) applyTurnEnd(event Event) bool {
	var data struct {
		Reason struct {
			Kind   string `json:"kind"`
			Reason struct {
				Kind string `json:"kind"`
			} `json:"reason"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"reason"`
	}
	if json.Unmarshal(event.Data, &data) != nil {
		return false
	}
	if data.Reason.Kind == "completed" || data.Reason.Kind == "" {
		return false
	}
	text := "Turn ended: " + data.Reason.Kind
	switch data.Reason.Kind {
	case "aborted":
		text = "Turn canceled"
		if data.Reason.Reason.Kind != "" {
			text += " (" + data.Reason.Reason.Kind + ")"
		}
	case "error":
		text = "Turn failed"
		if data.Reason.Error.Message != "" {
			text += ": " + data.Reason.Error.Message
		}
	}
	t.add(Item{
		Seq:     event.Seq,
		ID:      eventID(event),
		Kind:    KindNotice,
		Title:   "Notice",
		Text:    clip(text, maxMessageRunes),
		State:   data.Reason.Kind,
		IsError: data.Reason.Kind == "error",
	})
	return true
}

func (t *Transcript) add(item Item) {
	t.items = append(t.items, item)
}

type contentBlock struct {
	Type       string         `json:"type"`
	Text       string         `json:"text"`
	Attachment *attachmentRef `json:"attachment"`
}

// attachmentRef is the durable reference the Host records in a user event.
type attachmentRef struct {
	AttachmentID string `json:"attachmentId"`
	MediaType    string `json:"mediaType"`
	Bytes        int64  `json:"bytes"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Name         string `json:"name"`
}

// userText renders the visible text of a user message. Uploads get their own
// rows: the UI shows their metadata rather than a placeholder.
func userText(blocks []contentBlock, limit int) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	return clip(strings.Join(parts, "\n\n"), limit)
}

// itemAttachments keeps well-formed durable image references in wire order.
// Malformed references are dropped rather than rendered with suspicious data.
func itemAttachments(blocks []contentBlock) []Attachment {
	var attachments []Attachment
	for _, block := range blocks {
		if block.Type != "image" || block.Attachment == nil {
			continue
		}
		ref := block.Attachment
		if len(attachments) == maxAttachmentsPerMessage || !safeReference(ref.AttachmentID) || !safeImageMediaType(ref.MediaType) {
			continue
		}
		attachment := Attachment{ID: ref.AttachmentID, MediaType: ref.MediaType, Bytes: ref.Bytes, Width: ref.Width, Height: ref.Height}
		if ref.Bytes < 0 || ref.Bytes > maxAttachmentBytes || ref.Width < 0 || ref.Width > maxAttachmentDimension || ref.Height < 0 || ref.Height > maxAttachmentDimension {
			continue
		}
		if ref.Name != "" && utf8.RuneCountInString(ref.Name) <= maxAttachmentNameRunes && !strings.ContainsFunc(ref.Name, unicode.IsControl) {
			attachment.Name = ref.Name
		}
		attachments = append(attachments, attachment)
	}
	return attachments
}

// safeReference checks the opaque attachment identity: printable ASCII only,
// bounded, never interpreted as a path.
func safeReference(id string) bool {
	if id == "" || len(id) > 256 {
		return false
	}
	for _, character := range []byte(id) {
		if character <= ' ' || character > '~' {
			return false
		}
	}
	return true
}

// safeImageMediaType accepts exactly the raster formats the attachment seam
// normalizes; markup or other image media types never reach the UI.
func safeImageMediaType(mediaType string) bool {
	switch mediaType {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

// prettyJSON formats tool arguments for display. Invalid JSON is shown as the
// model produced it, as the session log records it.
func prettyJSON(text string, limit int) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if len(text) > 16*1024 || strings.Count(text, "{")+strings.Count(text, "[") > 64 {
		return clip(text, limit)
	}
	var buffer bytes.Buffer
	if json.Indent(&buffer, []byte(text), "", "  ") != nil {
		return clip(text, limit)
	}
	return clip(buffer.String(), limit)
}

func clip(text string, limit int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + "…"
}

func eventID(event Event) string {
	return "seq:" + strconv.FormatUint(event.Seq, 10)
}

const (
	maxMessageRunes = 8000
	maxToolRunes    = 4000
	// Uploaded image references are display metadata only; bound everything a
	// hostile event could inflate.
	maxAttachmentsPerMessage = 8
	maxAttachmentNameRunes   = 128
	maxAttachmentBytes       = 200 * 1024 * 1024
	maxAttachmentDimension   = 8192
)
