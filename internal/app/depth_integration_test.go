package app

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/view"
)

// TestAgainstMygoFakeHostDepthEvents projects plan mode, goal and todo snapshots
// from the fake host into the app model.
func TestAgainstMygoFakeHostDepthEvents(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	fakeHost, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 128)
	application := New(Options{
		Launch: host.Config{Command: node, Args: []string{fakeHost, "--depth"}},
		CWD:    t.TempDir(), Provider: "test", Model: "echo",
		Post: func(fn func()) { updates <- fn },
	})
	defer application.Shutdown(2 * time.Second)
	go application.Start()
	waitFor(t, application, updates, 20*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })

	application.Send("hi")
	waitFor(t, application, updates, 20*time.Second, func() bool {
		return application.Model().Phase == view.PhaseIdle && hasAssistantText(application.Model().Items, "Echo: hi")
	})

	tasks := application.Model().Tasks
	if len(tasks) != 2 || tasks[0].Status != "completed" || tasks[1].Status != "in_progress" {
		t.Fatalf("tasks = %+v", tasks)
	}
	if !application.Model().PlanActive {
		t.Fatal("plan mode was not projected")
	}
	goal := application.Model().Goal
	if goal == nil || goal.Objective != "Ship the fixture" || goal.Phase != "active" || goal.MaxRounds != 10 || goal.RoundsStarted != 1 {
		t.Fatalf("goal = %+v", goal)
	}
}

// TestAgainstMygoRealTodoProjection lets the mock adapter call the real
// todo_write tool: the Host appends todo/write and the app projects it.
func TestAgainstMygoRealTodoProjection(t *testing.T) {
	launch, root := realMygoLaunch(t, true)
	updates := make(chan func(), 512)
	application := New(Options{
		Launch: launch, CWD: root, Provider: "mygo-mock", Model: "todo-probe",
		Post: func(fn func()) { updates <- fn },
	})
	defer application.Shutdown(10 * time.Second)
	go application.Start()
	waitFor(t, application, updates, 90*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })

	application.Send("Track this work")
	waitFor(t, application, updates, 60*time.Second, func() bool {
		return application.Model().Phase == view.PhaseIdle && hasAssistantText(application.Model().Items, "todo probe complete")
	})

	tasks := application.Model().Tasks
	if len(tasks) != 2 {
		t.Fatalf("tasks = %+v", tasks)
	}
	if tasks[0].Content != "Inspect projection" || tasks[0].Status != "completed" || tasks[1].Content != "Project todo panel" || tasks[1].Status != "in_progress" {
		t.Fatalf("tasks = %+v", tasks)
	}
	application.Shutdown(10 * time.Second)
}

// TestAgainstMygoFakeHostSubagentStatus covers the Host's subagent/start and
// subagent/end forwarding plus the app's session-scoped status projection.
func TestAgainstMygoFakeHostSubagentStatus(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	fakeHost, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 128)
	application := New(Options{
		Launch: host.Config{Command: node, Args: []string{fakeHost, "--subagents"}},
		CWD:    t.TempDir(), Provider: "test", Model: "echo",
		Post: func(fn func()) { updates <- fn },
	})
	defer application.Shutdown(2 * time.Second)
	go application.Start()
	waitFor(t, application, updates, 20*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })

	application.Send("hi")
	waitFor(t, application, updates, 20*time.Second, func() bool {
		subagents := application.Model().Subagents
		return application.Model().Phase == view.PhaseIdle && hasAssistantText(application.Model().Items, "Echo: hi") &&
			len(subagents) == 1 && !subagents[0].Running && subagents[0].Reason == "completed"
	})
	subagent := application.Model().Subagents[0]
	if subagent.ID != "subagent-1" {
		t.Fatalf("subagent = %+v", subagent)
	}
	application.model.Draft = "parent draft"
	application.OpenSubagents()
	waitFor(t, application, updates, 20*time.Second, func() bool { return !application.model.Inspection.Busy })
	if len(application.model.Inspection.Children) != 1 {
		t.Fatalf("catalog: %+v", application.model.Inspection)
	}
	application.SelectSubagent(subagent.ID)
	waitFor(t, application, updates, 20*time.Second, func() bool { return !application.model.Inspection.Busy })
	if application.model.Inspection.Error != "" || application.model.Inspection.Content == nil || !hasAssistantText(application.model.Inspection.Content.Items, "Child inspection complete") || application.model.Inspection.EndReason != "completed" || application.model.Draft != "parent draft" {
		t.Fatalf("child inspection: %+v", application.model.Inspection)
	}
	application.CloseSubagents()
}

