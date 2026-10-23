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
	application.Shutdown(10 * time.Second)
}
