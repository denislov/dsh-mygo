//go:build windows

package host

import (
	"os"
	"os/exec"
	"syscall"
)

// CREATE_NEW_PROCESS_GROUP gives the child its own console group. A full Job
// Object teardown belongs to the packaged host supervisor; P0 keeps the
// process handle only.
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
