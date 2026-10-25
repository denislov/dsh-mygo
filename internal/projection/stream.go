package projection

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

const maxStreamInteger = 9007199254740991

type streamFrameData struct {
	Type      string          `json:"type"`
	AttemptID string          `json:"attemptId"`
	Revision  *uint64         `json:"revision"`
	Turn      *uint64         `json:"turn"`
	Step      *uint64         `json:"step"`
	Index     *uint64         `json:"index"`
	Time      *uint64         `json:"time"`
	Chunk     json.RawMessage `json:"chunk"`
	Outcome   struct {
		Kind      string  `json:"kind"`
		EventType string  `json:"eventType"`
		Seq       *uint64 `json:"seq"`
	} `json:"outcome"`
}

type streamChunk struct {
	Type      string  `json:"type"`
	Index     *uint64 `json:"index"`
	BlockType string  `json:"blockType"`
	Text      *string `json:"text"`
	Block     struct {
		Type string  `json:"type"`
		Text *string `json:"text"`
	} `json:"block"`
}

type streamBlock struct {
	kind      string
	text      strings.Builder
	runes     int
	ended     bool
	truncated bool
	startTime int64
	endTime   int64
}

type AssistantStream struct {
	attemptID         string
	revision          uint64
	index             uint64
	turn              uint64
	step              uint64
	blocks            map[uint64]*streamBlock
	order             []uint64
	textRunes         int
	reasoningRunes    int
	textTruncated     bool
	reasoningTruncated bool
	terminal          bool
	commitSeq         *uint64
	desynced          bool
}

func validStreamInteger(value *uint64) bool { return value != nil && *value <= maxStreamInteger }

func (stream *AssistantStream) Apply(raw json.RawMessage) error {
	var frame streamFrameData
	var chunk streamChunk
	invalid := errors.New("invalid assistant stream frame")
	if json.Unmarshal(raw, &frame) != nil || frame.AttemptID == "" || len(frame.AttemptID) > 256 || strings.ContainsRune(frame.AttemptID, '\x00') || !validStreamInteger(frame.Revision) || *frame.Revision == 0 {
		return invalid
	}
	switch frame.Type {
	case "start":
		if !validStreamInteger(frame.Turn) || !validStreamInteger(frame.Step) {
			return invalid
		}
	case "chunk":
		if !validStreamInteger(frame.Index) || !validStreamInteger(frame.Time) || json.Unmarshal(frame.Chunk, &chunk) != nil || chunk.Type == "" {
			return invalid
		}
		switch chunk.Type {
		case "block-start", "block-end", "text-delta", "reasoning-delta", "tool-call-delta":
			if !validStreamInteger(chunk.Index) {
				return invalid
			}
		}
		if chunk.Type == "block-start" && chunk.BlockType == "" || (chunk.Type == "text-delta" || chunk.Type == "reasoning-delta") && chunk.Text == nil || chunk.Type == "block-end" && (chunk.Block.Type == "" || (chunk.Block.Type == "text" || chunk.Block.Type == "reasoning") && chunk.Block.Text == nil) {
			return invalid
		}
	case "end":
		if !validStreamInteger(frame.Index) || frame.Outcome.Kind != "abandoned" && frame.Outcome.Kind != "committed" {
			return invalid
		}
		if frame.Outcome.Kind == "committed" && (!validStreamInteger(frame.Outcome.Seq) || frame.Outcome.EventType != "assistant/message" && frame.Outcome.EventType != "assistant/attempt") {
			return invalid
		}
	default:
		return invalid
	}
	if *frame.Revision <= stream.revision {
		return nil
	}
	previous := stream.revision
	stream.revision = *frame.Revision
	if frame.Type == "start" {
		stream.Clear()
		stream.attemptID = frame.AttemptID
		stream.turn, stream.step = *frame.Turn, *frame.Step
		stream.blocks = make(map[uint64]*streamBlock)
		return nil
	}
	if stream.revision != previous+1 {
		stream.Clear()
		stream.desynced = true
		return nil
	}
	if frame.AttemptID != stream.attemptID {
		return nil
	}
	if stream.terminal {
		return nil
	}
	if *frame.Index != stream.index {
		stream.Clear()
		stream.desynced = true
		return nil
	}
	if frame.Type == "end" {
		if frame.Outcome.Kind == "abandoned" {
			stream.Clear()
		} else {
			stream.terminal = true
			stream.commitSeq = frame.Outcome.Seq
		}
		return nil
	}
	stream.index++
	stream.applyChunk(chunk, int64(*frame.Time))
	return nil
}

