package app

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"dsh-mygo/internal/host"
)

// hostInstance owns exactly one started Host process and its protocol client.
// It is never reactivated: Restart and Runtime switches create a new instance.
// closed makes repeated stop calls idempotent and keeps a late stop from
// touching the process chosen by a newer generation.
type hostInstance struct {
	generation uint64
	client     rpcClient
	supervisor *host.Supervisor
	closed     atomic.Bool
}

// stop closes the instance's client and process exactly once. The process gets
// its own bounded stop deadline after the protocol client is closed.
func (h *hostInstance) stop(timeout time.Duration) {
	if h == nil || !h.closed.CompareAndSwap(false, true) {
		return
	}
	if h.client != nil {
		h.client.Close()
	}
	if h.supervisor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		_ = h.supervisor.Stop(ctx)
		cancel()
	}
}

// currentHost returns the active instance, or nil when none is installed.
func (a *App) currentHost() *hostInstance {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.host == nil || a.host.closed.Load() {
		return nil
	}
	return a.host
}

// installHost adopts a newly started instance. Older generations cannot
// replace a newer one, and shutdown rejects the install.
func (a *App) installHost(instance *hostInstance) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown.Load() || a.host != nil && a.host.generation >= instance.generation {
		return false
	}
	a.host = instance
	a.lastHost = nil
	return true
}

// detachHost removes the active instance before its asynchronous stop so its
// notifications and callbacks are rejected immediately.
func (a *App) detachHost() *hostInstance {
	a.mu.Lock()
	defer a.mu.Unlock()
	instance := a.host
	a.host = nil
	return instance
}

func (a *App) isCurrentHost(instance *hostInstance) bool {
	return a.currentHost() == instance
}

// currentSupervisor returns the active process, or the last stopped one for
// read-only diagnostics (PID and bounded stderr). It never returns a client.
func (a *App) currentSupervisor() *host.Supervisor {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.host != nil {
		return a.host.supervisor
	}
	if a.lastHost != nil {
		return a.lastHost.supervisor
	}
	return nil
}

func (a *App) currentClient() rpcClient {
	instance := a.currentHost()
	if instance == nil {
		return nil
	}
	return instance.client
}

// stopHostInstance closes exactly the captured generation. A slow stop cannot
// close the instance that a later start installed.
func (a *App) stopHostInstance(instance *hostInstance) {
	if instance == nil {
		return
	}
	a.mu.Lock()
	if a.host == instance {
		a.host = nil
	}
	a.lastHost = instance
	a.mu.Unlock()
	instance.stop(stopTimeout)
}

// stopHost stops the currently active instance, if any.
func (a *App) stopHost() {
	a.stopHostInstance(a.detachHost())
}

// forwardHostNotification keeps the notification queue ordered while rejecting
// frames from a generation that is no longer current.
func (a *App) forwardHostNotification(instance *hostInstance, incoming chan hostNotification, method string, params json.RawMessage) {
	if !a.isCurrentHost(instance) {
		return
	}
	select {
	case incoming <- hostNotification{method: method, params: params}:
	case <-a.stopped:
	}
}
