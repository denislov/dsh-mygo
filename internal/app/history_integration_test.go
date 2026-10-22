package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/host"
	"dsh-mygo/internal/projection"
)

func TestMygoRealHistoryAfterRestart(t *testing.T) {
	launch, root := realMygoLaunch(t, true)
	requestContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	type notification struct {
		method string
		params json.RawMessage
	}
	start := func() (*channel.Client, <-chan notification, func()) {
		t.Helper()
		supervisor, err := host.Start(requestContext, launch)
		if err != nil {
			t.Fatal(err)
		}
		client := channel.NewClient(supervisor.Stdout(), supervisor.Stdin(), 0)
		notifications := make(chan notification, 512)
		client.OnNotification(func(method string, params json.RawMessage) {
			select {
			case notifications <- notification{method, params}:
			case <-requestContext.Done():
			}
		})
		go func() { _ = client.Run() }()
		closed := false
		stop := func() {
			if closed {
				return
			}
			closed = true
			defer client.Close()
			stopContext, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer stopCancel()
			if _, err := client.Call(stopContext, "shutdown", map[string]any{}); err != nil {
				_ = supervisor.Stop(stopContext)
				t.Errorf("shutdown: %v", err)
			}
			select {
			case <-supervisor.Done():
				if supervisor.WaitError() != nil {
					t.Errorf("host exit: %v", supervisor.WaitError())
				}
			case <-stopContext.Done():
				_ = supervisor.Stop(stopContext)
				t.Error("host did not exit after shutdown")
			}
		}
		t.Cleanup(stop)
		return client, notifications, stop
	}
	call := func(client *channel.Client, method string, params any, result any) {
		t.Helper()
		raw, err := client.Call(requestContext, method, params)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if result != nil {
			if err := json.Unmarshal(raw, result); err != nil {
				t.Fatalf("%s result: %v", method, err)
			}
		}
	}
	initialize := func(client *channel.Client) {
		t.Helper()
		var result struct {
			Capabilities []string `json:"capabilities"`
		}
		call(client, "initialize", map[string]any{
			"protocolVersion": channel.ProtocolVersion, "capabilities": []string{"session.history"},
			"client": map[string]string{"name": "dsh-mygo", "version": channel.ReleaseVersion, "platform": "test", "locale": "zh-CN"},
		}, &result)
		if !reflect.DeepEqual(result.Capabilities, []string{"session.history"}) {
			t.Fatalf("capabilities = %v", result.Capabilities)
		}
	}
	type sessionResult struct {
		Session struct {
			ID      string `json:"id"`
			NextSeq int    `json:"nextSeq"`
		} `json:"session"`
	}
	prompt := func(client *channel.Client, notifications <-chan notification, id string) {
		t.Helper()
		call(client, "session/prompt", map[string]any{"sessionId": id, "content": []any{map[string]string{"type": "text", "text": "Reply with exactly: pong"}}}, nil)
		ended := false
		for {
			select {
			case received := <-notifications:
				var envelope struct {
					SessionID string `json:"sessionId"`
					Status    string `json:"status"`
					Event     struct {
						Type string `json:"type"`
					} `json:"event"`
				}
				if err := json.Unmarshal(received.params, &envelope); err != nil {
					t.Fatal(err)
				}
				if received.method == "host/fatal" {
					t.Fatal("host failed during history continuation")
				}
				if envelope.SessionID != id {
					continue
				}
				if received.method == "session/event" && envelope.Event.Type == "turn/end" {
					ended = true
				}
				if ended && received.method == "session/status" && envelope.Status == "idle" {
					return
				}
			case <-requestContext.Done():
				t.Fatal("timed out waiting for the durable turn and idle")
			}
		}
	}
	history := func(client *channel.Client, id string) []map[string]any {
		t.Helper()
		var events []map[string]any
		cursor := ""
		boundary := -1
		for pageCount := 0; pageCount < 1000; pageCount++ {
			params := map[string]any{"sessionId": id, "limit": 3}
			if cursor != "" {
				params["cursor"] = cursor
			}
			var page struct {
				Events     []map[string]any `json:"events"`
				NextCursor string           `json:"nextCursor"`
				HasMore    bool             `json:"hasMore"`
				NextSeq    int              `json:"nextSeq"`
			}
			call(client, "session/history", params, &page)
			if boundary == -1 {
				boundary = page.NextSeq
			}
			if boundary != page.NextSeq || len(page.Events) > 3 {
				t.Fatal("history snapshot changed during pagination")
			}
			for _, event := range page.Events {
				if event["seq"] != float64(len(events)) {
					t.Fatal("history sequence is not contiguous")
				}
				events = append(events, event)
			}
			if !page.HasMore {
				if len(events) != boundary || page.NextCursor != "" {
					t.Fatal("history did not reach its exact boundary")
				}
				return events
			}
			if page.NextCursor == "" || len(page.Events) == 0 {
				t.Fatal("history pagination made no progress")
			}
			cursor = page.NextCursor
		}
		t.Fatal("history exceeded the page limit")
		return nil
	}
	first, firstNotifications, stopFirst := start()
	initialize(first)
	var created sessionResult
	call(first, "session/create", map[string]string{"cwd": root, "provider": "mygo-mock", "model": "echo"}, &created)
	prompt(first, firstNotifications, created.Session.ID)
	original := history(first, created.Session.ID)
	if len(original) == 0 {
		t.Fatal("original history is empty")
	}
	stopFirst()
	second, secondNotifications, stopSecond := start()
	initialize(second)
	var listed struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	call(second, "session/list", map[string]any{}, &listed)
	if len(listed.Sessions) != 1 || listed.Sessions[0].ID != created.Session.ID {
		t.Fatal("restarted host did not list the persisted session")
	}
	var restored sessionResult
	call(second, "session/resume", map[string]string{"sessionId": created.Session.ID, "provider": "mygo-mock", "model": "echo"}, &restored)
	replayed := history(second, restored.Session.ID)
	if len(replayed) < len(original) || !reflect.DeepEqual(original, replayed[:len(original)]) || len(replayed) != restored.Session.NextSeq {
		t.Fatal("restored history did not preserve the original log prefix")
	}
	prompt(second, secondNotifications, restored.Session.ID)
	continued := history(second, restored.Session.ID)
	if len(continued) <= len(replayed) || !reflect.DeepEqual(replayed, continued[:len(replayed)]) {
		t.Fatal("continuation rewrote history or did not append events")
	}
	var transcript projection.Transcript
	for _, event := range continued {
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		var decoded projection.Event
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		transcript.Apply(decoded)
	}
	assistantCount := 0
	for _, item := range transcript.Items() {
		if item.Kind == projection.KindAssistant && item.Text == "pong" {
			assistantCount++
		}
	}
	if assistantCount != 2 {
		t.Fatalf("replayed assistant turns = %d; want 2", assistantCount)
	}
	contender, _, stopContender := start()
	initialize(contender)
	_, err := contender.Call(requestContext, "session/resume", map[string]string{"sessionId": restored.Session.ID, "provider": "mygo-mock", "model": "echo"})
	var rpcError *channel.RPCError
	if !errors.As(err, &rpcError) {
		t.Fatalf("writer contention returned %v; want an RPC error", err)
	}
	var detail struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(rpcError.Data, &detail) != nil || detail.Kind != "session/resume-failed" {
		t.Fatalf("writer contention kind = %q", detail.Kind)
	}
	stopSecond()
	call(contender, "session/resume", map[string]string{"sessionId": restored.Session.ID, "provider": "mygo-mock", "model": "echo"}, &restored)
	retried := history(contender, restored.Session.ID)
	if len(retried) < len(continued) || !reflect.DeepEqual(continued, retried[:len(continued)]) {
		t.Fatal("retry after ownership release lost the durable prefix")
	}
	stopContender()
}
