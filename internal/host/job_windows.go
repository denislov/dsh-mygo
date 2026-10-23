//go:build windows

package host

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processJob owns the Windows Job Object that contains the host and every
// process it starts. Closing the last handle kills the whole tree, so an app
// exit, crash, or forced termination cannot leave plugin processes behind.
type processJob struct {
	handle windows.Handle
	once   sync.Once
}

func newProcessJob() (*processJob, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("host: create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		handle,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("host: configure job object: %w", err)
	}
	return &processJob{handle: handle}, nil
}

func (j *processJob) assign(process *os.Process) error {
	if j == nil || process == nil {
		return nil
	}
	handle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(process.Pid),
	)
	if err != nil {
		return fmt.Errorf("host: open process %d: %w", process.Pid, err)
	}
	defer windows.CloseHandle(handle)
	if err := windows.AssignProcessToJobObject(j.handle, handle); err != nil {
		return fmt.Errorf("host: assign process %d to job: %w", process.Pid, err)
	}
	return nil
}

func (j *processJob) close() {
	if j == nil {
		return
	}
	j.once.Do(func() {
		if j.handle != 0 {
			_ = windows.CloseHandle(j.handle)
			j.handle = 0
		}
	})
}
