package app

import (
	"bufio"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/view"
)

func hasStreamingText(items []projection.Item, text string) bool {
	for _, item := range items {
		if item.State == "streaming" && item.Text == text {
			return true
		}
	}
	return false
}

func TestAgainstMygoFakeStreaming(t *testing.T) {
	for _, mode := range []string{"complete", "cancel", "old-host"} {
		t.Run(mode, func(t *testing.T) {
			node, err := exec.LookPath("node")
			if err != nil {
				t.Skip("node is not installed")
			}
			fakeHost, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
			if err != nil {
				t.Fatal(err)
			}
			args := []string{fakeHost, "--stream"}
			if mode == "cancel" {
				args[1] = "--stream-wait"
			}
			if mode == "old-host" {
				args = append(args, "--no-stream")
			}
			updates := make(chan func(), 512)
			application := New(Options{Launch: host.Config{Command: node, Args: args}, CWD: t.TempDir(), Provider: "test", Model: "echo", Post: func(fn func()) { updates <- fn }})
			defer application.Shutdown(3 * time.Second)
			go application.Start()
			waitFor(t, application, updates, 10*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })
			if application.Model().CanStream != (mode != "old-host") {
				t.Fatal("stream capability mismatch")
			}
			application.Send("stream probe")
			if mode != "old-host" {
				waitFor(t, application, updates, 5*time.Second, func() bool { return hasStreamingText(application.Model().Items, "Live prefix") })
				for _, item := range application.transcript.Items() {
					if item.Kind == projection.KindAssistant {
						t.Fatal("prefix entered durable transcript")
					}
				}
			}
			application.Model().Draft = "next draft"
			if mode == "cancel" {
				application.Cancel()
			}
			waitFor(t, application, updates, 10*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })
			assertOneSettledAssistant(t, application)
			if application.Model().Draft != "next draft" {
				t.Fatal("stream discarded composer draft")
			}
			application.Send("next turn")
			waitFor(t, application, updates, 10*time.Second, func() bool {
				return application.Model().Phase == view.PhaseIdle && hasAssistantText(application.Model().Items, "Echo: next turn")
			})
		})
	}
}

func assertOneSettledAssistant(t *testing.T, application *App) {
	t.Helper()
	count := 0
	for _, item := range application.Model().Items {
		if item.State == "streaming" || strings.Contains(item.Text, "Hidden fixture reasoning") {
			t.Fatal("preview or reasoning leaked into settled UI")
		}
		if item.Kind == projection.KindAssistant {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("settled assistant rows = %d", count)
	}
}

func TestAgainstMygoRealStreaming(t *testing.T) {
	for _, action := range []string{"complete", "cancel", "shutdown", "markdown", "markdown-cancel"} {
		t.Run(action, func(t *testing.T) {
			modelName := "stream-probe"
			prefix := "Live prefix"
			completed := "Live prefix complete"
			if strings.HasPrefix(action, "markdown") {
				modelName = "markdown-probe"
				prefix = "# Native reply\n\n**Streaming Markdown**\n\n```go\n  fmt.Println(\"你好\")\n"
				completed = prefix + "```\n"
			}
			launch, _ := realMygoLaunch(t, true)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			connections := make(chan net.Conn, 1)
			go func() {
				connection, err := listener.Accept()
				if err == nil {
					connections <- connection
				}
			}()
			launch.Env = append(launch.Env, "DSH_MYGO_MOCK_CONTROL="+listener.Addr().String())
			updates := make(chan func(), 512)
			application := New(Options{Launch: launch, CWD: t.TempDir(), Provider: "mygo-mock", Model: modelName, Post: func(fn func()) { updates <- fn }})
			defer application.Shutdown(10 * time.Second)
			go application.Start()
			waitFor(t, application, updates, 90*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })
			sessionID := application.sessionID
			application.Send("Stream the probe")
			waitFor(t, application, updates, 30*time.Second, func() bool { return hasStreamingText(application.Model().Items, prefix) })
			for _, item := range application.Model().Items {
				if strings.Contains(item.Text, "Hidden fixture reasoning") {
					t.Fatal("reasoning became visible")
				}
			}
			for _, item := range application.transcript.Items() {
				if item.Kind == projection.KindAssistant {
					t.Fatal("prefix was already committed")
				}
			}
			var connection net.Conn
			select {
			case connection = <-connections:
			case <-time.After(5 * time.Second):
				t.Fatal("mock stream did not reach its pause")
			}
			defer connection.Close()
			_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
			if line, err := bufio.NewReader(connection).ReadString('\n'); err != nil || line != "paused\n" {
				t.Fatal("fixture pause signal missing")
			}
			application.Model().Draft = "next draft"
			switch action {
			case "complete", "markdown":
				if _, err := connection.Write([]byte("continue\n")); err != nil {
					t.Fatal(err)
				}
			case "cancel", "markdown-cancel":
				application.Cancel()
			case "shutdown":
				application.Shutdown(10 * time.Second)
				if err := application.supervisor.WaitError(); err != nil {
					t.Fatalf("stream shutdown: %v", err)
				}
				for len(updates) > 0 {
					(<-updates)()
				}
				if hasStreamingText(application.Model().Items, "Live prefix") {
					t.Fatal("shutdown retained preview")
				}
				return
			}
			waitFor(t, application, updates, 30*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })
			assertOneSettledAssistant(t, application)
			expected := completed
			if action == "cancel" || action == "markdown-cancel" {
				expected = prefix
			}
			if !hasAssistantText(application.Model().Items, expected) || application.Model().Draft != "next draft" {
				t.Fatal("stream settlement lost final content or draft")
			}
			application.Send("Reply with exactly: pong")
			waitFor(t, application, updates, 30*time.Second, func() bool {
				return application.Model().Phase == view.PhaseIdle && hasAssistantText(application.Model().Items, "pong")
			})
			if application.sessionID != sessionID {
				t.Fatal("stream replaced session")
			}
			application.Shutdown(10 * time.Second)
			if err := application.supervisor.WaitError(); err != nil {
				t.Fatalf("stream host shutdown: %v", err)
			}
		})
	}
}
