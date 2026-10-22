package projection

import (
	"encoding/json"
	"errors"
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
	kind  string
	text  strings.Builder
	runes int
	ended bool
}

type AssistantStream struct {
	attemptID string
	revision  uint64
	index     uint64
	turn      uint64
	step      uint64
	blocks    map[uint64]*streamBlock
	order     []uint64
	runes     int
	truncated bool
	terminal  bool
	commitSeq *uint64
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
		if chunk.Type == "block-start" && chunk.BlockType == "" || (chunk.Type == "text-delta" || chunk.Type == "reasoning-delta") && chunk.Text == nil || chunk.Type == "block-end" && (chunk.Block.Type == "" || chunk.Block.Type == "text" && chunk.Block.Text == nil) {
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
	stream.applyChunk(chunk)
	return nil
}

func (stream *AssistantStream) applyChunk(chunk streamChunk) {
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
		stream.blocks[index] = &streamBlock{kind: chunk.BlockType}
		stream.order = append(stream.order, index)
	case "text-delta":
		if block == nil || block.kind != "text" || block.ended {
			stream.Clear()
			return
		}
		stream.appendText(block, *chunk.Text)
	case "block-end":
		if block == nil || block.ended || block.kind != chunk.Block.Type {
			stream.Clear()
			return
		}
		if block.kind == "text" {
			stream.runes -= block.runes
			block.text.Reset()
			block.runes = 0
			stream.appendText(block, *chunk.Block.Text)
		}
		block.ended = true
	}
}

func (stream *AssistantStream) appendText(block *streamBlock, text string) {
	remaining := maxMessageRunes - stream.runes
	for _, character := range text {
		if remaining == 0 {
			stream.truncated = true
			return
		}
		block.text.WriteRune(character)
		block.runes++
		stream.runes++
		remaining--
	}
}

func (stream *AssistantStream) Item() (Item, bool) {
	if stream.attemptID == "" {
		return Item{}, false
	}
	parts := make([]string, 0, len(stream.order))
	for _, index := range stream.order {
		block := stream.blocks[index]
		if block.kind == "text" && strings.TrimSpace(block.text.String()) != "" {
			parts = append(parts, block.text.String())
		}
	}
	text, clipped := clipMarkdown(strings.Join(parts, "\n\n"), maxMessageRunes)
	if text == "" {
		return Item{}, false
	}
	if stream.truncated && !clipped {
		text += "…"
	}
	return Item{ID: "stream:" + stream.attemptID, Kind: KindAssistant, Title: "Assistant", Text: text, Detail: "Streaming…", State: "streaming", Truncated: stream.truncated || clipped}, true
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
	stream.attemptID = ""
	stream.index = 0
	stream.blocks = nil
	stream.order = nil
	stream.runes = 0
	stream.truncated = false
	stream.terminal = false
	stream.commitSeq = nil
}
