package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/channel"
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
		Launch:      host.Config{Command: node, Args: []string{fakeHost}},
		CWD:         t.TempDir(),
		Provider:    "deepseek-official",
		Model:       "deepseek-flash",
		SessionID:   fmt.Sprintf("test-%d", time.Now().UnixNano()),
		SDKProtocol: true,
		Post:        func(fn func()) { updates <- fn },
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
		Launch:      launch,
		CWD:         dshDir,
		Provider:    "deepseek-official",
		Model:       "deepseek-flash",
		SessionID:   fmt.Sprintf("real-%d", time.Now().UnixNano()),
		SDKProtocol: true,
		Post:        func(fn func()) { updates <- fn },
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

func TestAgainstMygoFakeHost(t *testing.T) {
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
		Launch: host.Config{Command: node, Args: []string{fakeHost}},
		CWD:    t.TempDir(), Provider: "test", Model: "echo",
		Post: func(fn func()) { updates <- fn },
	})
	defer application.Shutdown(2 * time.Second)
	go application.Start()
	waitFor(t, application, updates, 10*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })
	if !validSessionID(application.sessionID) {
		t.Fatalf("session id = %q", application.sessionID)
	}
	for _, prompt := range []string{"hello", "second turn"} {
		application.Send(prompt)
		waitFor(t, application, updates, 10*time.Second, func() bool {
			return application.Model().Phase == view.PhaseIdle && hasAssistantText(application.Model().Items, "Echo: "+prompt)
		})
	}
}

func TestMygoRejectsInvalidHost(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	fakeHost, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"--bad-version", "--bad-session"} {
		t.Run(mode, func(t *testing.T) {
			updates := make(chan func(), 128)
			application := New(Options{
				Launch: host.Config{Command: node, Args: []string{fakeHost, mode, "--stderr-secret"}},
				CWD:    t.TempDir(), Provider: "test", Model: "echo",
				Post: func(fn func()) { updates <- fn },
			})
			defer application.Shutdown(2 * time.Second)
			go application.Start()
			waitFor(t, application, updates, 10*time.Second, func() bool { return application.Model().Phase == view.PhaseFailed })
			application.Send("must not send")
			if len(application.Model().Items) != 0 {
				t.Fatal("invalid host accepted a prompt")
			}
			if strings.Contains(application.Model().ErrorDetail, "fake-private-token") {
				t.Fatal("raw host stderr reached the UI")
			}
		})
	}
}

