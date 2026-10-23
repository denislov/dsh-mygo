package app

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/view"
)

// TestAgainstMygoFakeHostUsageAndContext drives the fake host's --usage events
// through the app: step accounting and route capacity must land on the model.
func TestAgainstMygoFakeHostUsageAndContext(t *testing.T) {
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
		Launch: host.Config{Command: node, Args: []string{fakeHost, "--usage"}},
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

	usage := application.Model().Usage
	if !usage.HasUsage || usage.InputTokens != 1200 || usage.OutputTokens != 350 || usage.CacheReadTokens != 200 || usage.TotalTokens != 1550 {
		t.Fatalf("usage = %+v", usage)
	}
	context := application.Model().Context
	if context.Provider != "test" || context.Model != "echo" || context.Window != 128000 {
		t.Fatalf("context = %+v", context)
	}
}

// TestAgainstMygoRealUsageContext checks the real composition: the mock adapter
// reports one token each way, and the Host forwards the route capacity event.
func TestAgainstMygoRealUsageContext(t *testing.T) {
	launch, root := realMygoLaunch(t, true)
	updates := make(chan func(), 512)
	application := New(Options{
		Launch: launch, CWD: root, Provider: "mygo-mock", Model: "echo",
		Post: func(fn func()) { updates <- fn },
	})
	defer application.Shutdown(10 * time.Second)
	go application.Start()
	waitFor(t, application, updates, 90*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })

	application.Send("Reply with exactly: pong")
	waitFor(t, application, updates, 60*time.Second, func() bool {
		return application.Model().Phase == view.PhaseIdle && hasAssistantText(application.Model().Items, "pong")
	})

	usage := application.Model().Usage
	if !usage.HasUsage || usage.InputTokens != 1 || usage.OutputTokens != 1 || usage.TotalTokens != 2 {
		t.Fatalf("usage = %+v", usage)
	}
	context := application.Model().Context
	if context.Provider != "mygo-mock" || context.Model != "echo" {
		t.Fatalf("context = %+v", context)
	}
	// The fixture model declares no context window; the UI must not invent one.
	if context.Window != 0 {
		t.Fatalf("context = %+v", context)
	}
	application.Shutdown(10 * time.Second)
}
