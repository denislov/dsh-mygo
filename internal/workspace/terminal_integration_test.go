package workspace

import (
	"github.com/egoist/mygo/plugins/terminal"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNativeTerminalShellRoundTrip(t *testing.T) {
	if os.Getenv("DSH_MYGO_NATIVE_TERMINAL") != "1" {
		t.Skip("set DSH_MYGO_NATIVE_TERMINAL=1 for the native terminal library + PTY smoke")
	}
	command := []string{"sh", "-c", "printf 'mygo-terminal-roundtrip\\n'"}
	if runtime.GOOS == "windows" {
		command = []string{"powershell.exe", "-NoProfile", "-Command", "Write-Output mygo-terminal-roundtrip"}
	}
	term, err := terminal.New(terminal.Options{Command: command, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	select {
	case <-term.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("native shell did not exit")
	}
	if term.ExitCode() != 0 || !strings.Contains(term.Text(), "mygo-terminal-roundtrip") {
		t.Fatalf("exit=%d text=%q", term.ExitCode(), term.Text())
	}
}