func (stream *AssistantStream) applyChunk(chunk streamChunk, time int64) {
	if chunk.Index == nil {
		return
	}
	index := *chunk.Index
	block := stream.blocks[index]
	switch chunk.Type {
	case "block-start":
		if block != nil || len(stream.blocks) >= 128 {
			stream.Clear()
			return
		}
		stream.blocks[index] = &streamBlock{kind: chunk.BlockType, startTime: time, endTime: time}
		stream.order = append(stream.order, index)
	case "text-delta":
		if block == nil || block.kind != "text" || block.ended {
			stream.Clear()
			return
		}
		stream.appendText(block, *chunk.Text)
		block.endTime = time
	case "reasoning-delta":
		if block == nil || block.kind != "reasoning" || block.ended {
			stream.Clear()
			return
		}
		stream.appendText(block, *chunk.Text)
		block.endTime = time
	case "block-end":
		if block == nil || block.ended || block.kind != chunk.Block.Type {
			stream.Clear()
			return
		}
		if block.kind == "text" || block.kind == "reasoning" {
			stream.removeText(block)
			stream.appendText(block, *chunk.Block.Text)
		}
		block.ended = true
		block.endTime = time
	}
}

func (stream *AssistantStream) removeText(block *streamBlock) {
	if block.kind == "reasoning" {
		stream.reasoningRunes -= block.runes
	} else {
		stream.textRunes -= block.runes
	}
	block.text.Reset()
	block.runes = 0
}

func (stream *AssistantStream) appendText(block *streamBlock, text string) {
	limit, counter, truncated := maxMessageRunes, &stream.textRunes, &stream.textTruncated
	if block.kind == "reasoning" {
		limit, counter, truncated = maxReasoningRunes, &stream.reasoningRunes, &stream.reasoningTruncated
	}
	remaining := limit - *counter
	for _, character := range text {
		if remaining == 0 {
			*truncated = true
			block.truncated = true
			return
		}
		block.text.WriteRune(character)
		block.runes++
		*counter++
		remaining--
	}
}

// Items returns the visible rows of the in-flight attempt: reasoning runs in
// block order and at most one assistant text row. Tool frames never contribute.
func (stream *AssistantStream) Items() []Item {
	if stream.attemptID == "" {
		return nil
	}
	items := make([]Item, 0, 2)
	parts := make([]string, 0, len(stream.order))
	run := 0
	var textStart, textEnd int64
	for _, index := range stream.order {
		block := stream.blocks[index]
		text := block.text.String()
		if strings.TrimSpace(text) == "" {
			continue
		}
		switch block.kind {
		case "reasoning":
			items = append(items, Item{
				ID:          "stream:" + stream.attemptID + ":r:" + strconv.Itoa(run),
				Kind:        KindReasoning,
				Turn:        stream.turn,
				Step:        stream.step,
				Time:        block.startTime,
				EndTime:     block.endTime,
				ActivityKey: stream.activityKey(run),
				Title:       "Reasoning",
				Text:        text,
				State:       "streaming",
				Truncated:   block.truncated,
			})
			run++
		case "text":
			parts = append(parts, text)
			if textStart == 0 || block.startTime < textStart {
				textStart = block.startTime
			}
			if block.endTime > textEnd {
				textEnd = block.endTime
			}
		}
	}
	if len(parts) > 0 {
		text, clipped := clipMarkdown(strings.Join(parts, "\n\n"), maxMessageRunes)
		if text != "" {
			if stream.textTruncated && !clipped {
				text += "…"
			}
			items = append(items, Item{
				ID:        "stream:" + stream.attemptID,
				Kind:      KindAssistant,
				Turn:      stream.turn,
				Step:      stream.step,
				Time:      textStart,
				EndTime:   textEnd,
				Title:     "Assistant",
				Text:      text,
				Detail:    "Streaming…",
				State:     "streaming",
				Truncated: stream.textTruncated || clipped,
			})
		}
	}
	return items
}

func (stream *AssistantStream) activityKey(run int) string {
	if stream.turn != 0 && stream.step != 0 {
		return activityKey(stream.turn, stream.step, 0, uint64(run))
	}
	return "activity:stream:" + stream.attemptID + ":r:" + strconv.Itoa(run)
}

func (stream *AssistantStream) Commit(event Event) {
	if event.Type == "turn/end" {
		stream.Clear()
		return
	}
	if event.Type != "assistant/message" && event.Type != "assistant/attempt" {
		return
	}
	var position struct {
		Turn *uint64 `json:"turn"`
		Step *uint64 `json:"step"`
	}
	if stream.commitSeq != nil && *stream.commitSeq == event.Seq {
		stream.Clear()
		return
	}
	if json.Unmarshal(event.Data, &position) == nil && position.Turn != nil && position.Step != nil && *position.Turn == stream.turn && *position.Step == stream.step {
		stream.Clear()
	}
}

func (stream *AssistantStream) Clear() {
	stream.desynced = false
	stream.attemptID = ""
	stream.index = 0
	stream.blocks = nil
	stream.order = nil
	stream.textRunes = 0
	stream.reasoningRunes = 0
	stream.textTruncated = false
	stream.reasoningTruncated = false
	stream.terminal = false
	stream.commitSeq = nil
}
