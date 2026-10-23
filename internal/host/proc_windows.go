//go:build windows

package host

import (
	"os"
	"os/exec"
	"syscall"
)

// CREATE_NEW_PROCESS_GROUP gives the child its own console group. The
// supervisor also assigns the host to a Job Object with kill-on-close, so
// the host and its descendants cannot outlive the app.
const createNewProcessGroup = 0x00000200

func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNewProcessGroup,
	}
}

func killProcess(process *os.Process) {
	if process == nil {
		return
	}
	_ = process.Kill()
}
