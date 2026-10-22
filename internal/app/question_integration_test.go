package app

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/view"
)

func TestAgainstMygoQuestions(t *testing.T) {
	for _, realHost := range []bool{false, true} {
		name := "fake"
		if realHost {
			name = "real"
		}
		t.Run(name, func(t *testing.T) {
			for _, action := range []string{"answer", "cancel", "stop", "shutdown"} {
				t.Run(action, func(t *testing.T) {
					var launch host.Config
					modelName := "echo"
					provider := "test"
					if realHost {
						launch, _ = realMygoLaunch(t, true)
						modelName = "question-probe"
						provider = "mygo-mock"
					} else {
						node, err := exec.LookPath("node")
						if err != nil {
							t.Skip("node is not installed")
						}
						fakeHost, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
						if err != nil {
							t.Fatal(err)
						}
						launch = host.Config{Command: node, Args: []string{fakeHost, "--question"}}
					}
					updates := make(chan func(), 512)
					application := New(Options{Launch: launch, CWD: t.TempDir(), Provider: provider, Model: modelName, Post: func(fn func()) { updates <- fn }})
					defer application.Shutdown(10 * time.Second)
					go application.Start()
					waitFor(t, application, updates, 90*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })
					sessionID := application.sessionID
					application.Send("Ask the question probe")
					waitFor(t, application, updates, 30*time.Second, func() bool { return len(application.Model().Questions) == 1 })
					request := application.Model().Questions[0]
					if len(request.Questions) != 3 || !request.Questions[1].MultiSelect {
						t.Fatal("question batch lost its shape")
					}
					application.Model().Draft = "next draft"
					switch action {
					case "answer":
						request.Drafts[0].Selected[0] = true
						request.Drafts[1].Selected[0] = true
						request.Drafts[1].Selected[1] = true
						request.Drafts[1].Custom = "Extra"
						request.Drafts[2].Custom = "Hello"
						application.RespondQuestion(request.RequestID, false)
					case "cancel":
						application.RespondQuestion(request.RequestID, true)
					case "stop":
						application.Cancel()
					case "shutdown":
						application.Shutdown(10 * time.Second)
						if err := application.supervisor.WaitError(); err != nil {
							t.Fatalf("pending question shutdown: %v", err)
						}
						for len(updates) > 0 {
							(<-updates)()
						}
						if len(application.Model().Questions) != 0 {
							t.Fatal("shutdown left a question")
						}
						return
					}
					waitFor(t, application, updates, 30*time.Second, func() bool {
						return application.Model().Phase == view.PhaseIdle && len(application.Model().Questions) == 0
					})
					if application.Model().Draft != "next draft" {
						t.Fatal("question discarded composer draft")
					}
					if action == "answer" {
						found := false
						for _, item := range application.Model().Items {
							if strings.Contains(item.Text, `"selected":["Logs","Tests"]`) && strings.Contains(item.Text, `"custom":"Hello"`) {
								found = true
							}
						}
						if !found {
							t.Fatal("structured answers did not reach the tool result")
						}
					}
					application.Send("Reply with exactly: pong")
					expected := "Echo: Reply with exactly: pong"
					if realHost {
						expected = "pong"
					}
					waitFor(t, application, updates, 30*time.Second, func() bool {
						return application.Model().Phase == view.PhaseIdle && hasAssistantText(application.Model().Items, expected)
					})
					if application.sessionID != sessionID {
						t.Fatal("question replaced the session")
					}
					application.Shutdown(10 * time.Second)
					if err := application.supervisor.WaitError(); err != nil {
						t.Fatalf("question host shutdown: %v", err)
					}
				})
			}
		})
	}
}
