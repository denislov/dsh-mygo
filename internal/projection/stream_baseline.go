package projection

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// Restore publishes a validated, bounded process-local snapshot atomically.
// It replaces the revision watermark even when the attempt has already ended.
func (stream *AssistantStream) Restore(raw json.RawMessage) error {
	invalid := errors.New("invalid assistant stream baseline")
	var baseline struct {
		Revision *uint64         `json:"revision"`
		Active   json.RawMessage `json:"activeAttempt"`
	}
	if len(raw) > 128*1024 || json.Unmarshal(raw, &baseline) != nil || !validStreamInteger(baseline.Revision) {
		return invalid
	}
	next := AssistantStream{revision: *baseline.Revision}
	if baseline.Active == nil {
		*stream = next
		return nil
	}
	var active struct {
		AttemptID string  `json:"attemptId"`
		Turn      *uint64 `json:"turn"`
		Step      *uint64 `json:"step"`
		NextIndex *uint64 `json:"nextIndex"`
		Blocks    []struct {
			Index *uint64 `json:"index"`
			Type  string  `json:"type"`
			Text  *string `json:"text"`
			Ended *bool   `json:"ended"`
		} `json:"blocks"`
		Truncated *bool `json:"truncated"`
	}
	if bytes.Equal(baseline.Active, []byte("null")) || json.Unmarshal(baseline.Active, &active) != nil || active.AttemptID == "" || len(active.AttemptID) > 256 || strings.ContainsRune(active.AttemptID, '\x00') || !validStreamInteger(active.Turn) || !validStreamInteger(active.Step) || !validStreamInteger(active.NextIndex) || *active.NextIndex >= next.revision || active.Truncated == nil || active.Blocks == nil || len(active.Blocks) > 128 || uint64(len(active.Blocks)) > *active.NextIndex {
		return invalid
	}
	next.attemptID, next.turn, next.step, next.index = active.AttemptID, *active.Turn, *active.Step, *active.NextIndex
	next.blocks = make(map[uint64]*streamBlock)
	next.textTruncated = *active.Truncated
	next.reasoningTruncated = *active.Truncated
	var lastText *streamBlock
	for _, block := range active.Blocks {
		if !validStreamInteger(block.Index) || next.blocks[*block.Index] != nil || block.Type == "" || len(block.Type) > 256 || strings.ContainsRune(block.Type, '\x00') || block.Ended == nil || block.Type == "text" && block.Text == nil || block.Text != nil && block.Type != "text" && block.Type != "reasoning" {
			return invalid
		}
		value := &streamBlock{kind: block.Type, ended: *block.Ended}
		if block.Text != nil {
			count := utf8.RuneCountInString(*block.Text)
			limit, used := maxMessageRunes, next.textRunes
			if block.Type == "reasoning" {
				limit, used = maxReasoningRunes, next.reasoningRunes
			}
			if used+count > limit {
				return invalid
			}
			next.appendText(value, *block.Text)
			lastText = value
		}
		next.blocks[*block.Index] = value
		next.order = append(next.order, *block.Index)
	}
	if *active.Truncated && lastText != nil {
		lastText.truncated = true
	}
	*stream = next
	return nil
}

func (stream *AssistantStream) NeedsResync() bool { return stream.desynced }

func (stream *AssistantStream) Revision() uint64 { return stream.revision }
