package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/view"
)

type sessionClient struct {
	fakeClient
	request func(context.Context, string, any) (json.RawMessage, error)
}

func sessionTestApp(client rpcClient) (*App, chan func()) {
	updates := make(chan func(), 64)
	application := New(Options{CWD: "C:/work", Provider: "test", Model: "echo", Post: func(fn func()) { updates <- fn }})
	application.client = client
	application.sessionID = "original"
	application.model.SessionID = "original"
	application.model.CanHistory = true
	application.model.Phase = view.PhaseIdle
	return application, updates
}

func completeSessionJob(t *testing.T, application *App, updates <-chan func()) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for application.model.SessionBusy || application.model.History.Busy {
		select {
		case update := <-updates:
			update()
		case <-deadline:
			t.Fatal("session job did not finish")
		}
	}
}

func TestSessionSwitchPreservesDraftsAndRejectsDuplicateActions(t *testing.T) {
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method == "session/resume" {
			id := params.(map[string]any)["sessionId"].(string)
			return json.RawMessage(`{"session":{"id":"` + id + `","title":"","cwd":"C:/work","provider":"test","model":"echo","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"nextSeq":1}}`), nil
		}
		return json.RawMessage(`{"events":[{"seq":0,"time":1,"type":"assistant/message","data":{"message":{"content":[{"type":"text","text":"saved answer"}]}}}],"nextSeq":1,"hasMore":false}`), nil
	}}
	application, updates := sessionTestApp(client)
	application.model.Draft = "original draft"
	application.model.SessionsOpen = true
	application.model.ToolCards = map[string]*view.ToolCardState{"tool": {Expanded: true}}
	application.model.CopiedMessageID = "old"
	application.SwitchSession("saved")
	application.Send("must not send")
	application.NewSession()
	application.CloseSessions()
	if !application.model.SessionSwitching || !application.model.SessionsOpen || application.model.Draft != "original draft" || application.sessionID != "original" {
		t.Fatal("switching changed the old session before history finished")
	}
	completeSessionJob(t, application, updates)
	if application.sessionID != "saved" || application.model.Draft != "" || len(application.model.Items) != 1 || application.model.Items[0].Text != "saved answer" || application.model.SessionsOpen || application.model.ToolCards != nil || application.model.CopiedMessageID != "" {
		t.Fatal("session was not replaced atomically or stale widget state survived")
	}
	application.model.Draft = "saved draft"
	application.SwitchSession("original")
	completeSessionJob(t, application, updates)
	if application.model.Draft != "original draft" || application.nextSeq != 1 {
		t.Fatal("original draft or history boundary was not restored")
	}
	application.SwitchSession("saved")
	completeSessionJob(t, application, updates)
	if application.model.Draft != "saved draft" {
		t.Fatal("second session draft was not restored")
	}
}

func TestSessionLoadFailureKeepsOldTranscriptAndDoesNotExposeErrors(t *testing.T) {
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) { return nil, errors.New("private-token") }}
	application, updates := sessionTestApp(client)
	application.model.Items = []projection.Item{{ID: "old", Text: "old answer"}}
	application.model.Draft = "draft"
	application.model.SessionsOpen = true
	application.SwitchSession("saved")
	completeSessionJob(t, application, updates)
	if application.sessionID != "original" || application.model.Draft != "draft" || application.model.Items[0].Text != "old answer" || !application.model.SessionsOpen || application.model.SessionsError == "" || strings.Contains(application.model.SessionsError, "private-token") {
		t.Fatal("failed load lost the current session or leaked runtime details")
	}
}

func TestClosedSessionListDiscardsLateResults(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) {
		close(started)
		<-finish
		return json.RawMessage(`{"sessions":[` + storedSummary + `]}`), nil
	}}
	application, updates := sessionTestApp(client)
	application.OpenSessions()
	<-started
	application.CloseSessions()
	close(finish)
	select {
	case update := <-updates:
		update()
	case <-time.After(3 * time.Second):
		t.Fatal("list did not return")
	}
	if application.model.SessionsOpen || len(application.model.Sessions) != 0 || application.model.SessionBusy {
		t.Fatal("closed session list accepted a stale result")
	}
}

func TestSessionNotificationsMergeAfterHistoryAndIgnoreOldSession(t *testing.T) {
	application, _ := sessionTestApp(&fakeClient{})
	application.model.SessionSwitching = true
	application.loadingSession = "saved"
	application.handleNotification("session/event", json.RawMessage(`{"sessionId":"saved","seq":1,"event":{"seq":1,"time":1,"type":"assistant/message","data":{"message":{"content":[{"type":"text","text":"live"}]}}}}`))
	if len(application.sessionBuffer) != 1 || len(application.model.Items) != 0 {
		t.Fatal("target events were not isolated while loading")
	}
	buffer := application.sessionBuffer
	application.cancelSessionOperation()
	application.sessionID = "saved"
	application.nextSeq = 1
	application.handleNotification(buffer[0].method, buffer[0].params)
	application.handleNotification(buffer[0].method, buffer[0].params)
	application.handleNotification("session/event", json.RawMessage(`{"sessionId":"original","seq":0,"event":{"seq":0,"type":"assistant/message","data":{}}}`))
	if application.nextSeq != 2 || len(application.model.Items) != 1 || application.model.Items[0].Text != "live" {
		t.Fatal("history/live merge duplicated rows or accepted an old session")
	}
}

