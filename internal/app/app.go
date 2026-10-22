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
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/host"
	"dsh-mygo/internal/interaction"
	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/view"
)

const (
	initializeTimeout   = 60 * time.Second
	promptTimeout       = 60 * time.Second
	cancelTimeout       = 10 * time.Second
	stopTimeout         = 5 * time.Second
	maxErrorRunes       = 3000
	maxPendingUIUpdates = 32
)

// Options describes one host session and how to publish view updates.
type Options struct {
	// Launch is the resolved host process configuration.
	Launch host.Config
	// CWD is recorded on the Host session header.
	CWD string
	// Provider and Model select the model route.
	Provider string
	// Model is the model id used when creating a session.
	Model string
	// ReasoningEffort is optional; empty leaves it to the host.
	ReasoningEffort string
	UseHostDefaults bool
	// SessionID names the session in legacy SDK mode; custom channel ids come from the Host.
	SessionID   string
	SDKProtocol bool
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
	approvals  interaction.ApprovalQueue
	questions  interaction.QuestionQueue
	stream     projection.AssistantStream

	mu                sync.Mutex
	supervisor        *host.Supervisor
	client            rpcClient
	notificationInput chan hostNotification

	// These fields belong to the main thread.
	awaitingTurn       bool
	sawActivity        bool
	promptID           uint64
	sessionID          string
	nextSeq            uint64
	promptDone         <-chan error
	cancelIdle         bool
	batchUpdating      bool
	transcriptDirty    bool
	sessionOperation   uint64
	sessionCancel      context.CancelFunc
	loadingSession     string
	sessionBuffer      []hostNotification
	sessionBufferBytes int
	drafts             map[string]string
	settingsOperation  uint64
	settingsCancel     context.CancelFunc

	shuttingDown atomic.Bool
	stopped      chan struct{}
	pendingUI    chan struct{}
}

// New creates an app. The caller starts it once the window exists.
func New(options Options) *App {
	if options.Post == nil {
		options.Post = func(fn func()) { fn() }
	}
	app := &App{options: options, sessionID: options.SessionID, stopped: make(chan struct{}), pendingUI: make(chan struct{}, maxPendingUIUpdates)}
	app.model = view.NewModel()
	if options.Provider != "" && options.Model != "" {
		app.model.SetActiveRoute(view.ModelRoute{Provider: options.Provider, Model: options.Model, Effort: options.ReasoningEffort})
	}
	app.model.Send = app.Send
	app.model.Dismiss = app.DismissError
	app.model.Cancel = app.Cancel
	app.model.RespondApproval = app.RespondApproval
	app.model.RespondQuestion = app.RespondQuestion
	app.model.OpenSessions = app.OpenSessions
	app.model.CloseSessions = app.CloseSessions
	app.model.RefreshSessions = app.RefreshSessions
	app.model.NewSession = app.NewSession
	app.model.SwitchSession = app.SwitchSession
	app.model.OpenSettings = app.OpenSettings
	app.model.CloseSettings = app.CloseSettings
	app.model.RefreshSettings = app.RefreshSettings
	app.model.ApplySettings = app.ApplySettings
	app.model.SaveDefaultModel = app.SaveDefaultModel
	app.model.SaveCredential = app.SaveCredential
	app.model.ClearCredential = app.ClearCredential
	app.model.ActiveRoute = view.ModelRoute{Provider: options.Provider, Model: options.Model, Effort: options.ReasoningEffort}
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
	if a.shuttingDown.Load() {
		a.mu.Unlock()
		a.stopHost()
		return
	}
	a.mu.Unlock()

	client := channel.NewClient(supervisor.Stdout(), supervisor.Stdin(), a.options.MaxFrame)
	incoming := make(chan hostNotification)
	go a.runNotifications(incoming)
	client.OnNotification(func(method string, params json.RawMessage) {
		select {
		case incoming <- hostNotification{method: method, params: params}:
		case <-a.stopped:
			return
		}
	})
	a.mu.Lock()
	a.client = client
	a.notificationInput = incoming
	if a.shuttingDown.Load() {
		a.mu.Unlock()
		a.stopHost()
		return
	}
	a.mu.Unlock()

	go a.watchHost(supervisor)
	go func() {
		err := client.Run()
		if errors.Is(err, io.EOF) || errors.Is(err, channel.ErrClosed) || a.shuttingDown.Load() {
			return
		}
		a.fail("The host connection closed", err)
		a.stopHost()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), initializeTimeout)
	defer cancel()
	initialized, err := a.initialize(ctx, client)
	if err != nil {
		if !a.options.SDKProtocol {
			err = fmt.Errorf("%w\n\nCheck that the mygo profile contains dsh-base and @dsh-mygo/host. Initialize a new development profile with resources/host/setup-profile.mjs --dsh-home <absolute-directory>", err)
		}
		a.fail("Could not initialize the host", err)
		a.stopHost()
		return
	}

	a.post(func() {
		if a.shuttingDown.Load() || a.model.Phase == view.PhaseFailed {
			return
		}
		a.sessionID = initialized.sessionID
		a.nextSeq = initialized.nextSeq
		a.model.SessionID = initialized.sessionID
		a.model.SessionCWD = a.options.CWD
		a.model.CanHistory = initialized.canHistory
		a.model.CanModels = initialized.canModels
		a.model.CanConfigure = initialized.canConfigure
		if initialized.route.Provider != "" {
			a.model.SetActiveRoute(initialized.route)
		}
		a.model.CanCancel = initialized.canCancel
		a.model.CanApprove = initialized.canApprove
		a.model.CanQuestion = initialized.canQuestion
		a.model.CanStream = initialized.canStream
		a.model.Detail = initialized.detail
		if a.model.Phase == view.PhaseStarting {
			a.model.Phase = view.PhaseIdle
		}
	})
}

