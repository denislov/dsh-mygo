package projection

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"
)

const MaxToolDetailBytes = 64 * 1024
const MaxToolCacheBytes = 4 * 1024 * 1024
const maxDiffBytes = 128 * 1024
const maxFileDiffs = 16

type FileDiff struct {
	Path           string
	OldText        string
	NewText        string
	OldTextMissing bool
}

type ToolDetails struct {
	Arguments          string
	Result             string
	ArgumentsTruncated bool
	ResultTruncated    bool
	Diffs              []FileDiff
	DiffNotice         string
	Notice             string
}

func (details *ToolDetails) Size() int {
	size := len(details.Arguments) + len(details.Result) + len(details.DiffNotice) + len(details.Notice)
	for _, diff := range details.Diffs {
		size += len(diff.Path) + len(diff.OldText) + len(diff.NewText)
	}
	return size
}

func boundedToolText(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, false
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return strings.Clone(value[:limit]), true
}

func toolResultText(blocks []contentBlock) (string, bool) {
	var result strings.Builder
	truncated := false
	for _, block := range blocks {
		text := ""
		switch block.Type {
		case "text":
			text = block.Text
		case "image":
			text = "[image]"
		case "file":
			text = "[file]"
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		if result.Len() > 0 {
			separator, cut := boundedToolText("\n\n", MaxToolDetailBytes-result.Len())
			result.WriteString(separator)
			truncated = truncated || cut
		}
		part, cut := boundedToolText(text, MaxToolDetailBytes-result.Len())
		result.WriteString(part)
		truncated = truncated || cut
		if cut {
			break
		}
	}
	return result.String(), truncated
}

func projectDiffs(raw json.RawMessage) ([]FileDiff, string) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, ""
	}
	invalid := "Diff unavailable (invalid or oversized metadata)."
	if len(raw) > 256*1024 {
		return nil, invalid
	}
	var metadata map[string]json.RawMessage
	if json.Unmarshal(raw, &metadata) != nil || metadata == nil {
		return nil, invalid
	}
	wire, present := metadata["diffs"]
	if !present {
		return nil, ""
	}
	var entries []map[string]json.RawMessage
	if json.Unmarshal(wire, &entries) != nil || bytes.Equal(bytes.TrimSpace(wire), []byte("null")) || len(entries) > maxFileDiffs {
		return nil, invalid
	}
	if len(entries) == 0 {
		var operation string
		_ = json.Unmarshal(metadata["operation"], &operation)
		if operation == "create" {
			return nil, "Created file (no textual hunks)."
		}
		return nil, "No textual diff hunks were recorded."
	}
	diffs := make([]FileDiff, 0, len(entries))
	total := 0
	for _, entry := range entries {
		var path, newText string
		var oldText *string
		if entry == nil || json.Unmarshal(entry["path"], &path) != nil || path == "" || len(path) > 4096 || strings.ContainsRune(path, '\x00') ||
			json.Unmarshal(entry["oldText"], &oldText) != nil || json.Unmarshal(entry["newText"], &newText) != nil || bytes.Equal(bytes.TrimSpace(entry["newText"]), []byte("null")) {
			return nil, invalid
		}
		diff := FileDiff{Path: path, NewText: newText, OldTextMissing: oldText == nil}
		if oldText != nil {
			diff.OldText = *oldText
		}
		if strings.ContainsRune(diff.OldText, '\x00') || strings.ContainsRune(diff.NewText, '\x00') {
			return nil, invalid
		}
		total += len(diff.Path) + len(diff.OldText) + len(diff.NewText)
		if total > maxDiffBytes {
			return nil, invalid
		}
		diffs = append(diffs, diff)
	}
	return diffs, ""
}

func (t *Transcript) setToolDetails(index int, details *ToolDetails) {
	t.items = slices.Clone(t.items)
	if previous := t.items[index].Tool; previous != nil {
		t.toolDetailBytes -= previous.Size()
	}
	t.items[index].Tool = details
	t.items[index].ToolEvicted = false
	t.toolDetailBytes += details.Size()
	if t.toolDetailBytes <= MaxToolCacheBytes {
		return
	}
	for olderIndex := range t.items {
		older := &t.items[olderIndex]
		if older.Tool == nil {
			continue
		}
		t.toolDetailBytes -= older.Tool.Size()
		older.Tool = nil
		older.ToolEvicted = true
		if t.toolDetailBytes <= MaxToolCacheBytes {
			break
		}
	}
}
