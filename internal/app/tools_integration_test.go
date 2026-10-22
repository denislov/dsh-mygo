package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/view"
)

func TestAgainstMygoToolDiffs(t *testing.T) {
	for _, realHost := range []bool{false, true} {
		name := "fake"
		if realHost {
			name = "real"
		}
		t.Run(name, func(t *testing.T) {
			var launch host.Config
			provider, modelName := "test", "echo"
			if realHost {
				launch, _ = realMygoLaunch(t, true)
				launch.Env = append(launch.Env, "DSH_PERMISSION_MODE=workspace-write")
				provider, modelName = "mygo-mock", "diff-probe"
			} else {
				node, err := exec.LookPath("node")
				if err != nil {
					t.Skip("node is not installed")
				}
				fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
				if err != nil {
					t.Fatal(err)
				}
				launch = host.Config{Command: node, Args: []string{fixture, "--tool-diff"}}
			}
			workspace := t.TempDir()
			updates := make(chan func(), 512)
			application := New(Options{Launch: launch, CWD: workspace, Provider: provider, Model: modelName, Post: func(fn func()) { updates <- fn }})
			defer application.Shutdown(10 * time.Second)
			go application.Start()
			waitFor(t, application, updates, 90*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })
			application.Send("Run the file diff probe")
			waitFor(t, application, updates, 30*time.Second, func() bool {
				if len(application.Model().Approvals) > 0 && !application.Model().Approvals[0].Responding {
					application.RespondApproval(application.Model().Approvals[0].RequestID, "allow")
				}
				return application.Model().Phase == view.PhaseIdle
			})
			var changes []projection.FileDiff
			created := false
			for _, item := range application.Model().Items {
				if item.Kind == projection.KindTool {
					if item.IsError || item.Tool == nil {
						t.Fatal("tool failed or lost details")
					}
					changes = append(changes, item.Tool.Diffs...)
					created = created || item.Tool.DiffNotice == "Created file (no textual hunks)."
				}
			}
			if realHost {
				content, err := os.ReadFile(filepath.Join(workspace, "diff-probe.txt"))
				if err != nil || string(content) != "after\n" {
					t.Fatal("real filesystem tools did not perform the expected edit")
				}
				if !created || len(changes) != 1 || changes[0].OldText != "before" || changes[0].NewText != "after" {
					t.Fatal("real diff metadata changed")
				}
			} else if len(changes) != 2 || changes[0].OldText != "before" || changes[0].NewText != "after" || !changes[1].OldTextMissing {
				t.Fatal("fake diff metadata changed")
			}
			application.Shutdown(10 * time.Second)
			if err := application.supervisor.WaitError(); err != nil {
				t.Fatalf("tool diff shutdown: %v", err)
			}
		})
	}
}