type initializedSession struct {
	sessionID    string
	nextSeq      uint64
	detail       string
	canCancel    bool
	canApprove   bool
	canQuestion  bool
	canStream    bool
	canHistory   bool
	canModels    bool
	canConfigure bool
	route        view.ModelRoute
}

func (a *App) initialize(ctx context.Context, client rpcClient) (initializedSession, error) {
	if a.options.SDKProtocol {
		params := map[string]any{"cwd": a.options.CWD, "provider": a.options.Provider, "model": a.options.Model}
		if a.options.ReasoningEffort != "" {
			params["reasoningEffort"] = a.options.ReasoningEffort
		}
		result, err := client.Call(ctx, "initialize", params)
		if err != nil {
			return initializedSession{}, err
		}
		var initialized struct {
			ServerInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
		}
		if json.Unmarshal(result, &initialized) != nil || initialized.ServerInfo.Name == "" {
			return initializedSession{}, errors.New("invalid SDK host handshake")
		}
		return initializedSession{sessionID: a.options.SessionID, detail: strings.TrimSpace(initialized.ServerInfo.Name + " " + initialized.ServerInfo.Version)}, nil
	}
	result, err := client.Call(ctx, "initialize", map[string]any{
		"protocolVersion": channel.ProtocolVersion,
		"client":          map[string]string{"name": "dsh-mygo", "version": channel.ReleaseVersion, "platform": runtime.GOOS, "locale": "zh-CN"},
		"capabilities":    []string{"session.cancel", "approval", "question", "session.assistant-stream", "session.history", "model.selection", "settings.configuration"},
	})
	if err != nil {
		return initializedSession{}, err
	}
	initialized, err := channel.DecodeInitialize(result)
	if err != nil {
		return initializedSession{}, err
	}
	canConfigure := slices.Contains(initialized.Capabilities, "settings.configuration")
	params := a.routeParams(canConfigure)
	params["cwd"] = a.options.CWD
	result, err = client.Call(ctx, "session/create", params)
	if err != nil {
		return initializedSession{}, err
	}
	var created struct {
		Session struct {
			ID       string  `json:"id"`
			Status   string  `json:"status"`
			NextSeq  *uint64 `json:"nextSeq"`
			Provider *string `json:"provider"`
			Model    *string `json:"model"`
			Effort   *string `json:"effort"`
		} `json:"session"`
	}
	if json.Unmarshal(result, &created) != nil || !validSessionID(created.Session.ID) || created.Session.Status != "idle" || created.Session.NextSeq == nil || *created.Session.NextSeq > maxSafeInteger {
		return initializedSession{}, errors.New("invalid session/create response")
	}
	var selected view.ModelRoute
	if canConfigure {
		if created.Session.Provider == nil || created.Session.Model == nil || created.Session.Effort == nil || !validModelLabel(*created.Session.Provider, false) || !validModelLabel(*created.Session.Model, false) || !validModelLabel(*created.Session.Effort, true) {
			return initializedSession{}, errors.New("Invalid created session route")
		}
		selected = view.ModelRoute{Provider: *created.Session.Provider, Model: *created.Session.Model, Effort: *created.Session.Effort}
	}
	return initializedSession{
		sessionID: created.Session.ID, nextSeq: *created.Session.NextSeq,
		detail:       initialized.Server.Name + " " + initialized.Server.Version + " · DSH " + initialized.Server.DSHVersion,
		canCancel:    slices.Contains(initialized.Capabilities, "session.cancel"),
		canApprove:   slices.Contains(initialized.Capabilities, "approval"),
		canQuestion:  slices.Contains(initialized.Capabilities, "question"),
		canStream:    slices.Contains(initialized.Capabilities, "session.assistant-stream"),
		canHistory:   slices.Contains(initialized.Capabilities, "session.history"),
		canModels:    slices.Contains(initialized.Capabilities, "model.selection"),
		canConfigure: canConfigure, route: selected,
	}, nil
}

