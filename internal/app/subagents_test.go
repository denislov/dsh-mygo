package app

import (
	"encoding/json"
	"testing"
)

func TestSubagentNotificationsTrackOnlyTheCurrentSession(t *testing.T) {
	application := newTestApp(&fakeClient{})
	application.options.SDKProtocol = false
	application.model.SessionID = "s1"
	application.handleNotification("subagent/started", json.RawMessage(`{
		"parentSessionId": "s1", "childSessionId": "child-1", "provider": "in-process"
	}`))
	if len(application.model.Subagents) != 1 {
		t.Fatalf("subagents = %+v", application.model.Subagents)
	}
	started := application.model.Subagents[0]
	if started.ID != "child-1" || !started.Running || started.Provider != "in-process" {
		t.Fatalf("subagent = %+v", started)
	}
	application.handleNotification("subagent/started", json.RawMessage(`{
		"parentSessionId": "other", "childSessionId": "child-2"
	}`))
	if len(application.model.Subagents) != 1 {
		t.Fatalf("foreign child was tracked: %+v", application.model.Subagents)
	}
	application.handleNotification("subagent/finished", json.RawMessage(`{
		"parentSessionId": "s1", "childSessionId": "child-1", "reason": "refusal"
	}`))
	finished := application.model.Subagents[0]
	if finished.Running || finished.Reason != "refusal" {
		t.Fatalf("subagent = %+v", finished)
	}
	application.handleNotification("subagent/finished", json.RawMessage(`{
		"parentSessionId": "s1", "childSessionId": "child-1", "reason": "made-up"
	}`))
	if application.model.Subagents[0].Reason != "error" {
		t.Fatalf("subagent = %+v", application.model.Subagents[0])
	}
	application.handleNotification("subagent/started", json.RawMessage(`{
		"parentSessionId": "s1", "childSessionId": "bad id!/../x"
	}`))
	application.handleNotification("subagent/started", json.RawMessage(`{"parentSessionId":"s1"}`))
	application.handleNotification("subagent/started", json.RawMessage(`not json`))
	if len(application.model.Subagents) != 1 {
		t.Fatalf("invalid payloads were tracked: %+v", application.model.Subagents)
	}
}

func TestSubagentNotificationsBoundTheTrackedList(t *testing.T) {
	application := newTestApp(&fakeClient{})
	application.options.SDKProtocol = false
	application.model.SessionID = "s1"
	for index := 0; index < maxTrackedSubagents+4; index++ {
		payload, _ := json.Marshal(map[string]string{
			"parentSessionId": "s1",
			"childSessionId":  "child-" + string(rune('a'+index)),
		})
		application.handleNotification("subagent/started", payload)
	}
	if len(application.model.Subagents) != maxTrackedSubagents {
		t.Fatalf("subagents = %d", len(application.model.Subagents))
	}
	if application.model.Subagents[0].ID != "child-e" {
		t.Fatalf("oldest entry was not dropped: %+v", application.model.Subagents)
	}
}
