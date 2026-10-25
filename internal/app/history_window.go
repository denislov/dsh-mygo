package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"dsh-mygo/internal/projection"
)

// Each request has its own bounded read budget, independent of total log size.
// beforeSeq is a durable append-only offset, so reading older history survives
// expired cursors, newer turns and releasing/reacquiring an Agent.
type historyWindowPage struct {
	snapshot historySnapshot
	events   []projection.Event
	cursor   string
	bytes    int
}

func readHistoryWindow(ctx context.Context, client rpcClient, id string, minimum uint64, before *uint64, live, wantStream bool) (historySnapshot, error) {
	page, err := readHistoryWindowPage(ctx, client, id, minimum, before, live, wantStream, "")
	for _, event := range page.events {
		page.snapshot.Transcript.Apply(event)
	}
	return page.snapshot, err
}

func readHistoryWindowPage(ctx context.Context, client rpcClient, id string, minimum uint64, before *uint64, live, wantStream bool, continuation string) (historyWindowPage, error) {
	invalid := errors.New("Invalid history window")
	params := map[string]any{"sessionId": id, "direction": "backward", "limit": 64}
	if !validSessionID(id) || minimum > maxSafeInteger {
		return historyWindowPage{}, invalid
	}
	if continuation != "" {
		params["cursor"] = continuation
	} else if before != nil {
		params["beforeSeq"] = *before
	}
	if live {
		params["live"] = true
	}
	if wantStream {
		params["assistantStream"] = true
	}
	raw, err := client.Call(ctx, "session/history", params)
	if err != nil {
		return historyWindowPage{}, errors.New("Could not load history. Retry loading earlier messages")
	}
	var page struct {
		Events   []json.RawMessage `json:"events"`
		Start    *uint64           `json:"startSeq"`
		Next     *uint64           `json:"nextSeq"`
		More     *bool             `json:"hasMore"`
		Cursor   json.RawMessage   `json:"nextCursor"`
		Status   string            `json:"status"`
		Revision *uint64           `json:"statusRevision"`
		Stream   json.RawMessage   `json:"assistantStream"`
	}
	if len(raw) > maxSessionHistoryBytes || json.Unmarshal(raw, &page) != nil || page.Events == nil || len(page.Events) > 64 || page.Start == nil || page.Next == nil || page.More == nil || *page.Next > maxSafeInteger || *page.Next < minimum {
		return historyWindowPage{}, invalid
	}
	end := *page.Next
	if before != nil {
		end = *before
	}
	if end > *page.Next || *page.Start > end || end-*page.Start != uint64(len(page.Events)) || *page.More != (*page.Start > 0) || end > 0 && len(page.Events) == 0 {
		return historyWindowPage{}, invalid
	}
	cursor, err := pageCursor(page.Cursor)
	if err != nil || (cursor != "") != *page.More {
		return historyWindowPage{}, invalid
	}
	result := historySnapshot{NextSeq: *page.Next, StartSeq: *page.Start, Status: page.Status, Baseline: page.Stream}
	if live {
		if page.Status != "idle" && page.Status != "running" || page.Revision == nil || *page.Revision > maxSafeInteger {
			return historyWindowPage{}, invalid
		}
		result.StatusRevision = *page.Revision
	}
	if wantStream {
		if err := result.Stream.Restore(page.Stream); err != nil {
			return historyWindowPage{}, err
		}
	}
	events := make([]projection.Event, 0, len(page.Events))
	for index, rawEvent := range page.Events {
		var event projection.Event
		var required struct {
			Seq  *uint64 `json:"seq"`
			Time *int64  `json:"time"`
		}
		if json.Unmarshal(rawEvent, &event) != nil || json.Unmarshal(rawEvent, &required) != nil || required.Seq == nil || required.Time == nil || *required.Time < 0 || *required.Time > maxSafeInteger || event.Seq != *page.Start+uint64(index) || len(event.Type) == 0 || len(event.Type) > 256 || strings.ContainsRune(event.Type, '\x00') || len(event.Data) == 0 || event.Data[0] != '{' {
			return historyWindowPage{}, invalid
		}
		events = append(events, event)
	}
	return historyWindowPage{snapshot: result, events: events, cursor: cursor, bytes: len(raw)}, nil
}

