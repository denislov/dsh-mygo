package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/view"
)

func TestAgainstMygoFakeApprovals(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	fakeHost, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, decision := range []string{"allow", "deny", "cancel"} {
		t.Run(decision, func(t *testing.T) {
			updates := make(chan func(), 128)
			application := New(Options{Launch: host.Config{Command: node, Args: []string{fakeHost, "--approval"}}, CWD: t.TempDir(), Provider: "test", Model: "echo", Post: func(fn func()) { updates <- fn }})
			defer application.Shutdown(2 * time.Second)
			go application.Start()
			waitFor(t, application, updates, 10*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })
			application.Send("approval probe")
			waitFor(t, application, updates, 10*time.Second, func() bool { return len(application.Model().Approvals) == 1 })
			request := application.Model().Approvals[0]
			if request.External || request.ToolName != "write" || request.Arguments == "" {
				t.Fatal("fake approval lost its tool association")
			}
			application.Model().Draft = "next draft"
			if decision == "cancel" {
				application.Cancel()
			} else {
				application.RespondApproval(request.RequestID, decision)
			}
			waitFor(t, application, updates, 10*time.Second, func() bool {
				return application.Model().Phase == view.PhaseIdle && len(application.Model().Approvals) == 0
			})
			if application.Model().Draft != "next draft" {
				t.Fatal("approval discarded the draft")
			}
			if decision != "cancel" {
				expected := "Denied write"
				if decision == "allow" {
					expected = "Approved write"
				}
				if !hasAssistantText(application.Model().Items, expected) {
					t.Fatal("approval decision did not reach the tool")
				}
			}
			application.Send("next turn")
			waitFor(t, application, updates, 10*time.Second, func() bool {
				return application.Model().Phase == view.PhaseIdle && hasAssistantText(application.Model().Items, "Echo: next turn")
			})
		})
	}
}

func TestAgainstMygoRealApprovals(t *testing.T) {
	for _, decision := range []string{"allow", "deny", "cancel", "shutdown"} {
		t.Run(decision, func(t *testing.T) {
			launch, _ := realMygoLaunch(t, true)
			launch.Env = append(launch.Env, "DSH_PERMISSION_MODE=workspace-write")
			workspace := t.TempDir()
			updates := make(chan func(), 512)
			application := New(Options{Launch: launch, CWD: workspace, Provider: "mygo-mock", Model: "approval-probe", Post: func(fn func()) { updates <- fn }})
			defer application.Shutdown(10 * time.Second)
			go application.Start()
			waitFor(t, application, updates, 90*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })
			sessionID := application.sessionID
			application.Send("Run the approval probe")
			waitFor(t, application, updates, 30*time.Second, func() bool { return len(application.Model().Approvals) == 1 })
			request := application.Model().Approvals[0]
			if request.External || request.ToolName != "mygo_approval_probe" || request.CallID == "" || request.Arguments != `{"file_path":"approval-marker.txt"}` {
				t.Fatal("real approval was not anchored to its committed tool call")
			}
			if decision == "shutdown" {
				application.Shutdown(10 * time.Second)
				if err := application.supervisor.WaitError(); err != nil {
					t.Fatalf("pending approval shutdown: %v", err)
				}
				for len(updates) > 0 {
					(<-updates)()
				}
				if len(application.Model().Approvals) != 0 {
					t.Fatal("shutdown left a pending approval")
				}
				if _, err := os.Stat(filepath.Join(workspace, "approval-marker.txt")); !os.IsNotExist(err) {
					t.Fatal("shutdown executed an unapproved tool")
				}
				return
			}
			if decision == "cancel" {
				application.Cancel()
			} else {
				application.RespondApproval(request.RequestID, decision)
			}
			waitFor(t, application, updates, 15*time.Second, func() bool {
				return application.Model().Phase == view.PhaseIdle && len(application.Model().Approvals) == 0
			})
			_, err := os.Stat(filepath.Join(workspace, "approval-marker.txt"))
			if decision == "allow" && err != nil {
				t.Fatal("approved tool did not execute")
			}
			if decision != "allow" && !os.IsNotExist(err) {
				t.Fatal("unapproved tool executed")
			}
			application.Send("Reply with exactly: pong")
			waitFor(t, application, updates, 30*time.Second, func() bool {
				return application.Model().Phase == view.PhaseIdle && hasAssistantText(application.Model().Items, "pong")
			})
			if application.sessionID != sessionID {
				t.Fatal("approval replaced the session")
			}
			application.Shutdown(10 * time.Second)
			if err := application.supervisor.WaitError(); err != nil {
				t.Fatalf("approval host shutdown: %v", err)
			}
			t.Logf("%s: real approval service and tool execution verified", decision)
		})
	}
}
