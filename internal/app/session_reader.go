package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/view"
)

const maxSessionEvents = 20000
const maxSessionHistoryBytes = 32 * 1024 * 1024
const maxSafeInteger = 1<<53 - 1

type sessionSummary struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	CWD       string  `json:"cwd"`
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
	Effort    string  `json:"effort"`
	CreatedAt int64   `json:"createdAt"`
	UpdatedAt int64   `json:"updatedAt"`
	Status    string  `json:"status"`
	Archived  bool    `json:"archived"`
	NextSeq   *uint64 `json:"nextSeq"`
}

func decodeSessionSummary(raw json.RawMessage) (sessionSummary, error) {
	var summary sessionSummary
	var fields map[string]json.RawMessage
	invalid := errors.New("Invalid session summary")
	if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(raw, &summary) != nil {
		return summary, invalid
	}
	for _, key := range []string{"id", "title", "cwd", "provider", "model", "effort", "createdAt", "updatedAt", "status", "archived"} {
		if len(fields[key]) == 0 || bytes.Equal(fields[key], []byte("null")) {
			return summary, invalid
		}
	}
	if !validSessionID(summary.ID) || summary.CreatedAt < 0 || summary.CreatedAt > maxSafeInteger || summary.UpdatedAt < 0 || summary.UpdatedAt > maxSafeInteger {
		return summary, invalid
	}
	for _, field := range []struct {
		value string
		limit int
	}{{summary.Title, 1024}, {summary.CWD, 32768}, {summary.Provider, 256}, {summary.Model, 256}, {summary.Effort, 256}} {
		if len(field.value) > field.limit || strings.ContainsRune(field.value, '\x00') {
			return summary, invalid
		}
	}
	if summary.Status != "stored" && summary.Status != "idle" && summary.Status != "running" || summary.NextSeq != nil && *summary.NextSeq > maxSafeInteger {
		return summary, invalid
	}
	return summary, nil
}

func pageCursor(raw json.RawMessage) (string, error) {
	if raw == nil {
		return "", nil
	}
	var cursor string
	if json.Unmarshal(raw, &cursor) != nil || len(cursor) == 0 || len(cursor) > 128 || strings.ContainsRune(cursor, '\x00') {
		return "", errors.New("Invalid session page cursor")
	}
	return cursor, nil
}

func readSessionList(ctx context.Context, client rpcClient) ([]view.SessionSummary, error) {
	var sessions []view.SessionSummary
	seen := make(map[string]bool)
	cursors := make(map[string]bool)
	cursor := ""
	totalBytes := 0
	for pageCount := 0; pageCount < 1001; pageCount++ {
		params := map[string]any{"limit": 64}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := client.Call(ctx, "session/list", params)
		if err != nil {
			return nil, errors.New("Could not load sessions. Refresh to retry")
		}
		totalBytes += len(raw)
		var page struct {
			Sessions   []json.RawMessage `json:"sessions"`
			NextCursor json.RawMessage   `json:"nextCursor"`
		}
		if totalBytes > 1024*1024 || json.Unmarshal(raw, &page) != nil || page.Sessions == nil || len(page.Sessions) > 64 || len(sessions)+len(page.Sessions) > 1000 {
			return nil, errors.New("Invalid or oversized session list")
		}
		for _, entry := range page.Sessions {
			summary, err := decodeSessionSummary(entry)
			if err != nil || seen[summary.ID] {
				return nil, errors.New("Invalid or duplicate session summary")
			}
			seen[summary.ID] = true
			sessions = append(sessions, view.SessionSummary{ID: summary.ID, Title: summary.Title, CWD: summary.CWD, CreatedAt: summary.CreatedAt, Status: summary.Status})
		}
		cursor, err = pageCursor(page.NextCursor)
		if err != nil || cursor != "" && (cursors[cursor] || len(page.Sessions) == 0) {
			return nil, errors.New("Invalid session list pagination")
		}
		if cursor == "" {
			return sessions, nil
		}
		cursors[cursor] = true
	}
	return nil, errors.New("Session list exceeds the page budget")
}

func readSessionHistory(ctx context.Context, client rpcClient, id string, minimumSeq uint64) (projection.Transcript, uint64, error) {
	var transcript projection.Transcript
	if !validSessionID(id) || minimumSeq > maxSessionEvents {
		return transcript, 0, errors.New("Session history exceeds the supported event budget")
	}
	cursor := ""
	cursors := make(map[string]bool)
	var offset, boundary uint64
	totalBytes := 0
	for pageCount := 0; pageCount < 1024; pageCount++ {
		params := map[string]any{"sessionId": id, "limit": 64, "direction": "forward"}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := client.Call(ctx, "session/history", params)
		if err != nil {
			return projection.Transcript{}, 0, errors.New("Could not load history. Wait for the session to be idle and retry")
		}
		totalBytes += len(raw)
		var page struct {
			Events     []json.RawMessage `json:"events"`
			NextSeq    *uint64           `json:"nextSeq"`
			HasMore    *bool             `json:"hasMore"`
			NextCursor json.RawMessage   `json:"nextCursor"`
		}
		if totalBytes > maxSessionHistoryBytes || json.Unmarshal(raw, &page) != nil || page.Events == nil || len(page.Events) > 64 || page.NextSeq == nil || *page.NextSeq > maxSessionEvents || page.HasMore == nil {
			return projection.Transcript{}, 0, errors.New("Invalid history page or history exceeds the supported budget")
		}
		if pageCount == 0 {
			boundary = *page.NextSeq
		}
		if *page.NextSeq != boundary || boundary < minimumSeq || offset+uint64(len(page.Events)) > boundary {
			return projection.Transcript{}, 0, errors.New("History sequence boundary changed")
		}
		for _, rawEvent := range page.Events {
			var event projection.Event
			var required struct {
				Seq  *uint64 `json:"seq"`
				Time *int64  `json:"time"`
			}
			if json.Unmarshal(rawEvent, &event) != nil || json.Unmarshal(rawEvent, &required) != nil || required.Seq == nil || required.Time == nil || *required.Time < 0 || *required.Time > maxSafeInteger || event.Seq != offset || len(event.Type) == 0 || len(event.Type) > 256 || strings.ContainsRune(event.Type, '\x00') || len(event.Data) == 0 || event.Data[0] != '{' {
				return projection.Transcript{}, 0, errors.New("Invalid or noncontiguous history event")
			}
			transcript.Apply(event)
			offset++
		}
		cursor, err = pageCursor(page.NextCursor)
		if err != nil || *page.HasMore != (offset < boundary) || *page.HasMore != (cursor != "") || cursor != "" && (cursors[cursor] || len(page.Events) == 0) {
			return projection.Transcript{}, 0, errors.New("Invalid history pagination")
		}
		if !*page.HasMore {
			return transcript, boundary, nil
		}
		cursors[cursor] = true
	}
	return projection.Transcript{}, 0, errors.New("History exceeds the page budget")
}
