//go:build !windows

package host

import "os"

// processJob is a no-op outside Windows. POSIX process groups already cover
// the host and its descendants in killProcess.
type processJob struct{}

func newProcessJob() (*processJob, error) { return &processJob{}, nil }

func (j *processJob) assign(*os.Process) error { return nil }

func (j *processJob) close() {}
