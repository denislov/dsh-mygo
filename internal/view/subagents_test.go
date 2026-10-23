package view

import "testing"

func TestSubagentLabelCountsStates(t *testing.T) {
	model := NewModel()
	if label := subagentLabel(model); label != "" {
		t.Fatalf("empty label %q", label)
	}
	model.Subagents = []SubagentState{
		{ID: "child-1", Running: true},
		{ID: "child-2", Running: true},
		{ID: "child-3", Running: false, Reason: "completed"},
	}
	if label := subagentLabel(model); label != "Subagents · 2 running · 1 done" {
		t.Fatalf("label = %q", label)
	}
	model.Prefs.Value.Locale = "zh-CN"
	if label := subagentLabel(model); label != "子代理 · 2 运行中 · 1 已完成" {
		t.Fatalf("label = %q", label)
	}
	model.Subagents = []SubagentState{{ID: "child-1", Running: false, Reason: "error"}}
	if label := subagentLabel(model); label != "子代理 · 1 已完成" {
		t.Fatalf("label = %q", label)
	}
}

func TestHeaderShowsSubagentRow(t *testing.T) {
	model := sessionModel()
	model.Subagents = []SubagentState{{ID: "child-1", Running: true}}
	tt := tester(model)
	if !tt.HasText("Subagents · 1 running") {
		t.Fatalf("subagent row missing in %v", tt.Texts())
	}
	model.Subagents = nil
	tt.Frame()
	if tt.HasText("Subagents · 1 running") {
		t.Fatalf("subagent row still shown in %v", tt.Texts())
	}
}
