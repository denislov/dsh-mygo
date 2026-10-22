package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/view"
)

// TestAgainstFakeHost drives the whole path the window uses: spawn a host,
// initialize it, send a prompt, and project the committed turn. It skips when
// Node is not installed.
func TestAgainstFakeHost(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	fakeHost, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-sdk-host.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fakeHost); err != nil {
		t.Fatalf("fake host: %v", err)
	}

	updates := make(chan func(), 64)
	application := New(Options{
		Launch:    host.Config{Command: node, Args: []string{fakeHost}},
		CWD:       t.TempDir(),
		Provider:  "deepseek-official",
		Model:     "deepseek-flash",
		SessionID: fmt.Sprintf("test-%d", time.Now().UnixNano()),
		Post:      func(fn func()) { updates <- fn },
	})
	defer application.Shutdown(2 * time.Second)
	go application.Start()

	waitFor(t, application, updates, 10*time.Second, func() bool {
		return application.Model().Phase == view.PhaseIdle
	})

	application.Send("hello")
	waitFor(t, application, updates, 10*time.Second, func() bool {
		return application.Model().Phase == view.PhaseIdle && len(application.Model().Items) >= 3
	})

	items := application.Model().Items
	if items[0].Text != "hello" {
		t.Fatalf("first item = %+v", items[0])
	}
	foundAssistant := false
	for _, item := range items {
		if item.Kind == projection.KindAssistant {
			foundAssistant = item.Text == "Echo: hello"
		}
	}
	if !foundAssistant {
		t.Fatalf("items = %+v", items)
	}
}

// TestAgainstRealDSH runs one real model turn through the same app path when
// a key and a DSH source checkout are supplied. It skips otherwise, so the
// default test run needs neither.
func TestAgainstRealDSH(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("DEEPSEEK_API_KEY is not set")
	}
	dshDir := os.Getenv("DSH_MYGO_DSH_DIR")
	if dshDir == "" {
		t.Skip("DSH_MYGO_DSH_DIR is not set")
	}
	launch, err := host.ResolveLaunch(host.LaunchOptions{
		DshDir:  dshDir,
		Profile: "sdk",
		CWD:     dshDir,
		DSHHome: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	updates := make(chan func(), 64)
	application := New(Options{
		Launch:    launch,
		CWD:       dshDir,
		Provider:  "deepseek-official",
		Model:     "deepseek-flash",
		SessionID: fmt.Sprintf("real-%d", time.Now().UnixNano()),
		Post:      func(fn func()) { updates <- fn },
	})
	defer application.Shutdown(10 * time.Second)
	go application.Start()

	waitFor(t, application, updates, 90*time.Second, func() bool {
		return application.Model().Phase == view.PhaseIdle
	})

	application.Send("Reply with exactly: pong")
	waitFor(t, application, updates, 3*time.Minute, func() bool {
		return application.Model().Phase == view.PhaseIdle && hasAssistantText(application.Model().Items, "pong")
	})
}

func hasAssistantText(items []projection.Item, text string) bool {
	for _, item := range items {
		if item.Kind == projection.KindAssistant && strings.Contains(strings.ToLower(item.Text), strings.ToLower(text)) {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, application *App, updates <-chan func(), timeout time.Duration, done func() bool) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		if done() {
			return
		}
		select {
		case update := <-updates:
			update()
		case <-deadline.C:
			t.Fatalf("timed out in phase %v with %d items", application.Model().Phase, len(application.Model().Items))
		}
	}
}
