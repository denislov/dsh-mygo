package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

const childCatalogJSON = `{"parentSessionId":"s1","children":[{"id":"child","label":"Inspect files","mode":"continuable","status":"stored","createdAt":1}]}`
const childHistoryJSON = `{"parentSessionId":"s1","childSessionId":"child","startSeq":5,"endSeq":7,"nextSeq":7,"inheritedEventCount":5,"hasMore":false,"events":[{"seq":5,"time":1,"type":"assistant/message","data":{"message":{"content":[{"type":"text","text":"Child result"}]}}},{"seq":6,"time":2,"type":"turn/end","data":{"reason":{"kind":"completed"}}}]}`

func TestSubagentInspectionDecodersValidateAddressBoundsAndSequence(t *testing.T) {
	children, err := decodeSubagentCatalog(json.RawMessage(childCatalogJSON), "s1")
	if err != nil || len(children) != 1 || children[0].Label != "Inspect files" {
		t.Fatalf("catalog: %+v %v", children, err)
	}
	for _, raw := range []string{
		strings.Replace(childCatalogJSON, `"s1"`, `"foreign"`, 1),
		strings.Replace(childCatalogJSON, `"createdAt":1`, `"createdAt":null`, 1),
		strings.Replace(childCatalogJSON, `"stored"`, `"made-up"`, 1),
		strings.Replace(childCatalogJSON, `"child"`, `"../child"`, 1),
		`{"parentSessionId":"s1","children":null}`,
	} {
		if _, err := decodeSubagentCatalog(json.RawMessage(raw), "s1"); err == nil {
			t.Fatalf("accepted invalid catalog %s", raw)
		}
	}
	page, err := decodeSubagentHistory(json.RawMessage(childHistoryJSON), "s1", "child", nil)
	if err != nil || page.Start != 5 || page.EndReason != "completed" || len(page.Items) != 1 || page.Items[0].Text != "Child result" {
		t.Fatalf("page: %+v %v", page, err)
	}
	for _, raw := range []string{
		strings.Replace(childHistoryJSON, `"startSeq":5`, `"startSeq":4`, 1),
		strings.Replace(childHistoryJSON, `"seq":6`, `"seq":8`, 1),
		strings.Replace(childHistoryJSON, `"hasMore":false`, `"hasMore":true`, 1),
		strings.Replace(childHistoryJSON, `"time":1`, `"time":null`, 1),
		strings.Replace(childHistoryJSON, `"assistant/message"`, `"request/header"`, 1),
		strings.Replace(childHistoryJSON, `"endSeq":7`, `"endSeq":null`, 1),
	} {
		if _, err := decodeSubagentHistory(json.RawMessage(raw), "s1", "child", nil); err == nil {
			t.Fatalf("accepted invalid page %s", raw)
		}
	}
	before := uint64(6)
	if _, err := decodeSubagentHistory(json.RawMessage(childHistoryJSON), "s1", "child", &before); err == nil {
		t.Fatal("accepted the wrong earlier window")
	}
	continued := strings.Replace(childHistoryJSON, `"endSeq":7,"nextSeq":7`, `"endSeq":8,"nextSeq":8`, 1)
	continued = strings.TrimSuffix(continued, "]}") + `,{"seq":7,"time":3,"type":"turn/start","data":{}}]}`
	page, err = decodeSubagentHistory(json.RawMessage(continued), "s1", "child", nil)
	if err != nil || page.EndReason != "" {
		t.Fatalf("continued task kept an earlier result: %+v %v", page, err)
	}
}

func TestSubagentInspectionReadCloseSwitchAndFailureIsolation(t *testing.T) {
	client := &fakeClient{results: map[string]json.RawMessage{"subagent/list": json.RawMessage(childCatalogJSON), "subagent/history": json.RawMessage(childHistoryJSON)}}
	a, updates := newMygoSendApp(client)
	a.model.SessionID, a.sessionID = "s1", "s1"
	a.model.Inspection.Available = true
	a.model.Phase = view.PhaseRunning
	a.model.Draft = "keep the parent draft"
	a.OpenSubagents()
	waitFor(t, a, updates, 3*time.Second, func() bool { return !a.model.Inspection.Busy })
	if len(a.model.Inspection.Children) != 1 {
		t.Fatal("children not loaded while parent running")
	}
	if list, ok := client.params[len(client.params)-1].(protocol.SubagentListParams); !ok || list.ParentSessionID != "s1" {
		t.Fatalf("list params = %#v", client.params[len(client.params)-1])
	}
	a.SelectSubagent("child")
	waitFor(t, a, updates, 3*time.Second, func() bool { return !a.model.Inspection.Busy })
	if a.model.Inspection.EndReason != "completed" || a.model.Inspection.Content.Items[0].Text != "Child result" || a.model.Draft != "keep the parent draft" {
		t.Fatal("inspection changed the parent or lost the child output")
	}
	if history, ok := client.params[len(client.params)-1].(protocol.SubagentHistoryParams); !ok || history.ParentSessionID != "s1" || history.ChildSessionID != "child" || history.BeforeSeq != nil {
		t.Fatalf("history params = %#v", client.params[len(client.params)-1])
	}
	a.RefreshSubagents()
	var late func()
	select {
	case late = <-updates:
	case <-time.After(3 * time.Second):
		t.Fatal("no pending refresh")
	}
	a.CloseSubagents()
	late()
	if a.model.Inspection.Open || a.model.Inspection.Content != nil {
		t.Fatal("close accepted a late reply")
	}
	a.OpenSubagents()
	select {
	case late = <-updates:
	case <-time.After(3 * time.Second):
		t.Fatal("no pending list")
	}
	a.model.SessionID, a.sessionID = "other", "other"
	late()
	if len(a.model.Inspection.Children) != 0 {
		t.Fatal("reply crossed parent identity")
	}
	a.CloseSubagents()
	a.model.SessionID, a.sessionID = "s1", "s1"
	client.err = json.Unmarshal([]byte("invalid"), new(any))
	a.OpenSubagents()
	waitFor(t, a, updates, 3*time.Second, func() bool { return !a.model.Inspection.Busy })
	if a.model.Inspection.Error == "" || a.model.Phase != view.PhaseRunning || a.model.Draft != "keep the parent draft" {
		t.Fatal("read failure damaged the parent session")
	}
}
