// Package host starts and owns the DeepSeek Harness Node process.
package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"sync"
	"time"
)

// DefaultStderrLimit bounds the retained tail of the host's stderr.
const DefaultStderrLimit = 64 << 10

// postKillWait is the time allowed for the process to disappear after a kill.
// A variable so tests can shorten it.
var postKillWait = 5 * time.Second

// Config describes one host process invocation.
type Config struct {
	// Command is the executable to start.
	Command string
	// Args are passed to Command unchanged.
	Args []string
	// Dir is the working directory; empty inherits the current directory.
	Dir string
	// Env is the complete environment; nil inherits the current environment.
	Env []string
	// StderrLimit bounds the retained stderr tail in bytes.
	StderrLimit int
}

// Supervisor owns one host process and its pipes. Start attaches the reader
// goroutines; the caller must call Stop or Wait before dropping it.
type Supervisor struct {
	config Config

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	stderrMu   sync.Mutex
	stderrTail []byte

	done    chan struct{}
	waitErr error

	stopOnce sync.Once
	stopErr  error

	job *processJob
}

// Start starts the host. It returns after the process is spawned; protocol
// readiness is the caller's initialize handshake, not Start.
func Start(ctx context.Context, config Config) (*Supervisor, error) {
	if config.Command == "" {
		return nil, errors.New("host: empty command")
	}
	if config.StderrLimit <= 0 {
		config.StderrLimit = DefaultStderrLimit
	}
	cmd := exec.Command(config.Command, config.Args...)
	cmd.Dir = config.Dir
	cmd.Env = config.Env
	configureCommand(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("host: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("host: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("host: stderr pipe: %w", err)
	}
	closePipes := func() {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
	}
	jobs, err := newProcessJob()
	if err != nil {
		closePipes()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		jobs.close()
		closePipes()
		return nil, fmt.Errorf("host: start %q: %w", config.Command, err)
	}
	if err := jobs.assign(cmd.Process); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		jobs.close()
		closePipes()
		return nil, err
	}

	supervisor := &Supervisor{
		config: config,
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		done:   make(chan struct{}),
		job:    jobs,
	}
	go supervisor.readStderr()
	go func() {
		supervisor.waitErr = cmd.Wait()
		// The host is gone; close the job so any descendants it left behind
		// cannot outlive this supervisor.
		supervisor.job.close()
		close(supervisor.done)
	}()
	go func() {
		select {
		case <-ctx.Done():
			// The caller's context owns the process lifetime; Stop closes stdin
			// and escalates if the host ignores EOF. Bound the graceful phase so
			// an unresponsive host cannot block this goroutine forever.
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = supervisor.Stop(stopCtx)
		case <-supervisor.done:
		}
	}()
	return supervisor, nil
}

// Stdin is the host's stdin; the JSON-RPC client writes frames there.
func (s *Supervisor) Stdin() io.Writer { return s.stdin }

// Stdout is the host's stdout; the JSON-RPC client reads frames from there.
func (s *Supervisor) Stdout() io.Reader { return s.stdout }

// PID returns the process id, or 0 when the process has not started.
func (s *Supervisor) PID() int {
	if s.cmd == nil || s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

// Done is closed when the process exits.
func (s *Supervisor) Done() <-chan struct{} { return s.done }

// WaitError returns the error from cmd.Wait after Done is closed.
func (s *Supervisor) WaitError() error {
	<-s.done
	return s.waitErr
}

// StderrTail returns the retained tail of the host's stderr.
func (s *Supervisor) StderrTail() string {
	s.stderrMu.Lock()
	defer s.stderrMu.Unlock()
	return string(s.stderrTail)
}

// Stop closes stdin, waits for a graceful exit until ctx expires, then kills the
// process and waits up to postKillWait for it to disappear. It is idempotent.
func (s *Supervisor) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		// Closing the job is the final guarantee that neither the host nor any
		// plugin process survives this supervisor.
		defer s.job.close()
		_ = s.stdin.Close()
		select {
		case <-s.done:
			return
		case <-ctx.Done():
		}
		killProcess(s.cmd.Process)
		timer := time.NewTimer(postKillWait)
		defer timer.Stop()
		select {
		case <-s.done:
		case <-timer.C:
			s.job.close()
			s.stopErr = errors.New("host: process did not exit after kill")
		}
	})
	return s.stopErr
}

func (s *Supervisor) readStderr() {
	buffer := make([]byte, 32<<10)
	for {
		n, err := s.stderr.Read(buffer)
		if n > 0 {
			s.appendStderr(buffer[:n])
		}
		if err != nil {
			return
		}
	}
}

func (s *Supervisor) appendStderr(chunk []byte) {
	s.stderrMu.Lock()
	defer s.stderrMu.Unlock()
	s.stderrTail = append(s.stderrTail, chunk...)
	if overflow := len(s.stderrTail) - s.config.StderrLimit; overflow > 0 {
		s.stderrTail = slices.Clone(s.stderrTail[overflow:])
	}
}
