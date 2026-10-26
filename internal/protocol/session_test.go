package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSessionPayloadsMatchSharedFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "protocol", "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		CreateDefault        json.RawMessage `json:"createDefault"`
		CreateExplicit       json.RawMessage `json:"createExplicit"`
		CreateExplicitEffort json.RawMessage `json:"createExplicitEffort"`
		ResumeDefault        json.RawMessage `json:"resumeDefault"`
		ResumeExplicit       json.RawMessage `json:"resumeExplicit"`
		ResumeExplicitEffort json.RawMessage `json:"resumeExplicitEffort"`
		Summary              json.RawMessage `json:"summary"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		value any
		want  json.RawMessage
	}{
		{"create default", SessionCreateParams{CWD: "C:/work", SessionRoute: HostDefaultRoute()}, fixture.CreateDefault},
		{"create explicit", SessionCreateParams{CWD: "C:/work", SessionRoute: ExplicitRoute("test", "echo", "")}, fixture.CreateExplicit},
		{"create explicit effort", SessionCreateParams{CWD: "C:/work", SessionRoute: ExplicitRoute("test", "echo", "high")}, fixture.CreateExplicitEffort},
		{"resume default", SessionResumeParams{SessionID: "saved", SessionRoute: HostDefaultRoute()}, fixture.ResumeDefault},
		{"resume explicit", SessionResumeParams{SessionID: "saved", SessionRoute: ExplicitRoute("test", "echo", "")}, fixture.ResumeExplicit},
		{"resume explicit effort", SessionResumeParams{SessionID: "saved", SessionRoute: ExplicitRoute("test", "echo", "high")}, fixture.ResumeExplicitEffort},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertJSONEqual(t, test.value, test.want)
		})
	}
	nextSeq := uint64(3)
	var summary SessionSummary
	if err := json.Unmarshal(fixture.Summary, &summary); err != nil {
		t.Fatal(err)
	}
	want := SessionSummary{
		MetadataIncomplete: true, MetadataCached: true, ID: "saved", Title: "Saved session", CWD: "C:/work",
		Provider: "test", Model: "echo", Effort: "high", CreatedAt: 1, UpdatedAt: 2, Status: "idle",
		Archived: false, NextSeq: &nextSeq,
	}
	if !reflect.DeepEqual(summary, want) {
		t.Fatalf("summary = %+v, want %+v", summary, want)
	}
	if MethodSessionCreate != "session/create" || MethodSessionResume != "session/resume" {
		t.Fatal("session method names changed")
	}
}

func TestSessionRouteWireVariantsStaySeparate(t *testing.T) {
	assertJSONEqual(t, HostDefaultRoute(), json.RawMessage(`{"useDefaultModel":true}`))
	assertJSONEqual(t, ExplicitRoute("", "", ""), json.RawMessage(`{"provider":"","model":""}`))
	empty := ""
	route := ExplicitRoute("test", "echo", "")
	route.Effort = &empty
	assertJSONEqual(t, route, json.RawMessage(`{"provider":"test","model":"echo","effort":""}`))
}

func assertJSONEqual(t *testing.T, value any, want []byte) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(encoded, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("encoded = %s, want %s", encoded, want)
	}
}
