package view

import (
	"testing"

	"dsh-mygo/internal/projection"
)

func TestTaskPanelShowsChecklistAndProgress(t *testing.T) {
	model := sessionModel()
	model.Tasks = []projection.Task{
		{Content: "Inspect projection", Status: "completed"},
		{Content: "Project todo panel", Status: "in_progress"},
		{Content: "Write tests", Status: "pending"},
	}
	tt := tester(model)
	if !tt.HasText("Tasks 1/3") {
		t.Fatalf("progress missing in %v", tt.Texts())
	}
	if !tt.HasText("✓ Inspect projection") || !tt.HasText("▶ Project todo panel") || !tt.HasText("○ Write tests") {
		t.Fatalf("tasks missing in %v", tt.Texts())
	}
	model.Prefs.Value.Locale = "zh-CN"
	tt.Frame()
	if !tt.HasText("任务 1/3") {
		t.Fatalf("Chinese progress missing in %v", tt.Texts())
	}
}

func TestTaskPanelBoundsVisibleRows(t *testing.T) {
	model := sessionModel()
	model.Tasks = make([]projection.Task, 0, 11)
	for index := 0; index < 11; index++ {
		model.Tasks = append(model.Tasks, projection.Task{Content: "task", Status: "pending"})
	}
	tt := tester(model)
	if !tt.HasText("+3 more") {
		t.Fatalf("overflow counter missing in %v", tt.Texts())
	}
}

func TestTaskPanelHiddenWithoutChecklist(t *testing.T) {
	model := sessionModel()
	tt := tester(model)
	if tt.HasText("Tasks 0/0") || tt.HasText("Tasks") {
		t.Fatalf("empty checklist rendered in %v", tt.Texts())
	}
}

func TestDepthLabelCombinesPlanAndGoal(t *testing.T) {
	model := NewModel()
	if label := depthLabel(model); label != "" {
		t.Fatalf("empty depth rendered %q", label)
	}
	model.PlanActive = true
	if label := depthLabel(model); label != "Plan mode" {
		t.Fatalf("label = %q", label)
	}
	model.Goal = &projection.Goal{Objective: "Ship M2-6", Phase: "active", MaxRounds: 10, RoundsStarted: 3}
	want := "Plan mode · Goal: Ship M2-6 · active · 3/10"
	if label := depthLabel(model); label != want {
		t.Fatalf("label = %q, want %q", label, want)
	}
	model.Prefs.Value.Locale = "zh-CN"
	want = "计划模式 · 目标: Ship M2-6 · 进行中 · 3/10"
	if label := depthLabel(model); label != want {
		t.Fatalf("label = %q, want %q", label, want)
	}
	model.Goal.Phase = "blocked"
	model.PlanActive = false
	if label := depthLabel(model); label != "目标: Ship M2-6 · 受阻 · 3/10" {
		t.Fatalf("label = %q", label)
	}
}

func TestHeaderShowsDepthRow(t *testing.T) {
	model := sessionModel()
	model.PlanActive = true
	model.Goal = &projection.Goal{Objective: "Ship M2-6", Phase: "paused", MaxRounds: 10, RoundsStarted: 2}
	tt := tester(model)
	if !tt.HasText("Plan mode · Goal: Ship M2-6 · paused · 2/10") {
		t.Fatalf("depth row missing in %v", tt.Texts())
	}
	model.PlanActive = false
	model.Goal = nil
	tt.Frame()
	if tt.HasText("Plan mode") {
		t.Fatalf("depth row still shown in %v", tt.Texts())
	}
}
