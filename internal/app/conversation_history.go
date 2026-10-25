package app

import (
	"bytes"
	"context"
	"errors"

	"dsh-mygo/internal/projection"
)

// A transport page is not a conversation boundary. Follow the frozen cursor
// until the oldest displayed turn is complete, then replay in log order once.
func readConversationHistory(ctx context.Context, client rpcClient, id string, minimum uint64, before *uint64, live, wantStream bool) (historySnapshot, error) {
	page, err := readHistoryWindowPage(ctx, client, id, minimum, before, live, wantStream, "")
	if err != nil {
		return historySnapshot{}, err
	}
	first := page.snapshot
	pages := []historyWindowPage{page}
	totalBytes, totalEvents := page.bytes, len(page.events)
	seen := make(map[string]bool)
	boundary := page.snapshot.StartSeq
	needsTurn := first.Status == "running"
	for _, e := range page.events {
		if e.Type == "turn/end" {
			needsTurn = true
		}
	}
	for {
		found := false
		for _, event := range page.events {
			if event.Type == "turn/start" {
				boundary = event.Seq
				found = true
				if len(pages) == 1 {
					break
				}
			}
		}
		if found || page.snapshot.StartSeq == 0 || !needsTurn {
			break
		}
		if page.cursor == "" || seen[page.cursor] {
			return historySnapshot{}, errors.New("Invalid history window cursor")
		}
		seen[page.cursor] = true
		end := page.snapshot.StartSeq
		next, err := readHistoryWindowPage(ctx, client, id, minimum, &end, live, wantStream, page.cursor)
		if err != nil {
			return historySnapshot{}, err
		}
		if next.snapshot.NextSeq != first.NextSeq || next.snapshot.Status != first.Status || next.snapshot.StatusRevision != first.StatusRevision || !bytes.Equal(next.snapshot.Baseline, first.Baseline) {
			return historySnapshot{}, errors.New("History changed. Refresh the latest messages to retry")
		}
		totalBytes += next.bytes
		totalEvents += len(next.events)
		if totalBytes > maxSessionHistoryBytes || totalEvents > maxSessionEvents {
			return historySnapshot{}, errors.New("Conversation turn exceeds the supported history budget")
		}
		pages = append(pages, next)
		page = next
		boundary = page.snapshot.StartSeq
	}
	first.StartSeq = boundary
	first.Transcript = projection.Transcript{}
	for i := len(pages) - 1; i >= 0; i-- {
		for _, event := range pages[i].events {
			if event.Seq >= boundary {
				first.Transcript.Apply(event)
			}
		}
	}
	return first, nil
}
