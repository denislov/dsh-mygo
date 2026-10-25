// Package projection turns committed DeepSeek Harness session events and
// temporary assistant frames into native UI rows. Transcript reads the durable
// session log; AssistantStream never persists its preview or writes to the host.
package projection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
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
	// KindReasoning is assistant thinking output. It is displayed inside an
	// activity group, never merged into the assistant text row.
	KindReasoning
)

// Item is one row the transcript list renders. ID is stable for the lifetime
// of the row so the list keeps its scroll position and row state.
type Item struct {
	Seq uint64
	// Turn and Step locate the row in the session log. Zero means the source
	// event did not carry a usable position; grouping tolerates that.
	Turn uint64
	Step uint64
	ID   string
	Kind Kind
	// Time is the event or first streaming chunk time in milliseconds.
	Time    int64
	EndTime int64
	// ActivityKey identifies a reasoning run across preview and commit. Tool
	// rows leave it empty; grouping derives their group from the first row.
	ActivityKey string
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

// UsageView is the latest token accounting an adapter reported for one step.
// It describes the model call that produced the newest assistant message; older
// totals are not aggregated because history windows may omit earlier steps.
type UsageView struct {
	HasUsage         bool
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	ReasoningTokens  int
	TotalTokens      int
}

// ContextView is the route capacity the Host recorded for the next request.
// Provider and Model are sanitized display strings, never request headers.
type ContextView struct {
	Provider string
	Model    string
	Window   int
}

// Task is one checklist row from the latest `todo/write` snapshot. Status is
// one of pending, in_progress or completed.
type Task struct {
	Content string
	Status  string
}

// Goal is the current durable goal snapshot in display form. Objective is
// single-line and bounded; Phase is one of active, paused, blocked, complete.
type Goal struct {
	Objective     string
	Phase         string
	MaxRounds     int
	RoundsStarted int
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
	// turn and step follow the newest log position so rows that omit their own
	// position can still join the right activity group.
	turn uint64
	step uint64
	// Usage and Context are session-scoped metrics, not transcript rows. They
	// follow the newest committed values and never advance nextSeq.
	Usage   UsageView
	Context ContextView
	// Tasks is the standing checklist from the latest todo/write snapshot; it
	// is cleared when a new turn starts, mirroring the Host's todos fold.
	Tasks []Task
	// PlanActive reports the latest plan/mode collaboration state.
	PlanActive bool
	// Goal is the current durable goal, nil before creation or after a clear.
	Goal *Goal
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
	case "request/context":
		return t.applyRequestContext(event)
	case "todo/write":
		return t.applyTodoWrite(event)
	case "plan/mode":
		return t.applyPlanMode(event)
	case "goal/change":
		return t.applyGoalChange(event)
	case "turn/start":
		return t.applyTurnStart(event)
	case "step/start", "step/end":
		return t.applyStep(event)
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
		Turn    json.RawMessage `json:"turn"`
		Step    json.RawMessage `json:"step"`
		Message struct {
			Content []contentBlock `json:"content"`
		} `json:"message"`
		Interrupted bool `json:"interrupted"`
	}
	if json.Unmarshal(event.Data, &data) != nil {
		return false
	}
	turn, step := t.position(parsePositionValue(data.Turn), parsePositionValue(data.Step))
	changed := false
	// Usage travels with the assistant message; a step may carry accounting
	// without visible text (a tool-only step), so it updates the banner too.
	if usage := assistantUsage(event.Data); usage != t.Usage {
		t.Usage = usage
		changed = true
	}
	runs := assistantRuns(data.Message.Content)
	reasoningRun, textRun := 0, 0
	for _, run := range runs {
		if run.reasoning {
			item := Item{
				Seq:         event.Seq,
				Turn:        turn,
				Step:        step,
				ID:          eventID(event) + ":r:" + strconv.Itoa(reasoningRun),
				Kind:        KindReasoning,
				Time:        event.Time,
				EndTime:     event.Time,
				ActivityKey: activityKey(turn, step, event.Seq, uint64(reasoningRun)),
				Title:       "Reasoning",
				Text:        run.text,
				Truncated:   run.truncated,
			}
			if data.Interrupted {
				item.State = "interrupted"
			}
			t.add(item)
			reasoningRun++
			continue
		}
		id := eventID(event)
		if textRun > 0 {
			id += ":t:" + strconv.Itoa(textRun)
		}
		item := Item{Seq: event.Seq, Turn: turn, Step: step, ID: id, Kind: KindAssistant, Time: event.Time, EndTime: event.Time, Title: "Assistant", Text: run.text, Truncated: run.truncated}
		if data.Interrupted {
			item.Detail = "Interrupted"
			item.State = "interrupted"
		}
		t.add(item)
		textRun++
	}
	return changed || len(runs) > 0
}