func realMygoLaunch(t *testing.T, mockModel bool) (host.Config, string) {
	t.Helper()
	dshDir := os.Getenv("DSH_MYGO_DSH_DIR")
	if dshDir == "" {
		t.Skip("DSH_MYGO_DSH_DIR is not set")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dshHome := t.TempDir()
	setup := exec.Command(node, filepath.Join(root, "resources", "host", "setup-profile.mjs"), "--dsh-home", dshHome)
	if output, err := setup.CombinedOutput(); err != nil {
		t.Fatalf("setup profile: %v\n%s", err, output)
	}
	launch, err := host.ResolveLaunch(host.LaunchOptions{DshDir: dshDir, Profile: "mygo", CWD: root, DSHHome: dshHome})
	if err != nil {
		t.Fatal(err)
	}
	if mockModel {
		fixture := filepath.Join(root, "testdata", "mygo-mock-llm.mjs")
		patch, err := json.Marshal([]any{map[string]any{"insert": []any{map[string]any{"id": "mygo-mock-llm", "name": filepath.ToSlash(fixture)}}}})
		if err != nil {
			t.Fatal(err)
		}
		patchPath := filepath.Join(dshHome, "mock.patch.yml")
		if err := os.WriteFile(patchPath, patch, 0o600); err != nil {
			t.Fatal(err)
		}
		launch.Args = append(launch.Args, "--patch", patchPath)
	}
	return launch, root
}

func TestAgainstMygoRealComposition(t *testing.T) {
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
	waitFor(t, application, updates, 30*time.Second, func() bool {
		return application.Model().Phase == view.PhaseIdle && hasAssistantText(application.Model().Items, "pong")
	})
	if !strings.Contains(application.Model().Detail, "DSH 0.2.1-rc.2") {
		t.Fatalf("host identity = %q", application.Model().Detail)
	}
	application.Shutdown(10 * time.Second)
	if err := application.supervisor.WaitError(); err != nil {
		t.Fatalf("host shutdown: %v; stderr: %s", err, application.supervisor.StderrTail())
	}
}

func TestMygoRealProtocolLifecycle(t *testing.T) {
	launch, _ := realMygoLaunch(t, true)
	for _, mode := range []string{"wrong-version", "wrong-release", "invalid-frame", "shutdown-before-initialize", "stdin-eof"} {
		t.Run(mode, func(t *testing.T) {
			supervisor, err := host.Start(context.Background(), launch)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = supervisor.Stop(stopContext)
			}()
			client := channel.NewClient(supervisor.Stdout(), supervisor.Stdin(), 0)
			defer client.Close()
			go func() { _ = client.Run() }()
			requestContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			switch mode {
			case "invalid-frame":
				if _, err := supervisor.Stdin().Write([]byte("{}\n")); err != nil {
					t.Fatal(err)
				}
			case "stdin-eof":
				if err := supervisor.Stop(requestContext); err != nil {
					t.Fatal(err)
				}
			case "shutdown-before-initialize":
				if _, err := client.Call(requestContext, "shutdown", map[string]any{}); err != nil {
					t.Fatal(err)
				}
			default:
				version := channel.ProtocolVersion
				release := channel.ReleaseVersion
				kind := "protocol/version-mismatch"
				if mode == "wrong-version" {
					version++
				} else {
					release = "9.0.0"
					kind = "protocol/release-mismatch"
				}
				_, err := client.Call(requestContext, "initialize", map[string]any{
					"protocolVersion": version, "capabilities": []string{},
					"client": map[string]string{"name": "dsh-mygo", "version": release, "platform": "test", "locale": "zh-CN"},
				})
				var rpcError *channel.RPCError
				if !errors.As(err, &rpcError) {
					t.Fatalf("initialize error = %v", err)
				}
				var detail struct {
					Kind string `json:"kind"`
				}
				if json.Unmarshal(rpcError.Data, &detail) != nil || detail.Kind != kind {
					t.Fatalf("error kind = %q", detail.Kind)
				}
			}
			select {
			case <-supervisor.Done():
			case <-requestContext.Done():
				t.Fatal("host did not exit within its deadline")
			}
			wantFailure := mode == "wrong-version" || mode == "wrong-release" || mode == "invalid-frame"
			if (supervisor.WaitError() != nil) != wantFailure {
				t.Fatalf("host exit = %v", supervisor.WaitError())
			}
		})
	}
}

func TestAgainstMygoRealModel(t *testing.T) {
	if strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")) == "" {
		t.Skip("DEEPSEEK_API_KEY is not set")
	}
	launch, _ := realMygoLaunch(t, false)
	updates := make(chan func(), 512)
	application := New(Options{
		Launch: launch, CWD: t.TempDir(), Provider: "deepseek-official", Model: "deepseek-flash",
		Post: func(fn func()) { updates <- fn },
	})
	defer application.Shutdown(10 * time.Second)
	go application.Start()
	waitFor(t, application, updates, 90*time.Second, func() bool {
		return application.Model().Phase == view.PhaseIdle
	})
	application.Send("Do not use any tools. Reply with exactly: pong")
	waitFor(t, application, updates, 3*time.Minute, func() bool {
		return application.Model().Phase == view.PhaseIdle && len(application.Model().Items) > 0
	})
	foundAssistant := false
	for _, item := range application.Model().Items {
		if item.Kind == projection.KindAssistant {
			if strings.TrimSpace(item.Text) != "pong" {
				t.Fatal("real model did not return the exact requested reply")
			}
			foundAssistant = true
		}
	}
	if !foundAssistant {
		t.Fatal("real model turn ended without an assistant reply")
	}
	application.Shutdown(10 * time.Second)
	if err := application.supervisor.WaitError(); err != nil {
		t.Fatalf("real model host shutdown: %v", err)
	}
	t.Log("deepseek-official/deepseek-flash replied with exactly pong; host exited successfully")
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
			if application.Model().Phase == view.PhaseFailed && !done() {
				t.Fatalf("host failed: %s: %s", application.Model().Error, application.Model().ErrorDetail)
			}
		case <-deadline.C:
			t.Fatalf("timed out in phase %v with %d items", application.Model().Phase, len(application.Model().Items))
		}
	}
}