// TestAgainstMygoRealSubagentStatus spawns a real in-process child through the
// mock adapter and checks the Host's live status notifications end to end.
func TestAgainstMygoRealSubagentStatus(t *testing.T) {
	launch, root := realMygoLaunch(t, true)
	updates := make(chan func(), 1024)
	application := New(Options{
		Launch: launch, CWD: root, Provider: "mygo-mock", Model: "subagent-probe",
		Post: func(fn func()) { updates <- fn },
	})
	defer application.Shutdown(10 * time.Second)
	go application.Start()
	waitFor(t, application, updates, 90*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })

	application.Send("Start a child")
	// The delivered child result starts another parent turn, so wait on the
	// committed text and the child's terminal status instead of idleness.
	waitFor(t, application, updates, 120*time.Second, func() bool {
		return hasAssistantText(application.Model().Items, "subagent probe complete")
	})
	waitFor(t, application, updates, 60*time.Second, func() bool {
		subagents := application.Model().Subagents
		return len(subagents) == 1 && !subagents[0].Running && subagents[0].Reason == "completed"
	})
	subagent := application.Model().Subagents[0]
	if !safeSubagentID(subagent.ID) || subagent.Provider != "spawn" {
		t.Fatalf("subagent = %+v", subagent)
	}
	parentID := application.model.SessionID
	application.model.Draft = "keep parent draft"
	application.OpenSubagents()
	waitFor(t, application, updates, 30*time.Second, func() bool { return !application.model.Inspection.Busy })
	if application.model.Inspection.Error != "" || len(application.model.Inspection.Children) != 1 {
		t.Fatalf("real catalog: %+v", application.model.Inspection)
	}
	application.SelectSubagent(subagent.ID)
	waitFor(t, application, updates, 30*time.Second, func() bool { return !application.model.Inspection.Busy })
	if application.model.Inspection.Content == nil || !hasAssistantText(application.model.Inspection.Content.Items, "child ok") || application.model.Inspection.EndReason != "completed" || application.model.Draft != "keep parent draft" {
		t.Fatalf("real inspection: %+v", application.model.Inspection)
	}
	application.CloseSubagents()
	application.Shutdown(10 * time.Second)

	// A fresh Host must rediscover the persisted child, without live notices or
	// resuming the child Agent. Only the ordinary parent conversation is resumed.
	coldUpdates := make(chan func(), 1024)
	cold := New(Options{Launch: launch, CWD: root, Provider: "mygo-mock", Model: "subagent-probe", Post: func(fn func()) { coldUpdates <- fn }})
	defer cold.Shutdown(10 * time.Second)
	go cold.Start()
	waitFor(t, cold, coldUpdates, 90*time.Second, func() bool { return cold.model.Phase == view.PhaseIdle })
	cold.SwitchSession(parentID)
	waitFor(t, cold, coldUpdates, 90*time.Second, func() bool { return cold.model.SessionID == parentID && !cold.model.SessionBusy })
	cold.OpenSubagents()
	waitFor(t, cold, coldUpdates, 30*time.Second, func() bool { return !cold.model.Inspection.Busy })
	if len(cold.model.Inspection.Children) != 1 || len(cold.model.Subagents) != 0 {
		t.Fatalf("cold catalog: %+v", cold.model.Inspection)
	}
	cold.SelectSubagent(subagent.ID)
	waitFor(t, cold, coldUpdates, 30*time.Second, func() bool { return !cold.model.Inspection.Busy })
	if cold.model.Inspection.Content == nil || !hasAssistantText(cold.model.Inspection.Content.Items, "child ok") || cold.model.Inspection.EndReason != "completed" {
		t.Fatalf("cold output: %+v", cold.model.Inspection)
	}
}