// position fills a missing turn/step from the newest tracked log position.
// Malformed position fields are treated as absent and never suppress content.
func (t *Transcript) position(turn, step uint64) (uint64, uint64) {
	if turn != 0 {
		t.turn = turn
	} else {
		turn = t.turn
	}
	if step != 0 {
		t.step = step
	} else {
		step = t.step
	}
	return turn, step
}

func parsePositionValue(raw json.RawMessage) uint64 {
	if len(raw) == 0 {
		return 0
	}
	value, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil || value == 0 || value > maxStreamInteger {
		return 0
	}
	return value
}

// eventPosition reads the turn/step pair shared by DSH session events.
func eventPosition(data json.RawMessage) (uint64, uint64) {
	var position struct {
		Turn json.RawMessage `json:"turn"`
		Step json.RawMessage `json:"step"`
	}
	if json.Unmarshal(data, &position) != nil {
		return 0, 0
	}
	return parsePositionValue(position.Turn), parsePositionValue(position.Step)
}

// activityKey keeps one reasoning run in the same activity group before and
// after its preview commits. Falls back to the event seq when DSH omits a
// position entirely.
func activityKey(turn, step, seq, run uint64) string {
	runLabel := strconv.FormatUint(run, 10)
	switch {
	case turn != 0 && step != 0:
		return "activity:" + strconv.FormatUint(turn, 10) + ":" + strconv.FormatUint(step, 10) + ":r:" + runLabel
	case turn != 0:
		return "activity:" + strconv.FormatUint(turn, 10) + ":r:" + runLabel
	default:
		return "activity:" + strconv.FormatUint(seq, 10) + ":r:" + runLabel
	}
}