func readOpenHistory(ctx context.Context, client rpcClient, id string, minimum uint64, window bool) (historySnapshot, error) {
	if window {
		return readConversationHistory(ctx, client, id, minimum, nil, false, false)
	}
	return readHistorySnapshot(ctx, client, id, minimum, false, false)
}

func (a *App) acceptHistoryWindow(snapshot historySnapshot) {
	a.model.History.Start = snapshot.StartSeq
	a.model.History.Detached = false
	a.model.History.Error = ""
}

func (a *App) LoadEarlierHistory() { a.loadHistoryPage(false) }
func (a *App) ReturnToLatest()     { a.loadHistoryPage(true) }

func (a *App) loadHistoryPage(latest bool) {
	if !a.canHistoryWindow || !a.model.CanLoadHistory() || !latest && a.model.History.Start == 0 {
		return
	}
	ctx, operation := a.beginSessionOperation()
	// A background read must not dim the shell's navigation and composer.
	// Keep the operation token so foreground actions can supersede its result.
	a.model.SessionBusy = false
	a.model.History.Busy = true
	client, cancel, id, minimum := a.currentClient(), a.sessionCancel, a.sessionID, a.nextSeq
	start := a.model.History.Start
	go func() {
		defer cancel()
		var before *uint64
		if !latest {
			before = &start
		}
		snapshot, err := readConversationHistory(ctx, client, id, minimum, before, false, false)
		a.post(func() {
			if !a.currentSessionOperation(operation) || a.sessionID != id {
				return
			}
			a.cancelSessionOperation()
			if err != nil {
				a.model.History.Error = err.Error()
				return
			}
			// Metadata writes cannot be accepted as a fresh realtime boundary.
			if snapshot.NextSeq != a.nextSeq {
				a.model.History.Error = "History changed. Refresh the latest messages to retry"
				return
			}
			if latest {
				a.transcript = snapshot.Transcript
				a.model.ResetTranscript(a.transcript.Items())
				a.acceptHistoryWindow(snapshot)
			} else {
				dropped := a.transcript.Prepend(&snapshot.Transcript)
				a.model.History.Start = snapshot.StartSeq
				a.model.History.Detached = a.model.History.Detached || dropped
				a.model.History.Error = ""
				if dropped {
					a.model.ToolInspector = nil
					a.model.ToolCards = nil
					a.model.List.ScrollTo(0, 0)
				}
			}
			a.publishTranscript()
		})
	}()
}

// Retired handles are released before the next navigation/mutation, after the
// previous UI commit. A failed target is retired too, never the active session.
func releaseSessions(ctx context.Context, client rpcClient, ids []string) error {
	for _, id := range ids {
		raw, err := client.Call(ctx, "session/release", map[string]any{"sessionId": id})
		var ack map[string]json.RawMessage
		if err != nil || json.Unmarshal(raw, &ack) != nil || ack == nil || len(ack) != 0 {
			return errors.New("Could not release an inactive session. Retry or restart the host")
		}
	}
	return nil
}
func (a *App) retiredSessionIDs(exclude ...string) []string {
	if !a.canRelease {
		return nil
	}
	var ids []string
	for id := range a.retiredSessions {
		if id != a.sessionID && (len(exclude) == 0 || id != exclude[0]) {
			ids = append(ids, id)
		}
	}
	return ids
}
func (a *App) retireSession(id string) {
	if !a.canRelease || !validSessionID(id) || id == a.sessionID {
		return
	}
	if a.retiredSessions == nil {
		a.retiredSessions = make(map[string]bool)
	}
	a.retiredSessions[id] = true
}
