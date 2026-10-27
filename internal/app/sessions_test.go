package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/attachment"
	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

type sessionClient struct {
	fakeClient
	request func(context.Context, string, any) (json.RawMessage, error)
}

func sessionTestApp(client rpcClient) (*App, chan func()) {
	updates := make(chan func(), 64)
	application := New(Options{CWD: "C:/work", Provider: "test", Model: "echo", Post: func(fn func()) { updates <- fn }})
	application.installHost(&hostInstance{client: client})
	application.sessionID = "original"
	application.model.SessionID = "original"
	application.model.CanHistory = true
	application.model.Phase = view.PhaseIdle
	return application, updates
}

func completeSessionJob(t *testing.T, application *App, updates <-chan func()) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for application.model.SessionBusy || application.model.History.Busy || application.model.SessionPanel.Refreshing {
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
			id := requestFields(t, params)["sessionId"].(string)
			return json.RawMessage(`{"session":{"id":"` + id + `","title":"","cwd":"C:/work","provider":"test","model":"echo","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"nextSeq":1}}`), nil
		}
		return json.RawMessage(`{"events":[{"seq":0,"time":1,"type":"assistant/message","data":{"message":{"content":[{"type":"text","text":"saved answer"}]}}}],"nextSeq":1,"hasMore":false}`), nil
	}}
	application, updates := sessionTestApp(client)
	application.acceptAttachments(testLimits())
	application.model.Attachments.Pending = []attachment.Draft{{Name: "staged.png", MediaType: attachment.MediaPNG, Data: []byte("png")}}
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
	if application.sessionID != "saved" || application.model.Draft != "" || len(application.model.Items) != 1 || application.model.Items[0].Text != "saved answer" || application.model.SessionsOpen || application.model.ToolCards != nil || application.model.CopiedMessageID != "" || len(application.model.Attachments.Pending) != 0 {
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

func TestSessionNavigationKeepsDefaultAndExplicitRoutesSeparate(t *testing.T) {
	for _, test := range []struct {
		name         string
		resume       bool
		useDefaults  bool
		canConfigure bool
		effort       string
		want         map[string]any
	}{
		{"create default", false, true, true, "", map[string]any{"cwd": "C:/work", "useDefaultModel": true}},
		{"create explicit", false, false, true, "", map[string]any{"cwd": "C:/work", "provider": "test", "model": "echo"}},
		{"create explicit effort", false, false, true, "high", map[string]any{"cwd": "C:/work", "provider": "test", "model": "echo", "effort": "high"}},
		{"resume default", true, true, true, "", map[string]any{"sessionId": "saved", "useDefaultModel": true}},
		{"resume explicit", true, false, true, "", map[string]any{"sessionId": "saved", "provider": "test", "model": "echo"}},
		{"resume explicit effort", true, false, true, "high", map[string]any{"sessionId": "saved", "provider": "test", "model": "echo", "effort": "high"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var captured any
			client := &sessionClient{request: func(_ context.Context, method string, params any) (json.RawMessage, error) {
				if method == protocol.MethodSessionCreate || method == protocol.MethodSessionResume {
					captured = params
					id := "new"
					if method == protocol.MethodSessionResume {
						id = "saved"
					}
					return json.RawMessage(`{"session":{"id":"` + id + `","title":"","cwd":"C:/work","provider":"test","model":"echo","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"nextSeq":0}}`), nil
				}
				return json.RawMessage(`{"events":[],"nextSeq":0,"hasMore":false}`), nil
			}}
			application, updates := sessionTestApp(client)
			application.options.UseHostDefaults = test.useDefaults
			application.options.ReasoningEffort = test.effort
			application.model.CanConfigure = test.canConfigure
			if test.resume {
				application.SwitchSession("saved")
			} else {
				application.NewSession()
			}
			completeSessionJob(t, application, updates)
			if captured == nil {
				t.Fatal("navigation did not send a session request")
			}
			if fields := requestFields(t, captured); !reflect.DeepEqual(fields, test.want) {
				t.Fatalf("request = %v, want %v", fields, test.want)
			}
		})
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
		request, ok := params.(protocol.SessionListParams)
		if method != "session/list" || !ok {
			t.Fatalf("request = %T, method = %s", params, method)
		}
		page++
		if page == 1 {
			if request.Cursor != "" || request.Limit != 64 || request.IncludeArchived {
				t.Fatalf("first page = %+v", request)
			}
			return json.RawMessage(`{"sessions":[` + storedSummary + `],"nextCursor":"next"}`), nil
		}
		if request.Cursor != "next" {
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
		request, ok := params.(protocol.SessionHistoryParams)
		if method != "session/history" || !ok || request.SessionID != "saved" {
			t.Fatal("history identity was not forwarded")
		}
		page++
		if page == 1 {
			if request.Direction != protocol.HistoryDirectionForward || request.Limit != 64 || request.Live || request.AssistantStream {
				t.Fatalf("first page = %+v", request)
			}
			return json.RawMessage(`{"events":[{"seq":0,"time":1,"type":"user/message","data":{"source":{"kind":"user"},"content":[{"type":"text","text":"hello"}]}}],"nextSeq":2,"hasMore":true,"nextCursor":"next"}`), nil
		}
		if request.Cursor != "next" {
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

func TestNewSessionReusesCurrentBlankSession(t *testing.T) {
	calls := 0
	client := &sessionClient{request: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
		if method == protocol.MethodSessionCreate {
			calls++
			return json.RawMessage(`{"session":{"id":"new","title":"","cwd":"C:/work","provider":"test","model":"echo","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"blank":true,"nextSeq":0}}`), nil
		}
		return json.RawMessage(`{"events":[],"nextSeq":0,"hasMore":false}`), nil
	}}
	application, updates := sessionTestApp(client)
	application.sessionBlank = true
	application.model.SessionsError = "stale error"
	application.model.Sessions = []view.SessionSummary{{ID: "original", Provider: "test", Model: "echo", Blank: true}}
	application.NewSession()
	if calls != 0 || application.model.SessionBusy || application.model.SessionsError != "" {
		t.Fatal("blank session was not reused cleanly")
	}
	application.sessionBlank = false
	application.NewSession()
	completeSessionJob(t, application, updates)
	if calls != 1 || application.sessionID != "new" || !application.sessionBlank {
		t.Fatalf("new blank session = calls %d, id %q, blank %v", calls, application.sessionID, application.sessionBlank)
	}
}

func TestNewSessionCreatesWhenBlankDirectoryOrRouteDiffers(t *testing.T) {
	for name, summary := range map[string]view.SessionSummary{
		"directory": {ID: "original", CWD: "C:/other", Provider: "test", Model: "echo", Blank: true},
		"route":     {ID: "original", CWD: "C:/work", Provider: "test", Model: "other", Blank: true},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			client := &sessionClient{request: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
				if method == protocol.MethodSessionCreate {
					calls++
					return json.RawMessage(`{"session":{"id":"new","title":"","cwd":"C:/work","provider":"test","model":"echo","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"blank":true,"nextSeq":0}}`), nil
				}
				return json.RawMessage(`{"events":[],"nextSeq":0,"hasMore":false}`), nil
			}}
			application, updates := sessionTestApp(client)
			application.sessionBlank = true
			application.model.SessionCWD = "C:/work"
			application.model.Sessions = []view.SessionSummary{summary}
			application.NewSession()
			completeSessionJob(t, application, updates)
			if calls != 1 || application.sessionID == "original" {
				t.Fatalf("blank session was reused despite a different %s", name)
			}
		})
	}
}

func TestCommittedEventClearsBlankSession(t *testing.T) {
	application, _ := sessionTestApp(&fakeClient{})
	application.sessionBlank = true
	application.model.Sessions = []view.SessionSummary{{ID: "original", Blank: true}}
	application.handleNotification("session/event", json.RawMessage(`{"sessionId":"original","seq":0,"event":{"seq":0,"time":1,"type":"user/message","data":{"source":{"kind":"user"},"content":[{"type":"text","text":"hello"}]}}}`))
	if application.sessionBlank || application.model.Sessions[0].Blank {
		t.Fatal("committed event left the session blank")
	}
}

func TestNonPromptEventKeepsBlankSession(t *testing.T) {
	application, _ := sessionTestApp(&fakeClient{})
	application.sessionBlank = true
	application.model.Sessions = []view.SessionSummary{{ID: "original", Blank: true}}
	application.handleNotification("session/event", json.RawMessage(`{"sessionId":"original","seq":0,"event":{"seq":0,"time":1,"type":"session/notice","data":{}}}`))
	if !application.sessionBlank || !application.model.Sessions[0].Blank {
		t.Fatal("a non-prompt event cleared the blank session")
	}
}

func TestSessionSummaryDecodeCarriesBlankAndRejectsInvalidFlag(t *testing.T) {
	const summary = `{"id":"saved","title":"Saved","cwd":"C:/work","provider":"test","model":"echo","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"blank":true,"nextSeq":0}`
	decoded, err := decodeSessionSummary(json.RawMessage(summary))
	if err != nil || !decoded.Blank {
		t.Fatalf("blank summary = %+v, %v", decoded, err)
	}
	if _, err := decodeSessionSummary(json.RawMessage(strings.Replace(summary, `"blank":true`, `"blank":"yes"`, 1))); err == nil {
		t.Fatal("non-boolean blank was accepted")
	}
}