func TestSessionsUnavailableWithoutNegotiationOrDuringInteraction(t *testing.T) {
	application, _ := sessionTestApp(&fakeClient{})
	application.model.CanHistory = false
	application.OpenSessions()
	application.SwitchSession("saved")
	application.NewSession()
	if application.model.SessionBusy || application.model.SessionsOpen {
		t.Fatal("unnegotiated navigation started")
	}
	application.model.CanHistory = true
	application.model.Phase = view.PhaseRunning
	application.OpenSessions()
	application.SwitchSession("saved")
	if application.model.SessionBusy || application.model.SessionsOpen {
		t.Fatal("navigation started during a turn")
	}
}

func TestSessionSwitchMergesBufferedNotificationsAtTheCapturedBoundary(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	client := &sessionClient{request: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
		if method == "session/resume" {
			return json.RawMessage(`{"session":{"id":"saved","title":"","cwd":"C:/saved","provider":"test","model":"echo","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"nextSeq":1}}`), nil
		}
		close(started)
		<-finish
		return json.RawMessage(`{"events":[{"seq":0,"time":1,"type":"assistant/message","data":{"message":{"content":[{"type":"text","text":"history"}]}}}],"nextSeq":1,"hasMore":false}`), nil
	}}
	application, updates := sessionTestApp(client)
	application.SwitchSession("saved")
	<-started
	application.handleNotification("session/event", json.RawMessage(`{"sessionId":"saved","seq":0,"event":{"seq":0,"time":1,"type":"assistant/message","data":{"message":{"content":[{"type":"text","text":"history"}]}}}}`))
	application.handleNotification("session/event", json.RawMessage(`{"sessionId":"saved","seq":1,"event":{"seq":1,"time":2,"type":"assistant/message","data":{"message":{"content":[{"type":"text","text":"after boundary"}]}}}}`))
	close(finish)
	completeSessionJob(t, application, updates)
	if len(application.model.Items) != 2 || application.model.Items[0].Text != "history" || application.model.Items[1].Text != "after boundary" || application.nextSeq != 2 || application.model.SessionCWD != "C:/saved" {
		t.Fatal("buffered events did not merge exactly once at the history boundary")
	}
}

func TestHistoryFailureAfterOnePageNeverPublishesPartialTranscript(t *testing.T) {
	page := 0
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) {
		page++
		if page == 1 {
			return json.RawMessage(`{"events":[{"seq":0,"time":1,"type":"assistant/message","data":{"message":{"content":[{"type":"text","text":"partial"}]}}}],"nextSeq":2,"hasMore":true,"nextCursor":"next"}`), nil
		}
		return nil, errors.New("private-token")
	}}
	transcript, _, err := readSessionHistory(context.Background(), client, "saved", 0)
	if err == nil || len(transcript.Items()) != 0 || strings.Contains(err.Error(), "private-token") {
		t.Fatal("a partial history escaped a failed page read")
	}
}

func TestHistoryBoundaryAndCursorCannotChangeBetweenPages(t *testing.T) {
	for _, second := range []string{
		`{"events":[{"seq":1,"time":1,"type":"unknown","data":{}}],"nextSeq":3,"hasMore":true,"nextCursor":"second"}`,
		`{"events":[{"seq":1,"time":1,"type":"unknown","data":{}}],"nextSeq":3,"hasMore":true,"nextCursor":"repeat"}`,
	} {
		page := 0
		client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) {
			page++
			if page == 1 {
				boundary := "2"
				if strings.Contains(second, `"repeat"`) {
					boundary = "3"
				}
				return json.RawMessage(`{"events":[{"seq":0,"time":1,"type":"unknown","data":{}}],"nextSeq":` + boundary + `,"hasMore":true,"nextCursor":"repeat"}`), nil
			}
			return json.RawMessage(second), nil
		}}
		if _, _, err := readSessionHistory(context.Background(), client, "saved", 0); err == nil {
			t.Fatal("history accepted a changed boundary or a cursor cycle")
		}
	}
}

func TestFailedHostDiscardsAnInflightSessionResult(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) {
		close(started)
		<-finish
		return json.RawMessage(`{"sessions":[` + storedSummary + `]}`), nil
	}}
	application, updates := sessionTestApp(client)
	application.OpenSessions()
	<-started
	application.setFailure("Host failed", errors.New("Connection closed"))
	close(finish)
	select {
	case update := <-updates:
		update()
	case <-time.After(3 * time.Second):
		t.Fatal("session list did not settle")
	}
	if application.model.Phase != view.PhaseFailed || application.model.SessionBusy || len(application.model.Sessions) != 0 {
		t.Fatal("a late list result revived a failed host")
	}
}

