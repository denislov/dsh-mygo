package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/protocol"
)

func TestSessionArchiveFailureRequiresReadbackAndPreservesDraft(t *testing.T) {
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method == protocol.MethodSessionArchive {
			if fields := requestFields(t, params); fields["sessionId"] != "original" || fields["archived"] != true {
				t.Errorf("archive params = %v", fields)
			}
			return nil, errors.New("private-token")
		}
		if !params.(protocol.SessionListParams).IncludeArchived {
			t.Error("refresh omitted archived state")
		}
		return json.RawMessage(`{"sessions":[` + strings.Replace(storedSummary, `"id":"saved"`, `"id":"original"`, 1) + `]}`), nil
	}}
	a, updates := sessionTestApp(client)
	a.model.SessionPanel.CanArchive = true
	a.model.Draft = "keep me"
	a.SetSessionArchived("original", true)
	completeSessionJob(t, a, updates)
	if !a.model.SessionPanel.Uncertain || a.model.Draft != "keep me" || strings.Contains(a.model.SessionsError, "private-token") {
		t.Fatal("failed write lost state or did not require refresh")
	}
	a.NewSession()
	if a.model.SessionBusy {
		t.Fatal("uncertain mutation allowed navigation")
	}
	a.RefreshSessions()
	completeSessionJob(t, a, updates)
	if a.model.SessionPanel.Uncertain || a.model.Draft != "keep me" {
		t.Fatal("readback did not safely unlock")
	}
}

func TestRenameFailureReadbackRepairsSequenceAndKeepsDraft(t *testing.T) {
	client := &sessionClient{request: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
		switch method {
		case "session/rename":
			return nil, context.DeadlineExceeded
		case "session/list":
			return json.RawMessage(`{"sessions":[{"id":"original","title":"Late title","cwd":"C:/work","provider":"test","model":"echo","effort":"","createdAt":1,"updatedAt":2,"status":"idle","archived":false}]}`), nil
		case "session/history":
			return json.RawMessage(`{"events":[{"seq":0,"time":1,"type":"assistant/message","data":{"message":{"content":[{"type":"text","text":"kept answer"}]}}},{"seq":1,"time":2,"type":"session/title","data":{"title":"Late title"}}],"nextSeq":2,"hasMore":false}`), nil
		}
		t.Fatalf("unexpected method: %s", method)
		return nil, nil
	}}
	a, updates := sessionTestApp(client)
	a.nextSeq = 1
	a.model.Items = []projection.Item{{ID: "old", Text: "kept answer"}}
	a.model.Draft = "keep draft"
	a.model.SessionPanel.CanRename = true
	a.OpenSessionRename("original")
	a.model.SessionPanel.Title = "Late title"
	a.RenameSession()
	completeSessionJob(t, a, updates)
	if !a.model.SessionPanel.Uncertain {
		t.Fatal("timeout did not block input")
	}
	a.RefreshSessions()
	completeSessionJob(t, a, updates)
	if a.model.SessionPanel.Uncertain || a.nextSeq != 2 || a.model.Draft != "keep draft" || len(a.model.Items) != 1 || a.model.Items[0].Text != "kept answer" {
		t.Fatal("readback failed to reconcile durable history")
	}
}

func TestSessionRenameRejectsInvalidTitleAndMissingCommittedEvent(t *testing.T) {
	calls := 0
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		calls++
		if method == protocol.MethodSessionRename {
			if fields := requestFields(t, params); fields["sessionId"] != "original" || fields["title"] != "Renamed" {
				t.Errorf("rename params = %v", fields)
			}
		}
		if method == "session/list" {
			return json.RawMessage(`{"sessions":[{"id":"original","title":"Renamed","cwd":"C:/work","provider":"test","model":"echo","effort":"","createdAt":1,"updatedAt":2,"status":"idle","archived":false}]}`), nil
		}
		return json.RawMessage(`{"session":{"id":"original","title":"Renamed","cwd":"C:/work","provider":"test","model":"echo","effort":"","createdAt":1,"updatedAt":2,"status":"idle","archived":false,"nextSeq":1}}`), nil
	}}
	a, updates := sessionTestApp(client)
	a.model.SessionPanel.CanRename = true
	a.OpenSessionRename("original")
	a.model.SessionPanel.Title = "invalid\nname"
	a.RenameSession()
	if calls != 0 || a.model.SessionsError == "" {
		t.Fatal("invalid title reached Host")
	}
	a.model.SessionPanel.Title = "Renamed"
	a.RenameSession()
	completeSessionJob(t, a, updates)
	if !a.model.SessionPanel.Uncertain {
		t.Fatal("rename without committed title event was accepted")
	}
}
