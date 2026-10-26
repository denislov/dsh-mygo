package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestManagementPayloadsMatchSharedFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "protocol", "management.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rename                json.RawMessage `json:"rename"`
		Archive               json.RawMessage `json:"archive"`
		Unarchive             json.RawMessage `json:"unarchive"`
		ArchiveDefault        json.RawMessage `json:"archiveDefault"`
		SubagentList          json.RawMessage `json:"subagentList"`
		SubagentHistory       json.RawMessage `json:"subagentHistory"`
		SubagentHistoryLatest json.RawMessage `json:"subagentHistoryLatest"`
		SubagentListResult    json.RawMessage `json:"subagentListResult"`
		SubagentHistoryResult json.RawMessage `json:"subagentHistoryResult"`
		SubagentStarted       json.RawMessage `json:"subagentStarted"`
		SubagentFinished      json.RawMessage `json:"subagentFinished"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	archived, unarchived := true, false
	before := uint64(61)
	for _, test := range []struct {
		name  string
		value any
		want  json.RawMessage
	}{
		{"rename", SessionRenameParams{SessionID: "stored-1", Title: "  工作会话  "}, fixture.Rename},
		{"archive", SessionArchiveParams{SessionID: "stored-1", Archived: &archived}, fixture.Archive},
		{"unarchive", SessionArchiveParams{SessionID: "stored-1", Archived: &unarchived}, fixture.Unarchive},
		{"archive default", SessionArchiveParams{SessionID: "stored-1"}, fixture.ArchiveDefault},
		{"subagent list", SubagentListParams{ParentSessionID: "parent"}, fixture.SubagentList},
		{"subagent history", SubagentHistoryParams{ParentSessionID: "parent", ChildSessionID: "child", BeforeSeq: &before}, fixture.SubagentHistory},
		{"subagent history latest", SubagentHistoryParams{ParentSessionID: "parent", ChildSessionID: "child"}, fixture.SubagentHistoryLatest},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertJSONEqual(t, test.value, test.want)
		})
	}
	var list SubagentListResult
	if err := json.Unmarshal(fixture.SubagentListResult, &list); err != nil || list.ParentSessionID != "parent" || len(list.Children) != 1 {
		t.Fatalf("list result = %+v, %v", list, err)
	}
	child := list.Children[0]
	if child.ID != "child" || child.Label == nil || *child.Label != "Inspect files" || child.Mode != SubagentModeContinuable || child.Status != SubagentStatusStored || child.CreatedAt == nil || *child.CreatedAt != 1 {
		t.Fatalf("child = %+v", child)
	}
	var history SubagentHistoryResult
	if err := json.Unmarshal(fixture.SubagentHistoryResult, &history); err != nil || history.ParentSessionID != "parent" || history.ChildSessionID != "child" || history.StartSeq == nil || *history.StartSeq != 5 || history.EndSeq == nil || *history.EndSeq != 7 || history.NextSeq == nil || *history.NextSeq != 7 || history.InheritedEventCount == nil || *history.InheritedEventCount != 5 || history.HasMore == nil || *history.HasMore || len(history.Events) != 2 {
		t.Fatalf("history result = %+v, %v", history, err)
	}
	for _, test := range []struct {
		name string
		wire json.RawMessage
	}{
		{"started", fixture.SubagentStarted},
		{"finished", fixture.SubagentFinished},
	} {
		var status SubagentStatusParams
		if err := json.Unmarshal(test.wire, &status); err != nil || status.ParentSessionID != "parent" || status.ChildSessionID != "child" {
			t.Fatalf("%s = %+v, %v", test.name, status, err)
		}
	}
	var missing SubagentHistoryResult
	if err := json.Unmarshal([]byte(`{"parentSessionId":"parent","childSessionId":"child","events":[]}`), &missing); err != nil || missing.StartSeq != nil || missing.HasMore != nil || missing.Events == nil {
		t.Fatal("missing history boundaries must stay distinguishable")
	}
	var absentChild SubagentSummary
	if err := json.Unmarshal([]byte(`{"id":"child","mode":"external","status":"external"}`), &absentChild); err != nil || absentChild.Label != nil || absentChild.CreatedAt != nil {
		t.Fatal("missing catalog fields must stay distinguishable")
	}
	if MethodSessionRename != "session/rename" || MethodSessionArchive != "session/archive" || MethodSubagentList != "subagent/list" || MethodSubagentHistory != "subagent/history" {
		t.Fatal("management method names changed")
	}
}
