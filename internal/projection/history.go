package projection

import (
	"sort"
	"strings"
)

type sequenceRange struct{ first, last uint64 }

func (t *Transcript) markSeen(first, last uint64) bool {
	i := sort.Search(len(t.seen), func(i int) bool { return t.seen[i].last >= first || t.seen[i].last+1 == first })
	if i < len(t.seen) && t.seen[i].first <= first && t.seen[i].last >= last {
		return false
	}
	end := i
	for end < len(t.seen) && (t.seen[end].first <= last || t.seen[end].first == last+1) {
		if t.seen[end].first < first {
			first = t.seen[end].first
		}
		if t.seen[end].last > last {
			last = t.seen[end].last
		}
		end++
	}
	ranges := append([]sequenceRange(nil), t.seen[:i]...)
	ranges = append(ranges, sequenceRange{first, last})
	t.seen = append(ranges, t.seen[end:]...)
	return true
}

const MaxHistoryRows = 4096
const MaxHistoryPreviewBytes = 16 * 1024 * 1024

// Prepend joins disjoint adjacent log windows without retaining raw events.
// A tool call may be in the older page and its authoritative result in the
// newer page. The returned flag means newer rows were dropped for browsing.
func (t *Transcript) Prepend(older *Transcript) bool {
	items := append([]Item(nil), older.items...)
	indices := make(map[string]int, len(items))
	for i, item := range items {
		indices[item.ID] = i
	}
	for _, item := range t.items {
		if index, exists := indices[item.ID]; exists {
			previous := items[index]
			if item.Kind == KindTool {
				if previous.Title != "Tool result" {
					item.Title, item.Text = previous.Title, previous.Text
					item.Seq = previous.Seq
				}
				if item.Tool != nil && previous.Tool != nil {
					details := *item.Tool
					details.Arguments, details.ArgumentsTruncated = previous.Tool.Arguments, previous.Tool.ArgumentsTruncated
					item.Tool = &details
				}
				items[index] = item
			}
		} else {
			indices[item.ID] = len(items)
			items = append(items, item)
		}
	}
	// Only projected previews survive. Browsing keeps older rows on overflow;
	// the UI requires returning to the latest window before another prompt.
	bytes, keep := 0, 0
	for _, item := range items {
		size := len(item.ID) + len(item.Title) + len(item.Text) + len(item.Detail)
		if keep >= MaxHistoryRows || bytes+size > MaxHistoryPreviewBytes {
			break
		}
		bytes += size
		keep++
	}
	dropped := keep < len(items)
	t.items = append([]Item(nil), items[:keep]...)
	t.toolIndex = make(map[string]int)
	t.toolDetailBytes = 0
	for i := range t.items {
		item := &t.items[i]
		if item.Kind == KindTool && strings.HasPrefix(item.ID, "tool:") {
			t.toolIndex[strings.TrimPrefix(item.ID, "tool:")] = i
		}
		if item.Tool != nil {
			t.toolDetailBytes += item.Tool.Size()
		}
	}
	for i := range t.items {
		if t.toolDetailBytes <= MaxToolCacheBytes {
			break
		}
		if t.items[i].Tool != nil {
			t.toolDetailBytes -= t.items[i].Tool.Size()
			t.items[i].Tool = nil
			t.items[i].ToolEvicted = true
		}
	}
	for _, interval := range older.seen {
		t.markSeen(interval.first, interval.last)
	}
	return dropped
}

// TrimRecent bounds a live transcript without limiting the size of its log.
// The returned boundary can be used to load any evicted history again.
func (t *Transcript) TrimRecent() (uint64, bool) {
	bytes, start := 0, len(t.items)
	for start > 0 && len(t.items)-start < MaxHistoryRows {
		item := t.items[start-1]
		size := len(item.ID) + len(item.Title) + len(item.Text) + len(item.Detail)
		if bytes+size > MaxHistoryPreviewBytes {
			break
		}
		bytes += size
		start--
	}
	if start == 0 {
		return 0, false
	}
	t.items = append([]Item(nil), t.items[start:]...)
	t.toolIndex = make(map[string]int)
	t.toolDetailBytes = 0
	for i, item := range t.items {
		if item.Kind == KindTool && strings.HasPrefix(item.ID, "tool:") {
			t.toolIndex[strings.TrimPrefix(item.ID, "tool:")] = i
		}
		if item.Tool != nil {
			t.toolDetailBytes += item.Tool.Size()
		}
	}
	return t.items[0].Seq, true
}
