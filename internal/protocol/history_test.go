package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryPayloadsMatchSharedFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "protocol", "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ListDefault    json.RawMessage `json:"listDefault"`
		List           json.RawMessage `json:"list"`
		Forward        json.RawMessage `json:"forward"`
		ForwardLive    json.RawMessage `json:"forwardLive"`
		Backward       json.RawMessage `json:"backward"`
		BackwardCursor json.RawMessage `json:"backwardCursor"`
		BackwardLive   json.RawMessage `json:"backwardLiveStream"`
		Release        json.RawMessage `json:"release"`
		ListResult     json.RawMessage `json:"listResult"`
		HistoryResult  json.RawMessage `json:"historyResult"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	before := uint64(128)
	for _, test := range []struct {
		name  string
		value any
		want  json.RawMessage
	}{
		{"list default", SessionListParams{Limit: 64}, fixture.ListDefault},
		{"list archived cursor", SessionListParams{Limit: 64, IncludeArchived: true, Cursor: "next"}, fixture.List},
		{"forward", SessionHistoryParams{SessionID: "s1", Limit: 64, Direction: HistoryDirectionForward}, fixture.Forward},
		{"forward live", SessionHistoryParams{SessionID: "s1", Limit: 64, Direction: HistoryDirectionForward, Live: true}, fixture.ForwardLive},
		{"backward before", SessionHistoryParams{SessionID: "s1", Limit: 64, Direction: HistoryDirectionBackward, BeforeSeq: &before}, fixture.Backward},
		{"backward cursor live stream", SessionHistoryParams{SessionID: "s1", Limit: 64, Direction: HistoryDirectionBackward, Cursor: "older", Live: true, AssistantStream: true}, fixture.BackwardCursor},
		{"backward live stream", SessionHistoryParams{SessionID: "s1", Limit: 64, Direction: HistoryDirectionBackward, Live: true, AssistantStream: true}, fixture.BackwardLive},
		{"release", SessionReleaseParams{SessionID: "s1"}, fixture.Release},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertJSONEqual(t, test.value, test.want)
		})
	}
	var list SessionListResult
	if err := json.Unmarshal(fixture.ListResult, &list); err != nil || len(list.Sessions) != 1 || list.NextCursor == nil {
		t.Fatalf("list result = %+v, %v", list, err)
	}
	var cursor string
	if err := json.Unmarshal(list.NextCursor, &cursor); err != nil || cursor != "next" {
		t.Fatalf("cursor = %q, %v", cursor, err)
	}
	var history SessionHistoryResult
	if err := json.Unmarshal(fixture.HistoryResult, &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Events) != 1 || history.StartSeq == nil || *history.StartSeq != 0 || history.NextSeq == nil || *history.NextSeq != 1 || history.HasMore == nil || !*history.HasMore || history.Status != "running" || history.StatusRevision == nil || *history.StatusRevision != 2 || history.AssistantStream == nil {
		t.Fatalf("history result = %+v", history)
	}
	if MethodSessionList != "session/list" || MethodSessionHistory != "session/history" || MethodSessionRelease != "session/release" {
		t.Fatal("history method names changed")
	}
	if HistoryDirectionForward != "forward" || HistoryDirectionBackward != "backward" {
		t.Fatal("history direction names changed")
	}
}
