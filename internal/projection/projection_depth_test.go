package projection

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTodoWriteReplacesTheStandingChecklist(t *testing.T) {
	var transcript Transcript
	first := Event{Seq: 1, Type: "todo/write", Data: json.RawMessage(`{"todos":[
		{"content":"Inspect projection","status":"completed"},
		{"content":"Project todo panel","status":"in_progress"},
		{"content":"Write tests","status":"pending"}
	]}`)}
	if !transcript.Apply(first) {
		t.Fatal("todo snapshot was not projected")
	}
	if len(transcript.Tasks) != 3 || transcript.Tasks[0].Content != "Inspect projection" || transcript.Tasks[1].Status != "in_progress" {
		t.Fatalf("tasks = %+v", transcript.Tasks)
	}
	if len(transcript.Items()) != 0 {
		t.Fatalf("todo snapshot added a row: %+v", transcript.Items())
	}
	if transcript.Apply(Event{Seq: 2, Type: "todo/write", Data: json.RawMessage(`{"todos":[{"content":"Only item","status":"pending"}]}`)}) != true {
		t.Fatal("replacement was not reported")
	}
	if len(transcript.Tasks) != 1 || transcript.Tasks[0].Content != "Only item" {
		t.Fatalf("tasks = %+v", transcript.Tasks)
	}
	if transcript.Apply(Event{Seq: 3, Type: "todo/write", Data: json.RawMessage(`{"todos":[{"content":"Only item","status":"pending"}]}`)}) {
		t.Fatal("identical snapshot reported a change")
	}
}

func TestTurnStartClearsTheChecklist(t *testing.T) {
	var transcript Transcript
	transcript.Apply(Event{Seq: 1, Type: "todo/write", Data: json.RawMessage(`{"todos":[{"content":"Item","status":"pending"}]}`)})
	if !transcript.Apply(Event{Seq: 2, Type: "turn/start", Data: json.RawMessage(`{"turn":2}`)}) {
		t.Fatal("turn start did not clear the checklist")
	}
	if len(transcript.Tasks) != 0 {
		t.Fatalf("tasks = %+v", transcript.Tasks)
	}
	if transcript.Apply(Event{Seq: 3, Type: "turn/start", Data: json.RawMessage(`{"turn":3}`)}) {
		t.Fatal("empty checklist reported a change")
	}
}

func TestTodoWriteSkipsInvalidRowsAndBoundsTheList(t *testing.T) {
	var transcript Transcript
	rows := make([]string, 0, 80)
	for index := 0; index < 80; index++ {
		rows = append(rows, `{"content":"item","status":"pending"}`)
	}
	rows = append(rows,
		`{"content":"  ","status":"pending"}`,
		`{"content":"bad status","status":"done"}`,
		`{"content":"control\nchars","status":"completed"}`,
	)
	event := Event{Seq: 1, Type: "todo/write", Data: json.RawMessage(`{"todos":[` + strings.Join(rows, ",") + `]}`)}
	if !transcript.Apply(event) {
		t.Fatal("snapshot was not projected")
	}
	if len(transcript.Tasks) != maxTaskCount {
		t.Fatalf("tasks = %d", len(transcript.Tasks))
	}
	for _, task := range transcript.Tasks {
		if task.Content == "" || strings.ContainsAny(task.Content, "\n\r") {
			t.Fatalf("task = %+v", task)
		}
	}
}

func TestPlanModeTracksTheLatestValue(t *testing.T) {
	var transcript Transcript
	if !transcript.Apply(Event{Seq: 1, Type: "plan/mode", Data: json.RawMessage(`{"active":true}`)}) {
		t.Fatal("plan mode on was not projected")
	}
	if !transcript.PlanActive {
		t.Fatal("plan mode did not turn on")
	}
	if transcript.Apply(Event{Seq: 2, Type: "plan/mode", Data: json.RawMessage(`{"active":true}`)}) {
		t.Fatal("identical plan mode reported a change")
	}
	if !transcript.Apply(Event{Seq: 3, Type: "plan/mode", Data: json.RawMessage(`{"active":false}`)}) || transcript.PlanActive {
		t.Fatal("plan mode did not turn off")
	}
}

func TestGoalChangeTracksSnapshotsAndClear(t *testing.T) {
	var transcript Transcript
	create := Event{Seq: 1, Type: "goal/change", Data: json.RawMessage(`{
		"kind":"goal/change","version":1,"operation":"create",
		"goal":{"id":"goal-1","revision":1,"objective":"Ship\nM2-6","phase":"active","maxGoalRounds":10},
		"roundsStarted":1,"createdAt":1,"updatedAt":2
	}`)}
	if !transcript.Apply(create) {
		t.Fatal("goal create was not projected")
	}
	goal := transcript.Goal
	if goal == nil || goal.Objective != "Ship M2-6" || goal.Phase != "active" || goal.MaxRounds != 10 || goal.RoundsStarted != 1 {
		t.Fatalf("goal = %+v", goal)
	}
	update := Event{Seq: 2, Type: "goal/change", Data: json.RawMessage(`{
		"operation":"pause","goal":{"id":"goal-1","revision":2,"objective":"Ship M2-6","phase":"paused","maxGoalRounds":10},"roundsStarted":3
	}`)}
	if !transcript.Apply(update) || transcript.Goal.Phase != "paused" || transcript.Goal.RoundsStarted != 3 {
		t.Fatalf("goal = %+v", transcript.Goal)
	}
	if !transcript.Apply(Event{Seq: 3, Type: "goal/change", Data: json.RawMessage(`{"operation":"clear","cleared":{"id":"goal-1","revision":3},"clearedAt":4}`)}) {
		t.Fatal("goal clear was not projected")
	}
	if transcript.Goal != nil {
		t.Fatalf("goal = %+v", transcript.Goal)
	}
	if transcript.Apply(Event{Seq: 4, Type: "goal/change", Data: json.RawMessage(`{"operation":"clear","cleared":{"id":"goal-1","revision":3}}`)}) {
		t.Fatal("second clear reported a change")
	}
}

func TestMalformedGoalChangeIsIgnored(t *testing.T) {
	var transcript Transcript
	for index, data := range []string{
		`{"operation":"create"}`,
		`{"operation":"create","goal":{"objective":"","phase":"active","maxGoalRounds":5}}`,
		`{"operation":"create","goal":{"objective":"x","phase":"unknown","maxGoalRounds":5}}`,
		`{"operation":"create","goal":{"objective":"ok","phase":"active","maxGoalRounds":-5},"roundsStarted":-1}`,
	} {
		transcript.Apply(Event{Seq: uint64(index + 1), Type: "goal/change", Data: json.RawMessage(data)})
	}
	if transcript.Goal == nil {
		t.Fatal("no valid goal was projected")
	}
	if transcript.Goal.MaxRounds != 0 || transcript.Goal.RoundsStarted != 0 || transcript.Goal.Objective != "ok" {
		t.Fatalf("goal = %+v", transcript.Goal)
	}
}
