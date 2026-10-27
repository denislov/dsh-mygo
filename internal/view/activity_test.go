package view

import (
	"testing"
	"time"

	"dsh-mygo/internal/projection"
)

func activityModel() *Model {
	model := NewModel()
	model.Phase = PhaseIdle
	model.Items = []projection.Item{
		{ID: "u1", Kind: projection.KindUser, Title: "You", Text: "hello"},
		{ID: "seq:2:r:0", Kind: projection.KindReasoning, Title: "Reasoning", Text: "private reasoning", Turn: 1, Step: 1, ActivityKey: "activity:1:1:r:0", Time: 1000},
		{ID: "tool:c1", Kind: projection.KindTool, Title: "Tool · read_file", Text: "arguments preview", Detail: "result preview", State: "done", Turn: 1, Step: 1, Time: 4000, EndTime: 6000},
		{ID: "seq:3", Kind: projection.KindAssistant, Title: "Assistant", Text: "answer"},
	}
	return model
}

func TestActivityGroupStartsCollapsedAndExpands(t *testing.T) {
	model := activityModel()
	tester := tester(model)
	if tester.HasText("private reasoning") || tester.HasText("arguments preview") {
		t.Fatalf("activity started expanded: %v", tester.Texts())
	}
	if !tester.HasText("Thought for 5s") || !tester.HasText("1 tool") {
		t.Fatalf("summary missing: %v", tester.Texts())
	}
	if err := tester.Click("Thought for 5s · 1 tool"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(300 * time.Millisecond)
	if !tester.HasText("private reasoning") {
		t.Fatal("reasoning did not expand")
	}
	if tester.HasText("arguments preview") {
		t.Fatal("nested tool card started expanded")
	}
	if err := tester.Click("Tool · read_file"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !tester.HasText("arguments preview") || !tester.HasText("result preview") {
		t.Fatal("tool card did not expand inside the group")
	}
	if !model.Activity["activity:1:1:r:0"].Expanded || !model.ToolCards["tool:c1"].Expanded {
		t.Fatal("expansion state was not retained")
	}
}

func TestToolOnlyRowsGroupAndSummarizeNames(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	model.Items = []projection.Item{
		{ID: "tool:a", Kind: projection.KindTool, Title: "Tool · read_file", Text: "first arguments", State: "done", Turn: 2, Step: 1},
		{ID: "tool:b", Kind: projection.KindTool, Title: "Tool · grep", Text: "second arguments", State: "running", Turn: 2, Step: 1},
	}
	tester := tester(model)
	if tester.HasText("first arguments") {
		t.Fatal("tool rows were not collapsed")
	}
	if !tester.HasText("Working… · 2 tools") {
		t.Fatalf("tool summary missing: %v", tester.Texts())
	}
	if err := tester.Click("Working… · 2 tools"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(300 * time.Millisecond)
	if !tester.HasText("Tool · read_file") || !tester.HasText("Tool · grep") {
		t.Fatal("tools did not appear inside the group")
	}
	if err := tester.Click("Tool · read_file"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !tester.HasText("first arguments") {
		t.Fatal("nested tool card did not expand")
	}
}

func TestTurnProcessFoldsNarrationBeforeFinalAnswer(t *testing.T) {
	items := []projection.Item{
		{ID: "r1", Kind: projection.KindReasoning, ActivityKey: "activity:1:1:r:0", Turn: 1},
		{ID: "tool:t1", Kind: projection.KindTool, Turn: 1},
		{ID: "a1", Kind: projection.KindAssistant, Text: "narration", Turn: 1},
		{ID: "r2", Kind: projection.KindReasoning, ActivityKey: "activity:1:2:r:0", Turn: 1},
		{ID: "a2", Kind: projection.KindAssistant, Text: "final", Turn: 1},
		{ID: "tool:t2", Kind: projection.KindTool, Turn: 2},
	}
	rows := buildTranscriptRows(items)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].activity == nil || rows[0].key != "activity:1:1:r:0" || len(rows[0].activity.items) != 4 {
		t.Fatalf("process group = %+v", rows[0])
	}
	if rows[1].item == nil || rows[1].item.ID != "a2" {
		t.Fatalf("final answer = %+v", rows[1])
	}
	if rows[2].activity == nil || rows[2].key != "activity:tool:t2" || len(rows[2].activity.items) != 1 {
		t.Fatalf("turn break = %+v", rows[2])
	}
}

func TestTurnWithoutFinalAnswerFoldsAllProcessRows(t *testing.T) {
	items := []projection.Item{
		{ID: "a1", Kind: projection.KindAssistant, Text: "first narration", Turn: 1},
		{ID: "a2", Kind: projection.KindAssistant, Text: "second narration", Turn: 1},
		{ID: "tool:t1", Kind: projection.KindTool, Turn: 1},
	}
	rows := buildTranscriptRows(items)
	if len(rows) != 1 || rows[0].activity == nil || len(rows[0].activity.items) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].key != "activity:a1" {
		t.Fatalf("group key = %q", rows[0].key)
	}
}

func TestSingleAssistantAnswerStaysOutsideAProcessGroup(t *testing.T) {
	rows := buildTranscriptRows([]projection.Item{
		{ID: "a1", Kind: projection.KindAssistant, Text: "answer", Turn: 1},
	})
	if len(rows) != 1 || rows[0].item == nil || rows[0].item.ID != "a1" {
		t.Fatalf("rows = %+v", rows)
	}
}

// Unpositioned rows cannot prove they share a turn, so only the legacy
// reasoning/tool folding applies.
func TestUnpositionedAssistantTextBreaksLegacyActivityGroup(t *testing.T) {
	items := []projection.Item{
		{ID: "r1", Kind: projection.KindReasoning, ActivityKey: "activity:1:1:r:0"},
		{ID: "tool:t1", Kind: projection.KindTool},
		{ID: "a1", Kind: projection.KindAssistant, Text: "answer"},
		{ID: "r2", Kind: projection.KindReasoning, ActivityKey: "activity:1:2:r:0"},
		{ID: "tool:t2", Kind: projection.KindTool},
	}
	rows := buildTranscriptRows(items)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].activity == nil || rows[0].key != "activity:1:1:r:0" || len(rows[0].activity.items) != 2 {
		t.Fatalf("first group = %+v", rows[0])
	}
	if rows[1].item == nil || rows[1].item.ID != "a1" {
		t.Fatalf("assistant row = %+v", rows[1])
	}
	if rows[2].activity == nil || rows[2].key != "activity:1:2:r:0" || len(rows[2].activity.items) != 2 {
		t.Fatalf("second group = %+v", rows[2])
	}
}

func TestTurnRendersOneCopyActionForItsFinalAnswer(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	model.Items = []projection.Item{
		{ID: "u", Kind: projection.KindUser, Title: "You", Text: "hello"},
		{ID: "a1", Kind: projection.KindAssistant, Title: "Assistant", Text: "Let me inspect the files.", Turn: 1, Step: 1, Time: 1000, EndTime: 1000},
		{ID: "tool:c1", Kind: projection.KindTool, Title: "Tool · read_file", Text: "arguments", State: "done", Turn: 1, Step: 1, Time: 1000, EndTime: 3000},
		{ID: "a2", Kind: projection.KindAssistant, Title: "Assistant", Text: "Here is the final answer.", Turn: 1, Step: 2, Time: 4000, EndTime: 4000},
	}
	tester := tester(model)
	if tester.HasText("Let me inspect the files.") {
		t.Fatal("earlier assistant narration was not folded")
	}
	if !tester.HasText("Here is the final answer.") {
		t.Fatal("final answer missing")
	}
	if got := countExactText(tester.Texts(), "Copy Markdown"); got != 1 {
		t.Fatalf("copy actions = %d, want 1", got)
	}
	if err := tester.Click("Worked for 2s · 1 tool"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(300 * time.Millisecond)
	if !tester.HasText("Let me inspect the files.") {
		t.Fatal("earlier assistant narration did not expand")
	}
	if got := countExactText(tester.Texts(), "Copy Markdown"); got != 1 {
		t.Fatalf("copy actions after expanding = %d, want 1", got)
	}
}

func TestTurnInToolWorkHasNoAnswerCopyAction(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	model.Items = []projection.Item{
		{ID: "a1", Kind: projection.KindAssistant, Title: "Assistant", Text: "Checking the file.", Turn: 1, Step: 1},
		{ID: "tool:c1", Kind: projection.KindTool, Title: "Tool · read_file", Text: "arguments", State: "running", Turn: 1, Step: 1},
	}
	tester := tester(model)
	if tester.HasText("Checking the file.") {
		t.Fatal("unfinished turn narration was not folded")
	}
	if got := countExactText(tester.Texts(), "Copy Markdown"); got != 0 {
		t.Fatalf("copy actions = %d, want 0", got)
	}
	if err := tester.Click("Working… · 1 tool"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(300 * time.Millisecond)
	if !tester.HasText("Checking the file.") {
		t.Fatal("unfinished turn narration did not expand")
	}
	if got := countExactText(tester.Texts(), "Copy Markdown"); got != 0 {
		t.Fatalf("copy actions after expanding = %d, want 0", got)
	}
}

func TestAssistantOnlyProcessUsesEarlierMessagesSummary(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	model.Items = []projection.Item{
		{ID: "a1", Kind: projection.KindAssistant, Title: "Assistant", Text: "first narration", Turn: 1},
		{ID: "a2", Kind: projection.KindAssistant, Title: "Assistant", Text: "second narration", Turn: 1},
	}
	tester := tester(model)
	if !tester.HasText("Earlier messages") || !tester.HasText("second narration") || tester.HasText("first narration") {
		t.Fatalf("assistant-only process = %v", tester.Texts())
	}
	if got := countExactText(tester.Texts(), "Copy Markdown"); got != 1 {
		t.Fatalf("copy actions = %d, want 1", got)
	}
}

func countExactText(texts []string, want string) int {
	count := 0
	for _, text := range texts {
		if text == want {
			count++
		}
	}
	return count
}

func TestActivityPruneAndReset(t *testing.T) {
	model := activityModel()
	model.Activity = map[string]*ActivityState{
		"activity:1:1:r:0": {Expanded: true},
		"stale":            {Expanded: true},
	}
	model.PruneTranscriptState(model.Items)
	if model.Activity["stale"] != nil || model.Activity["activity:1:1:r:0"] == nil {
		t.Fatalf("activity state = %+v", model.Activity)
	}
	model.ResetTranscript(nil)
	if len(model.Activity) != 0 {
		t.Fatal("reset retained activity state")
	}
}

func TestStreamingReasoningShowsLiveSummaryAndExpands(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseIdle
	model.Items = []projection.Item{
		{ID: "stream:s1:r:0", Kind: projection.KindReasoning, State: "streaming", Text: "live thought", Turn: 1, Step: 1, ActivityKey: "activity:1:1:r:0"},
		{ID: "stream:s1", Kind: projection.KindAssistant, State: "streaming", Text: "partial answer", Detail: "Streaming…"},
	}
	tester := tester(model)
	if !tester.HasText("Thinking…") {
		t.Fatalf("live summary missing: %v", tester.Texts())
	}
	if tester.HasText("live thought") {
		t.Fatal("streaming reasoning started expanded")
	}
	if !tester.HasText("partial answer") {
		t.Fatal("assistant preview missing")
	}
	if err := tester.Click("Thinking…"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(300 * time.Millisecond)
	if !tester.HasText("live thought") {
		t.Fatal("streaming reasoning did not expand")
	}
}

func TestActivitySummaryIsLocalized(t *testing.T) {
	model := activityModel()
	model.Prefs.Value.Locale = "zh-CN"
	tester := tester(model)
	if !tester.HasText("思考了 5s") || !tester.HasText("1 个工具") {
		t.Fatalf("localized summary missing: %v", tester.Texts())
	}
}
