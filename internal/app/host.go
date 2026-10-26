package app

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"dsh-mygo/internal/host"
)

// hostInstance owns exactly one started Host process and its protocol client.
// It is never reactivated: Restart and Runtime switches create a new instance.
// closed rejects further notifications; stopOnce coordinates completion of
// repeated stops without touching the process chosen by a newer generation.
type hostInstance struct {
	generation uint64
	client     rpcClient
	supervisor *host.Supervisor
	closed     atomic.Bool
	stopOnce   sync.Once
}

// stop closes the instance's client and process exactly once. The process gets
// its own bounded stop deadline after the protocol client is closed.
func (h *hostInstance) stop(timeout time.Duration) {
	if h == nil {
		return
	}
	// Once also makes concurrent callers wait for the original stop. Restart
	// must not resume a session while its previous Host still owns the log.
	h.stopOnce.Do(func() {
		h.closed.Store(true)
		if h.client != nil {
			h.client.Close()
		}
		if h.supervisor != nil {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			_ = h.supervisor.Stop(ctx)
			cancel()
		}
	})
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

// detachHost removes the active instance so its notifications are rejected.
// It retains and returns the last instance even if cleanup already started:
// Restart, Runtime switching and Shutdown must wait for that stop to finish.
func (a *App) detachHost() *hostInstance {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.host != nil {
		a.lastHost = a.host
	}
	a.host = nil
	return a.lastHost
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
// close the instance that a later start installed. Asynchronous callers capture
// the instance before starting the goroutine, never inside it.
func (a *App) stopHostInstance(instance *hostInstance) {
	if instance == nil {
		return
	}
	a.mu.Lock()
	if a.host == instance {
		a.host = nil
	}
	if a.host == nil && (a.lastHost == nil || a.lastHost.generation <= instance.generation) {
		a.lastHost = instance
	}
	a.mu.Unlock()
	instance.stop(stopTimeout)
}

// stopHost stops the active instance or waits for the last instance's cleanup.
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
	case incoming <- hostNotification{host: instance, method: method, params: params}:
	case <-a.stopped:
	}
}

// processNotification is the single ordered commit boundary for host frames and
// main-thread callbacks. Host frames carry their generation; local callbacks
// have no host and survive a generation change.
func (a *App) processNotification(notification hostNotification) {
	if notification.host != nil && !a.isCurrentHost(notification.host) {
		return
	}
	if notification.apply != nil {
		notification.apply()
		return
	}
	a.handleNotificationFrom(notification.host, notification.method, notification.params)
}