func validSessionID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character != '-' && character != '_' && !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}

// Send queues one user prompt. It runs on the main thread and ignores calls
// while the host is busy or not ready.
func (a *App) Send(text string) {
	text = strings.TrimSpace(text)
	if text == "" || a.shuttingDown.Load() || a.model.Phase != view.PhaseIdle || len(a.model.Questions) > 0 || a.model.SessionBusy || a.model.SettingsBusy || a.model.SettingsOpen || a.model.ModelUncertain || a.model.ConfigurationUncertain {
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
	a.clearStream()
	a.model.CancelPending = false
	a.cancelIdle = false
	a.awaitingTurn = true
	a.sawActivity = false
	a.promptID++
	delivery := make(chan error, 1)
	a.promptDone = delivery
	go a.prompt(client, text, a.promptID, a.sessionID, delivery)
}

func (a *App) prompt(client rpcClient, text string, promptID uint64, sessionID string, delivery chan error) {
	defer close(delivery)
	ctx, cancel := context.WithTimeout(context.Background(), promptTimeout)
	defer cancel()
	contentField := "content"
	if a.options.SDKProtocol {
		contentField = "contentBlocks"
	}
	_, err := client.Call(ctx, "session/prompt", map[string]any{
		"sessionId": sessionID,
		contentField: []map[string]any{
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
		a.model.CancelPending = false
		a.model.Error = "Could not deliver the prompt"
		a.model.ErrorDetail = clipDetail(err.Error()+"\n\nThe prompt may not have reached the host. Check before retrying.", maxErrorRunes)
		if a.model.Draft == "" {
			a.model.Draft = text
		}
	})
	delivery <- err
}

func (a *App) Cancel() {
	if a.shuttingDown.Load() || a.options.SDKProtocol || !a.model.CanCancel || a.model.Phase != view.PhaseRunning || a.model.CancelPending {
		return
	}
	client := a.currentClient()
	if client == nil {
		return
	}
	a.model.CancelPending = true
	a.cancelIdle = false
	a.model.Error = ""
	a.model.ErrorDetail = ""
	go a.cancelTurn(client, a.promptID, a.sessionID, a.promptDone)
}

func (a *App) cancelTurn(client rpcClient, promptID uint64, sessionID string, delivery <-chan error) {
	ctx, cancel := context.WithTimeout(context.Background(), cancelTimeout)
	defer cancel()
	var result json.RawMessage
	var err error
	if delivery != nil {
		select {
		case deliveryError := <-delivery:
			if deliveryError != nil {
				return
			}
		case <-ctx.Done():
			err = ctx.Err()
		case <-a.stopped:
			return
		}
	}
	if err == nil {
		result, err = client.Call(ctx, "session/cancel", map[string]any{"sessionId": sessionID})
	}
	a.post(func() {
		if a.shuttingDown.Load() || a.promptID != promptID || a.model.Phase != view.PhaseRunning || !a.model.CancelPending {
			return
		}
		a.model.CancelPending = false
		if a.cancelIdle {
			a.awaitingTurn = false
			a.sawActivity = false
			a.model.Phase = view.PhaseIdle
			return
		}
		var stopped struct {
			Cancelled *bool  `json:"cancelled"`
			Status    string `json:"status"`
		}
		if err == nil && (json.Unmarshal(result, &stopped) != nil || stopped.Cancelled == nil || stopped.Status != "idle") {
			err = errors.New("invalid session/cancel response")
		}
		if err != nil {
			var rpcError *channel.RPCError
			if errors.As(err, &rpcError) {
				var detail struct {
					Kind string `json:"kind"`
				}
				if json.Unmarshal(rpcError.Data, &detail) == nil && detail.Kind == "session/cancel-timeout" {
					a.awaitingTurn = false
				}
			}
			a.model.Error = "Could not stop the turn"
			a.model.ErrorDetail = clipDetail(err.Error(), maxErrorRunes)
			return
		}
		a.awaitingTurn = false
		a.sawActivity = false
		a.model.Phase = view.PhaseIdle
	})
}

// DismissError hides the error banner without changing the host state.
func (a *App) DismissError() {
	a.model.Error = ""
	a.model.ErrorDetail = ""
}

// handleNotification runs on the main thread.
func (a *App) handleNotification(method string, params json.RawMessage) {
	if a.shuttingDown.Load() || a.model.Phase == view.PhaseFailed {
		return
	}
	if !a.options.SDKProtocol {
		if a.bufferSessionNotification(method, params) {
			return
		}
		switch method {
		case "session/assistant-stream":
			a.handleStreamNotification(params)
			return
		case "question/request", "question/closed":
			a.handleQuestionNotification(method, params)
			return
		case "approval/request", "approval/closed":
			a.handleApprovalNotification(method, params)
			return
		case "session/event":
			method = "session.event"
		case "session/status":
			method = "session.status"
		case "host/fatal":
			a.setFailure("The host reported an agent failure", errors.New("Restart the host before retrying"))
			go a.stopHost()
			return
		case "session.event", "session.status":
			a.setFailure("Invalid host notification", errors.New("SDK notifications are not valid on the mygo channel"))
			go a.stopHost()
			return
		}
	}
	switch method {
	case "session.event":
		notification, err := projection.DecodeNotification(params)
		if err != nil {
			if !a.options.SDKProtocol {
				a.setFailure("Invalid host event", errors.New("The host sent an invalid session event"))
				go a.stopHost()
			}
			return
		}
		if notification.SessionID != a.sessionID {
			return
		}
		if !a.options.SDKProtocol {
			var envelope struct {
				Seq   *uint64 `json:"seq"`
				Event struct {
					Seq *uint64 `json:"seq"`
				} `json:"event"`
			}
			if json.Unmarshal(params, &envelope) != nil || envelope.Seq == nil || envelope.Event.Seq == nil || *envelope.Seq != notification.Event.Seq || *envelope.Seq > a.nextSeq {
				a.setFailure("Invalid session event sequence", errors.New("Restart the host; this version cannot resynchronize history"))
				go a.stopHost()
				return
			}
			if *envelope.Seq < a.nextSeq {
				return
			}
			a.nextSeq++
		}
		if isTurnActivity(notification.Event.Type) {
			a.sawActivity = true
		}
		a.stream.Commit(notification.Event)
		a.transcript.Apply(notification.Event)
		a.publishTranscript()
	case "session.status":
		var notification struct {
			SessionID string `json:"sessionId"`
			Status    string `json:"status"`
		}
		if json.Unmarshal(params, &notification) != nil {
			if !a.options.SDKProtocol {
				a.setFailure("Invalid host status", errors.New("The host sent an invalid session status"))
				go a.stopHost()
			}
			return
		}
		if notification.SessionID != a.sessionID {
			return
		}
		if !a.options.SDKProtocol && notification.Status != "idle" && notification.Status != "running" {
			a.setFailure("Invalid host status", errors.New("The host sent an unknown session status"))
			go a.stopHost()
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
		a.cancelIdle = false
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
		a.questions.RemoveSession(a.sessionID)
		a.clearStream()
		a.publishQuestions()
		if a.model.CancelPending {
			a.clearApprovals()
			a.cancelIdle = true
			return
		}
		a.model.Phase = view.PhaseIdle
		a.model.CancelPending = false
		a.clearApprovals()
	}
}

// Shutdown stops the host and is safe to call more than once. The deadline
// bounds the protocol shutdown, and the process stop gets its own deadline.
func (a *App) Shutdown(timeout time.Duration) {
	if !a.shuttingDown.CompareAndSwap(false, true) {
		return
	}
	close(a.stopped)
	a.post(a.clearApprovals)
	a.post(a.clearQuestions)
	a.post(a.clearStream)
	a.post(a.cancelSessionOperation)
	a.post(a.cancelSettingsOperation)
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
	a.cancelSessionOperation()
	a.model.SessionsOpen = false
	a.cancelSettingsOperation()
	a.model.SettingsOpen = false
	a.model.CancelPending = false
	a.clearApprovals()
	a.clearQuestions()
	a.clearStream()
	a.model.Error = summary
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	if a.options.SDKProtocol {
		if tail := a.stderrTail(); tail != "" {
			detail += "\n\nHost stderr:\n" + tail
		}
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
	a.mu.Lock()
	incoming := a.notificationInput
	a.mu.Unlock()
	if incoming == nil || a.shuttingDown.Load() {
		a.options.Post(fn)
		return
	}
	select {
	case incoming <- hostNotification{apply: fn}:
	case <-a.stopped:
	}
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
