// Package app wires the MyGo UI model to the DeepSeek Harness host process.
// Host I/O and protocol reads run on goroutines; every change to the view
// model is posted to the window's main thread through Options.Post.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/host"
	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/view"
)

const (
	initializeTimeout = 60 * time.Second
	promptTimeout     = 60 * time.Second
	stopTimeout       = 5 * time.Second
	maxErrorRunes     = 3000
)

// Options describes one host session and how to publish view updates.
type Options struct {
	// Launch is the resolved host process configuration.
	Launch host.Config
	// CWD is recorded on the SDK session header.
	CWD string
	// Provider and Model select the model route.
	Provider string
	// Model is the model id sent in initialize.
	Model string
	// ReasoningEffort is optional; empty leaves it to the host.
	ReasoningEffort string
	// SessionID names the SDK session this window drives.
	SessionID string
	// MaxFrame bounds one JSON-RPC frame; zero uses the channel default.
	MaxFrame int
	// Post runs fn on the window's main thread. A nil Post runs it inline,
	// which tests use.
	Post func(fn func())
}

// rpcClient is the subset of channel.Client the app uses. It keeps the app
// testable without a host process.
type rpcClient interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
	OnNotification(handler channel.NotificationHandler)
	Run() error
	Close()
}

// App owns one window's host process, protocol client, and projected
// transcript.
type App struct {
	options    Options
	model      *view.Model
	transcript projection.Transcript

	mu         sync.Mutex
	supervisor *host.Supervisor
	client     rpcClient

	// These fields belong to the main thread.
	awaitingTurn bool
	sawActivity  bool
	promptID     uint64

	shuttingDown atomic.Bool
}

// New creates an app. The caller starts it once the window exists.
func New(options Options) *App {
	if options.Post == nil {
		options.Post = func(fn func()) { fn() }
	}
	app := &App{options: options}
	app.model = view.NewModel()
	if options.Provider != "" && options.Model != "" {
		app.model.ModelLabel = options.Provider + " · " + options.Model
	}
	app.model.Send = app.Send
	app.model.Dismiss = app.DismissError
	return app
}

// Model is the state the window renders.
func (a *App) Model() *view.Model { return a.model }

// Start spawns the host, begins reading its protocol frames, and initializes
// the SDK runtime. It blocks until initialization finishes, so call it on a
// goroutine.
func (a *App) Start() {
	supervisor, err := host.Start(context.Background(), a.options.Launch)
	if err != nil {
		a.fail("Could not start the DeepSeek Harness host", err)
		return
	}
	a.mu.Lock()
	a.supervisor = supervisor
	a.mu.Unlock()

	client := channel.NewClient(supervisor.Stdout(), supervisor.Stdin(), a.options.MaxFrame)
	client.OnNotification(func(method string, params json.RawMessage) {
		a.post(func() { a.handleNotification(method, params) })
	})
	a.mu.Lock()
	a.client = client
	a.mu.Unlock()

	go a.watchHost(supervisor)
	go func() {
		err := client.Run()
		if errors.Is(err, io.EOF) || errors.Is(err, channel.ErrClosed) || a.shuttingDown.Load() {
			return
		}
		a.fail("The host connection closed", err)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), initializeTimeout)
	defer cancel()
	params := map[string]any{
		"cwd":      a.options.CWD,
		"provider": a.options.Provider,
		"model":    a.options.Model,
	}
	if a.options.ReasoningEffort != "" {
		params["reasoningEffort"] = a.options.ReasoningEffort
	}
	result, err := client.Call(ctx, "initialize", params)
	if err != nil {
		a.fail("Could not initialize the host", err)
		a.stopHost()
		return
	}

	var initialized struct {
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	_ = json.Unmarshal(result, &initialized)
	a.post(func() {
		if a.shuttingDown.Load() || a.model.Phase == view.PhaseFailed {
			return
		}
		if initialized.ServerInfo.Name != "" || initialized.ServerInfo.Version != "" {
			a.model.Detail = strings.TrimSpace(initialized.ServerInfo.Name + " " + initialized.ServerInfo.Version)
		}
		if a.model.Phase == view.PhaseStarting {
			a.model.Phase = view.PhaseIdle
		}
	})
}

// Send queues one user prompt. It runs on the main thread and ignores calls
// while the host is busy or not ready.
func (a *App) Send(text string) {
	text = strings.TrimSpace(text)
	if text == "" || a.shuttingDown.Load() || a.model.Phase != view.PhaseIdle {
		return
	}
	client := a.currentClient()
	if client == nil {
		return
	}
	a.model.Draft = ""
	a.model.Error = ""
	a.model.ErrorDetail = ""
	a.model.Phase = view.PhaseRunning
	a.awaitingTurn = true
	a.sawActivity = false
	a.promptID++
	go a.prompt(client, text, a.promptID)
}

