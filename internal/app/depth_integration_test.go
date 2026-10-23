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
