package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

const maxSessionEvents = 20000
const maxSessionHistoryBytes = 32 * 1024 * 1024
const maxSafeInteger = 1<<53 - 1

func decodeSessionSummary(raw json.RawMessage) (protocol.SessionSummary, error) {
	var summary protocol.SessionSummary
	var fields map[string]json.RawMessage
	invalid := errors.New("Invalid session summary")
	if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(raw, &summary) != nil {
		return summary, invalid
	}
	for _, key := range []string{"metadataIncomplete", "metadataCached"} {
		if value, present := fields[key]; present && !bytes.Equal(value, []byte("true")) && !bytes.Equal(value, []byte("false")) {
			return summary, invalid
		}
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

func readSessionList(ctx context.Context, client rpcClient, includeArchived ...bool) ([]view.SessionSummary, error) {
	var sessions []view.SessionSummary
	seen := make(map[string]bool)
	cursors := make(map[string]bool)
	cursor := ""
	totalBytes := 0
	for pageCount := 0; pageCount < 1001; pageCount++ {
		params := protocol.SessionListParams{Limit: 64, IncludeArchived: len(includeArchived) > 0 && includeArchived[0], Cursor: cursor}
		raw, err := client.Call(ctx, protocol.MethodSessionList, params)
		if err != nil {
			return nil, errors.New("Could not load sessions. Refresh to retry")
		}
		totalBytes += len(raw)
		var page protocol.SessionListResult
		if totalBytes > 1024*1024 || json.Unmarshal(raw, &page) != nil || page.Sessions == nil || len(page.Sessions) > 64 || len(sessions)+len(page.Sessions) > 1000 {
			return nil, errors.New("Invalid or oversized session list")
		}
		for _, entry := range page.Sessions {
			summary, err := decodeSessionSummary(entry)
			if err != nil || seen[summary.ID] {
				return nil, errors.New("Invalid or duplicate session summary")
			}
			seen[summary.ID] = true
			sessions = append(sessions, sessionSummaryView(summary))
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

func sessionSummaryView(summary protocol.SessionSummary) view.SessionSummary {
	return view.SessionSummary{ID: summary.ID, Title: summary.Title, CWD: summary.CWD, CreatedAt: summary.CreatedAt, UpdatedAt: summary.UpdatedAt, Status: summary.Status, Archived: summary.Archived, MetadataIncomplete: summary.MetadataIncomplete, MetadataCached: summary.MetadataCached}
}

type historySnapshot struct {
	StartSeq       uint64
	Transcript     projection.Transcript
	NextSeq        uint64
	Status         string
	StatusRevision uint64
	Stream         projection.AssistantStream
	Baseline       json.RawMessage
}

func readSessionHistory(ctx context.Context, client rpcClient, id string, minimumSeq uint64) (projection.Transcript, uint64, error) {
	snapshot, err := readHistorySnapshot(ctx, client, id, minimumSeq, false, false)
	return snapshot.Transcript, snapshot.NextSeq, err
}

func readHistorySnapshot(ctx context.Context, client rpcClient, id string, minimumSeq uint64, live, wantStream bool) (historySnapshot, error) {
	var transcript projection.Transcript
	var stream projection.AssistantStream
	var baseline json.RawMessage
	if !validSessionID(id) || minimumSeq > maxSessionEvents {
		return historySnapshot{}, errors.New("Session history exceeds the supported event budget")
	}
	cursor := ""
	cursors := make(map[string]bool)
	var offset, boundary uint64
	var status string
	var statusRevision uint64
	totalBytes := 0
	for pageCount := 0; pageCount < 1024; pageCount++ {
		params := protocol.SessionHistoryParams{
			SessionID: id, Limit: 64, Direction: protocol.HistoryDirectionForward,
			Live: live, AssistantStream: wantStream, Cursor: cursor,
		}
		raw, err := client.Call(ctx, protocol.MethodSessionHistory, params)
		if err != nil {
			return historySnapshot{}, errors.New("Could not load history. Wait for the session to be idle and retry")
		}
		totalBytes += len(raw)
		var page protocol.SessionHistoryResult
		if totalBytes > maxSessionHistoryBytes || json.Unmarshal(raw, &page) != nil || page.Events == nil || len(page.Events) > 64 || page.NextSeq == nil || *page.NextSeq > maxSessionEvents || page.HasMore == nil {
			return historySnapshot{}, errors.New("Invalid history page or history exceeds the supported budget")
		}
		if pageCount == 0 {
			boundary = *page.NextSeq
			status = page.Status
			if page.StatusRevision != nil {
				statusRevision = *page.StatusRevision
			}
			if wantStream {
				if err := stream.Restore(page.AssistantStream); err != nil {
					return historySnapshot{}, err
				}
				baseline = page.AssistantStream
			}
		}
		if wantStream && !bytes.Equal(baseline, page.AssistantStream) {
			return historySnapshot{}, errors.New("Stream baseline changed during pagination")
		}
		if live && (page.Status != "idle" && page.Status != "running" || page.StatusRevision == nil || *page.StatusRevision > maxSafeInteger || page.Status != status || *page.StatusRevision != statusRevision) {
			return historySnapshot{}, errors.New("Invalid live history status boundary")
		}
		if *page.NextSeq != boundary || boundary < minimumSeq || offset+uint64(len(page.Events)) > boundary {
			return historySnapshot{}, errors.New("History sequence boundary changed")
		}
		for _, rawEvent := range page.Events {
			var event projection.Event
			var required struct {
				Seq  *uint64 `json:"seq"`
				Time *int64  `json:"time"`
			}
			if json.Unmarshal(rawEvent, &event) != nil || json.Unmarshal(rawEvent, &required) != nil || required.Seq == nil || required.Time == nil || *required.Time < 0 || *required.Time > maxSafeInteger || event.Seq != offset || len(event.Type) == 0 || len(event.Type) > 256 || strings.ContainsRune(event.Type, '\x00') || len(event.Data) == 0 || event.Data[0] != '{' {
				return historySnapshot{}, errors.New("Invalid or noncontiguous history event")
			}
			transcript.Apply(event)
			offset++
		}
		cursor, err = pageCursor(page.NextCursor)
		if err != nil || *page.HasMore != (offset < boundary) || *page.HasMore != (cursor != "") || cursor != "" && (cursors[cursor] || len(page.Events) == 0) {
			return historySnapshot{}, errors.New("Invalid history pagination")
		}
		if !*page.HasMore {
			return historySnapshot{Transcript: transcript, NextSeq: boundary, Status: status, StatusRevision: statusRevision, Stream: stream, Baseline: baseline}, nil
		}
		cursors[cursor] = true
	}
	return historySnapshot{}, errors.New("History exceeds the page budget")
}