func (client *sessionClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return client.request(ctx, method, params)
}

const storedSummary = `{"id":"saved","title":"","cwd":"C:/work","provider":"","model":"","effort":"","createdAt":1,"updatedAt":1,"status":"stored","archived":false}`

func TestReadSessionListPagesAndValidatesSummaries(t *testing.T) {
	page := 0
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method != "session/list" {
			t.Fatalf("method = %s", method)
		}
		page++
		if page == 1 {
			return json.RawMessage(`{"sessions":[` + storedSummary + `],"nextCursor":"next"}`), nil
		}
		if params.(map[string]any)["cursor"] != "next" {
			t.Fatal("cursor was not forwarded")
		}
		return json.RawMessage(`{"sessions":[]}`), nil
	}}
	sessions, err := readSessionList(context.Background(), client)
	if err != nil || len(sessions) != 1 || sessions[0].ID != "saved" || page != 2 {
		t.Fatalf("list = %+v, pages = %d, error = %v", sessions, page, err)
	}
	for _, payload := range []string{
		`{}`, `{"sessions":null}`, `{"sessions":[{"id":"../bad"}]}`,
		`{"sessions":[` + strings.Replace(storedSummary, `"createdAt":1`, `"createdAt":null`, 1) + `]}`,
		`{"sessions":[` + storedSummary + `,` + storedSummary + `]}`,
		`{"sessions":[],"nextCursor":"stuck"}`,
	} {
		client.request = func(context.Context, string, any) (json.RawMessage, error) { return json.RawMessage(payload), nil }
		if _, err := readSessionList(context.Background(), client); err == nil {
			t.Errorf("accepted invalid list: %s", payload)
		}
	}
}

func TestReadSessionHistoryPagesBeforePublishing(t *testing.T) {
	page := 0
	client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method != "session/history" || params.(map[string]any)["sessionId"] != "saved" {
			t.Fatal("history identity was not forwarded")
		}
		page++
		if page == 1 {
			return json.RawMessage(`{"events":[{"seq":0,"time":1,"type":"user/message","data":{"source":{"kind":"user"},"content":[{"type":"text","text":"hello"}]}}],"nextSeq":2,"hasMore":true,"nextCursor":"next"}`), nil
		}
		if params.(map[string]any)["cursor"] != "next" {
			t.Fatal("history cursor was not forwarded")
		}
		return json.RawMessage(`{"events":[{"seq":1,"time":2,"type":"assistant/message","data":{"message":{"content":[{"type":"text","text":"pong"}]}}}],"nextSeq":2,"hasMore":false}`), nil
	}}
	transcript, nextSeq, err := readSessionHistory(context.Background(), client, "saved", 2)
	if err != nil || nextSeq != 2 || len(transcript.Items()) != 2 || transcript.Items()[1].Kind != projection.KindAssistant || transcript.Items()[1].Text != "pong" {
		t.Fatalf("history = %+v, nextSeq = %d, error = %v", transcript.Items(), nextSeq, err)
	}
}

func TestReadSessionHistoryRefusesMalformedAndUnboundedPages(t *testing.T) {
	for _, payload := range []string{
		`{}`, `{"events":null,"nextSeq":0,"hasMore":false}`,
		`{"events":[],"nextSeq":2,"hasMore":true,"nextCursor":"stuck"}`,
		`{"events":[],"nextSeq":1,"hasMore":false}`,
		`{"events":[],"nextSeq":9007199254740992,"hasMore":false}`,
		`{"events":[{"seq":1,"type":"unknown","time":1,"data":{}}],"nextSeq":1,"hasMore":false}`,
		`{"events":[{"type":"unknown","time":1,"data":{}}],"nextSeq":1,"hasMore":false}`,
		`{"events":[{"seq":0,"type":"unknown","data":{}}],"nextSeq":1,"hasMore":false}`,
		`{"events":[{"seq":0,"time":1,"type":"unknown","data":null}],"nextSeq":1,"hasMore":false}`,
		`{"events":[],"nextSeq":0,"hasMore":false,"nextCursor":"extra"}`,
	} {
		client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) { return json.RawMessage(payload), nil }}
		if _, _, err := readSessionHistory(context.Background(), client, "saved", 0); err == nil {
			t.Errorf("accepted invalid history: %s", payload)
		}
	}
	client := &sessionClient{request: func(context.Context, string, any) (json.RawMessage, error) { return nil, errors.New("private-token") }}
	if _, _, err := readSessionHistory(context.Background(), client, "saved", 0); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("history failure was not safely reported: %v", err)
	}
}
