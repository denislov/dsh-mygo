package app

import (
	"bufio"
	"net"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/view"
)

func TestAgainstMygoResync(t *testing.T) {
	for _, mode := range []string{"fake", "real", "approval", "question", "stream-cancel", "baseline-complete", "baseline-cancel"} {
		t.Run(mode, func(t *testing.T) {
			var launch host.Config
			var root string
			provider, model := "mygo-mock", "echo"
			if mode == "fake" {
				node, err := exec.LookPath("node")
				if err != nil {
					t.Skip("node unavailable")
				}
				fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
				if err != nil {
					t.Fatal(err)
				}
				launch = host.Config{Command: node, Args: []string{fixture, "--sessions", "--event-gap"}}
				root = t.TempDir()
				provider = "test"
			} else {
				launch, root = realMygoLaunch(t, true)
				filter, err := filepath.Abs(filepath.Join("..", "..", "testdata", "drop-session-event.mjs"))
				if err != nil {
					t.Fatal(err)
				}
				prefix := []string{filter}
				if mode == "baseline-complete" || mode == "baseline-cancel" {
					prefix = append(prefix, "--stream-gap")
				}
				launch.Args = append(append(prefix, launch.Command), launch.Args...)
				if mode == "approval" {
					model = "approval-probe"
				}
				if mode == "question" {
					model = "question-probe"
				}
				if mode == "stream-cancel" || mode == "baseline-complete" || mode == "baseline-cancel" {
					model = "stream-probe"
				}
			}
			var connections chan net.Conn
			if mode == "stream-cancel" || mode == "baseline-complete" || mode == "baseline-cancel" {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				connections = make(chan net.Conn, 1)
				go func() {
					connection, err := listener.Accept()
					if err == nil {
						connections <- connection
					}
				}()
				launch.Env = append(launch.Env, "DSH_MYGO_MOCK_CONTROL="+listener.Addr().String())
			}
			updates := make(chan func(), 512)
			a := New(Options{Launch: launch, CWD: root, Provider: provider, Model: model, Post: func(fn func()) { updates <- fn }})
			defer a.Shutdown(10 * time.Second)
			go a.Start()
			waitFor(t, a, updates, 90*time.Second, func() bool { return a.model.Phase == view.PhaseIdle })
			if !a.model.CanResync {
				t.Fatal("live history was not negotiated")
			}
			id, pid := a.sessionID, a.supervisor.PID()
			a.Send("recover this prompt")
			a.model.Draft = "next draft"
			sawResync := false
			settled := func() bool {
				if a.model.Phase == view.PhaseResyncing {
					sawResync = true
				}
				if mode == "approval" {
					return sawResync && a.resync == nil && len(a.model.Approvals) > 0
				}
				if mode == "question" {
					return sawResync && a.resync == nil && len(a.model.Questions) > 0
				}
				if mode == "stream-cancel" {
					return sawResync && a.resync == nil && a.model.Phase == view.PhaseRunning
				}
				if mode == "baseline-complete" || mode == "baseline-cancel" {
					return sawResync && a.resync == nil && hasStreamingText(a.model.Items, "Live prefix")
				}
				return sawResync && a.resync == nil && a.model.Phase == view.PhaseIdle && assistantAnswers(a) == 1
			}
			waitFor(t, a, updates, 30*time.Second, settled)
			switch mode {
			case "approval":
				a.RespondApproval(a.model.Approvals[0].RequestID, "deny")
			case "question":
				a.RespondQuestion(a.model.Questions[0].RequestID, true)
			case "stream-cancel", "baseline-complete", "baseline-cancel":
				var connection net.Conn
				select {
				case connection = <-connections:
				case <-time.After(5 * time.Second):
					t.Fatal("stream did not pause")
				}
				defer connection.Close()
				_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
				if line, err := bufio.NewReader(connection).ReadString('\n'); err != nil || line != "paused\n" {
					t.Fatal("stream pause missing")
				}
				if mode == "baseline-complete" {
					if _, err := connection.Write([]byte("continue\n")); err != nil {
						t.Fatal(err)
					}
				} else {
					a.Cancel()
				}
			}
			waitFor(t, a, updates, 30*time.Second, func() bool { return a.model.Phase == view.PhaseIdle })
			if mode == "baseline-complete" || mode == "baseline-cancel" {
				assertOneSettledAssistant(t, a)
				if mode == "baseline-complete" && !hasAssistantText(a.model.Items, "Live prefix complete") {
					t.Fatal("baseline did not continue into committed text")
				}
			}
			users := 0
			for _, item := range a.model.Items {
				if item.Kind == projection.KindUser && item.Text == "recover this prompt" {
					users++
				}
				if item.State == "streaming" {
					t.Fatal("settled resync retained a stream")
				}
			}
			if users != 1 || a.model.Draft != "next draft" || a.sessionID != id || a.supervisor.PID() != pid || a.hostGeneration.Load() != 1 {
				t.Fatal("resync duplicated history, lost a draft or restarted the Host")
			}
			before := assistantAnswers(a)
			a.Send("continue after resync")
			waitFor(t, a, updates, 30*time.Second, func() bool { return a.model.Phase == view.PhaseIdle && assistantAnswers(a) > before })
		})
	}
}