// assistantUsage extracts the optional step accounting, dropping implausible
// numbers instead of trusting an adapter's raw counters.
func assistantUsage(data json.RawMessage) UsageView {
	var envelope struct {
		Usage *struct {
			InputTokens      int `json:"inputTokens"`
			OutputTokens     int `json:"outputTokens"`
			TotalTokens      int `json:"totalTokens"`
			CacheReadTokens  int `json:"cacheReadTokens"`
			CacheWriteTokens int `json:"cacheWriteTokens"`
			ReasoningTokens  int `json:"reasoningTokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Usage == nil {
		return UsageView{}
	}
	usage := UsageView{
		HasUsage:         true,
		InputTokens:      tokenCount(envelope.Usage.InputTokens),
		OutputTokens:     tokenCount(envelope.Usage.OutputTokens),
		CacheReadTokens:  tokenCount(envelope.Usage.CacheReadTokens),
		CacheWriteTokens: tokenCount(envelope.Usage.CacheWriteTokens),
		ReasoningTokens:  tokenCount(envelope.Usage.ReasoningTokens),
		TotalTokens:      tokenCount(envelope.Usage.TotalTokens),
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = clampTokens(usage.InputTokens, usage.OutputTokens)
	}
	return usage
}

// applyRequestContext records the route capacity from the latest log-only
// request context event. It never adds a transcript row.
func (t *Transcript) applyRequestContext(event Event) bool {
	var data struct {
		Provider      string `json:"provider"`
		Model         string `json:"model"`
		ContextWindow int    `json:"contextWindow"`
	}
	if json.Unmarshal(event.Data, &data) != nil {
		return false
	}
	context := ContextView{Window: tokenCount(data.ContextWindow)}
	if safeReference(data.Provider) {
		context.Provider = data.Provider
	}
	if safeReference(data.Model) {
		context.Model = data.Model
	}
	if context == t.Context {
		return false
	}
	t.Context = context
	return true
}

// applyTurnStart records the new turn position and clears the standing
// checklist the way the Host's `todos` projection does; finished lists stay
// visible until the next turn begins.
func (t *Transcript) applyTurnStart(event Event) bool {
	if turn, _ := eventPosition(event.Data); turn != 0 {
		t.turn, t.step = turn, 0
	}
	if len(t.Tasks) == 0 {
		return false
	}
	t.Tasks = nil
	return true
}

// applyStep follows the log position so rows that omit turn/step still join
// the current activity group. Step boundaries alone do not add a row.
func (t *Transcript) applyStep(event Event) bool {
	if turn, step := eventPosition(event.Data); turn != 0 || step != 0 {
		if turn != 0 {
			t.turn = turn
		}
		if step != 0 {
			t.step = step
		}
	}
	return false
}

// applyTodoWrite replaces the standing checklist from one whole-list snapshot.
// Malformed rows are skipped rather than trusted, and nothing becomes a
// transcript row.
func (t *Transcript) applyTodoWrite(event Event) bool {
	var data struct {
		Todos []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	}
	if json.Unmarshal(event.Data, &data) != nil {
		return false
	}
	tasks := make([]Task, 0, len(data.Todos))
	for _, raw := range data.Todos {
		if len(tasks) >= maxTaskCount {
			break
		}
		content := singleLine(raw.Content, maxTaskRunes)
		if content == "" || !validTaskStatus(raw.Status) {
			continue
		}
		tasks = append(tasks, Task{Content: content, Status: raw.Status})
	}
	if len(tasks) == 0 {
		tasks = nil
	}
	if slices.Equal(tasks, t.Tasks) {
		return false
	}
	t.Tasks = tasks
	return true
}

func validTaskStatus(status string) bool {
	switch status {
	case "pending", "in_progress", "completed":
		return true
	default:
		return false
	}
}

// applyPlanMode records the latest whole-value plan-mode state.
func (t *Transcript) applyPlanMode(event Event) bool {
	var data struct {
		Active bool `json:"active"`
	}
	if json.Unmarshal(event.Data, &data) != nil {
		return false
	}
	if data.Active == t.PlanActive {
		return false
	}
	t.PlanActive = data.Active
	return true
}

// applyGoalChange records the latest durable goal snapshot or clear tombstone.
func (t *Transcript) applyGoalChange(event Event) bool {
	var data struct {
		Operation string `json:"operation"`
		Goal      *struct {
			Objective     string `json:"objective"`
			Phase         string `json:"phase"`
			MaxGoalRounds int    `json:"maxGoalRounds"`
		} `json:"goal"`
		RoundsStarted int `json:"roundsStarted"`
	}
	if json.Unmarshal(event.Data, &data) != nil {
		return false
	}
	if data.Operation == "clear" {
		if t.Goal == nil {
			return false
		}
		t.Goal = nil
		return true
	}
	if data.Goal == nil || data.Operation == "" {
		return false
	}
	objective := singleLine(data.Goal.Objective, maxGoalRunes)
	if objective == "" || !validGoalPhase(data.Goal.Phase) {
		return false
	}
	goal := &Goal{
		Objective:     objective,
		Phase:         data.Goal.Phase,
		MaxRounds:     boundedRounds(data.Goal.MaxGoalRounds),
		RoundsStarted: boundedRounds(data.RoundsStarted),
	}
	if t.Goal != nil && *t.Goal == *goal {
		return false
	}
	t.Goal = goal
	return true
}

func validGoalPhase(phase string) bool {
	switch phase {
	case "active", "paused", "blocked", "complete":
		return true
	default:
		return false
	}
}

func boundedRounds(value int) int {
	if value < 0 || value > maxGoalRounds {
		return 0
	}
	return value
}

// singleLine flattens a user/model supplied string for a one-line label,
// dropping control characters and bounding its length.
func singleLine(text string, limit int) string {
	var builder strings.Builder
	for _, character := range text {
		switch character {
		case '\r', '\n', '\t':
			builder.WriteRune(' ')
		default:
			if unicode.IsControl(character) {
				continue
			}
			builder.WriteRune(character)
		}
	}
	return clip(builder.String(), limit)
}

// tokenCount rejects negative or absurd counters so one malformed event cannot
// overflow the banner or tread on the JSON safe-integer bound.
func tokenCount(value int) int {
	if value < 0 || value > maxTokenCount {
		return 0
	}
	return value
}

func clampTokens(input, output int) int {
	if input > maxTokenCount-output {
		return maxTokenCount
	}
	return input + output
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
	turn, step := t.position(eventPosition(event.Data))
	item := Item{
		Seq:   event.Seq,
		Turn:  turn,
		Step:  step,
		Time:  event.Time,
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
		t.items[index].Turn, t.items[index].Step = turn, step
		if t.items[index].Time == 0 {
			t.items[index].Time = event.Time
		}
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
	turn, step := t.position(eventPosition(event.Data))
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
		item.Turn, item.Step = turn, step
		item.EndTime = event.Time
		if item.Time == 0 {
			item.Time = event.Time
		}
		return true
	}
	id := "tool:" + callID
	if callID == "" {
		id = eventID(event)
	}
	t.add(Item{
		Seq:     event.Seq,
		Turn:    turn,
		Step:    step,
		Time:    event.Time,
		EndTime: event.Time,
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
	maxMessageRunes    = 64 * 1024
	maxStreamTextRunes = 8000
	maxReasoningRunes  = 8000
	// One event may alternate reasoning and text, but the row budget stays
	// small enough for hostile logs.
	maxAssistantParts = 8
	maxToolRunes      = 4000
	// Uploaded image references are display metadata only; bound everything a
	// hostile event could inflate.
	maxAttachmentsPerMessage = 8
	maxAttachmentNameRunes   = 128
	maxAttachmentBytes       = 200 * 1024 * 1024
	maxAttachmentDimension   = 8192
	// Token counters beyond this bound are treated as absent.
	maxTokenCount = 1 << 40
	// Deep-info snapshots are display-only; keep a hostile event bounded.
	maxTaskCount  = 64
	maxTaskRunes  = 200
	maxGoalRunes  = 400
	maxGoalRounds = 1_000_000
)
