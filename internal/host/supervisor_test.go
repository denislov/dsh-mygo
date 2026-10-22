package host

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		helperProcess()
		return
	}
	os.Exit(m.Run())
}

// TestHelperProcess is never run directly; TestMain handles the helper mode.
func TestHelperProcess(t *testing.T) {}

func helperProcess() {
	fmt.Fprintln(os.Stderr, "helper-ready")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		switch line {
		case "kill":
			select {}
		case "exit":
			os.Exit(0)
		}
		params, _ := json.Marshal(map[string]any{"line": line})
		fmt.Fprintf(os.Stdout, "{\"jsonrpc\":\"2.0\",\"method\":\"echo\",\"params\":%s}\n", params)
	}
	os.Exit(0)
}

func helperConfig() Config {
	return Config{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     append(os.Environ(), "GO_WANT_HELPER_PROCESS=1"),
	}
}

func TestStartEchoAndStop(t *testing.T) {
	supervisor, err := Start(context.Background(), helperConfig())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		_ = supervisor.Stop(context.Background())
	}()

	if _, err := fmt.Fprintln(supervisor.Stdin(), "hello"); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	reader := bufio.NewReader(supervisor.Stdout())
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	var notification struct {
		Method string `json:"method"`
		Params struct {
			Line string `json:"line"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(line), &notification); err != nil {
		t.Fatalf("decode notification: %v", err)
	}
	if notification.Method != "echo" || notification.Params.Line != "hello" {
		t.Fatalf("notification = %+v", notification)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := supervisor.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !strings.Contains(supervisor.StderrTail(), "helper-ready") {
		t.Fatalf("stderr tail = %q", supervisor.StderrTail())
	}
}

func TestStopKillsUnresponsiveProcess(t *testing.T) {
	previous := postKillWait
	postKillWait = 2 * time.Second
	defer func() { postKillWait = previous }()

	supervisor, err := Start(context.Background(), helperConfig())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := fmt.Fprintln(supervisor.Stdin(), "kill"); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := supervisor.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-supervisor.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("process still running after kill")
	}
}

func TestAppendStderrKeepsTail(t *testing.T) {
	supervisor := &Supervisor{config: Config{StderrLimit: 16}}
	supervisor.appendStderr([]byte("0123456789"))
	supervisor.appendStderr([]byte("abcdefghij"))
	if got := supervisor.StderrTail(); got != "456789abcdefghij" {
		t.Fatalf("stderr tail = %q", got)
	}
}