func (a *App) prompt(client rpcClient, text string, promptID uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), promptTimeout)
	defer cancel()
	_, err := client.Call(ctx, "session/prompt", map[string]any{
		"sessionId": a.options.SessionID,
		"contentBlocks": []map[string]any{
			{"type": "text", "text": text},
		},
	})
	a.post(func() {
		if a.shuttingDown.Load() || err == nil || a.model.Phase == view.PhaseFailed || a.promptID != promptID {
			return
		}
		a.awaitingTurn = false
		a.sawActivity = false
		a.model.Phase = view.PhaseIdle
		a.model.Error = "Could not deliver the prompt"
		a.model.ErrorDetail = clipDetail(err.Error()+"\n\nThe prompt may not have reached the host. Check before retrying.", maxErrorRunes)
		if a.model.Draft == "" {
			a.model.Draft = text
		}
	})
}

// DismissError hides the error banner without changing the host state.
func (a *App) DismissError() {
	a.model.Error = ""
	a.model.ErrorDetail = ""
}

// handleNotification runs on the main thread.
func (a *App) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "session.event":
		notification, err := projection.DecodeNotification(params)
		if err != nil {
			return
		}
		if notification.SessionID != a.options.SessionID {
			return
		}
		if isTurnActivity(notification.Event.Type) {
			a.sawActivity = true
		}
		if a.transcript.Apply(notification.Event) {
			a.model.Items = a.transcript.Items()
		}
	case "session.status":
		var notification struct {
			SessionID string `json:"sessionId"`
			Status    string `json:"status"`
		}
		if json.Unmarshal(params, &notification) != nil || notification.SessionID != a.options.SessionID {
			return
		}
		a.applyStatus(notification.Status)
	}
}

func (a *App) applyStatus(status string) {
	switch status {
	case "running":
		if a.model.Phase == view.PhaseStarting || a.model.Phase == view.PhaseFailed {
			return
		}
		a.model.Phase = view.PhaseRunning
	case "idle":
		if a.model.Phase == view.PhaseStarting || a.model.Phase == view.PhaseFailed {
			return
		}
		if a.awaitingTurn {
			// An idle status can be emitted before the queued prompt is
			// claimed. Only the idle after turn activity ends our turn.
			if !a.sawActivity {
				return
			}
			a.awaitingTurn = false
			a.sawActivity = false
		}
		a.model.Phase = view.PhaseIdle
	}
}

// Shutdown stops the host and is safe to call more than once. The deadline
// bounds the protocol shutdown, and the process stop gets its own deadline.
func (a *App) Shutdown(timeout time.Duration) {
	if !a.shuttingDown.CompareAndSwap(false, true) {
		return
	}
	a.mu.Lock()
	client := a.client
	supervisor := a.supervisor
	a.mu.Unlock()

	if client != nil {
		callTimeout := timeout
		if callTimeout > 3*time.Second {
			callTimeout = 3 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		_, _ = client.Call(ctx, "shutdown", map[string]any{})
		cancel()
		client.Close()
	}
	if supervisor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		_ = supervisor.Stop(ctx)
		cancel()
	}
}

func (a *App) watchHost(supervisor *host.Supervisor) {
	err := supervisor.WaitError()
	a.post(func() {
		if a.shuttingDown.Load() || a.model.Phase == view.PhaseFailed {
			return
		}
		detail := "host exited"
		if err != nil {
			detail = err.Error()
		}
		a.setFailure("The DeepSeek Harness host exited", errors.New(detail))
	})
}

func (a *App) fail(summary string, err error) {
	a.post(func() { a.setFailure(summary, err) })
}

func (a *App) setFailure(summary string, err error) {
	if a.model.Phase == view.PhaseFailed {
		return
	}
	a.model.Phase = view.PhaseFailed
	a.model.Error = summary
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	if tail := a.stderrTail(); tail != "" {
		detail += "\n\nHost stderr:\n" + tail
	}
	a.model.ErrorDetail = clipDetail(detail, maxErrorRunes)
	a.awaitingTurn = false
	a.sawActivity = false
}

func (a *App) stopHost() {
	a.mu.Lock()
	client := a.client
	supervisor := a.supervisor
	a.mu.Unlock()
	if client != nil {
		client.Close()
	}
	if supervisor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		_ = supervisor.Stop(ctx)
		cancel()
	}
}

func (a *App) currentClient() rpcClient {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.client
}

func (a *App) stderrTail() string {
	a.mu.Lock()
	supervisor := a.supervisor
	a.mu.Unlock()
	if supervisor == nil {
		return ""
	}
	return strings.TrimSpace(supervisor.StderrTail())
}

func (a *App) post(fn func()) {
	a.options.Post(fn)
}

func isTurnActivity(eventType string) bool {
	switch eventType {
	case "turn/start", "step/start", "user/message", "assistant/message",
		"assistant/attempt", "tool/call", "turn/end":
		return true
	default:
		return false
	}
}

func clipDetail(text string, limit int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return fmt.Sprintf("…%s", string(runes[len(runes)-limit:]))
}
