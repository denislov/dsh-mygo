//go:build windows

package host

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestStopKillsProcessTree(t *testing.T) {
	supervisor, err := Start(context.Background(), helperConfig())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		_ = supervisor.Stop(context.Background())
	}()

	childPID := spawnChild(t, supervisor)
	if _, err := fmt.Fprintln(supervisor.Stdin(), "kill"); err != nil {
		t.Fatalf("stop responding: %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := supervisor.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitProcessGone(t, supervisor.PID(), 5*time.Second)
	waitProcessGone(t, childPID, 5*time.Second)
}

func TestHostExitKillsProcessTree(t *testing.T) {
	supervisor, err := Start(context.Background(), helperConfig())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		_ = supervisor.Stop(context.Background())
	}()

	childPID := spawnChild(t, supervisor)
	if _, err := fmt.Fprintln(supervisor.Stdin(), "exit"); err != nil {
		t.Fatalf("exit host: %v", err)
	}
	select {
	case <-supervisor.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("host did not exit")
	}
	waitProcessGone(t, childPID, 5*time.Second)
}

func spawnChild(t *testing.T, supervisor *Supervisor) int {
	t.Helper()
	if _, err := fmt.Fprintln(supervisor.Stdin(), "spawn-child"); err != nil {
		t.Fatalf("spawn child: %v", err)
	}
	line, err := bufio.NewReader(supervisor.Stdout()).ReadString('\n')
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	if !strings.HasPrefix(line, "child=") {
		t.Fatalf("unexpected child line %q", line)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "child=")))
	if err != nil {
		t.Fatalf("child pid %q: %v", line, err)
	}
	if !processAlive(pid) {
		t.Fatalf("child %d is not running", pid)
	}
	return pid
}

func processAlive(pid int) bool {
	handle, err := windows.OpenProcess(
		windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(pid),
	)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	event, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return false
	}
	return event == uint32(windows.WAIT_TIMEOUT)
}

func waitProcessGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d is still running", pid)
}
