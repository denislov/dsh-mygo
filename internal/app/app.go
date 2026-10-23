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
	"log"
	"os"
	"path/filepath"
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
	"dsh-mygo/internal/platform"
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
	// Preflight checks startup prerequisites such as an unresolved host entry
	// or a profile that could not be created. Start and Restart run it before
	// spawning; PreflightSummary overrides the banner summary.
	Preflight        func() error
	PreflightSummary string
	// LogDir receives dsh-mygo.log and crash reports. Empty disables file logs.
	LogDir string
	// Quit exits the application from the recovery dialog.
	Quit func()
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
	awaitingTurn        bool
	sawActivity         bool
	promptID            uint64
	sessionID           string
	nextSeq             uint64
	promptDone          <-chan error
	cancelIdle          bool
	batchUpdating       bool
	transcriptDirty     bool
	sessionOperation    uint64
	sessionCancel       context.CancelFunc
	loadingSession      string
	sessionBuffer       []hostNotification
	sessionBufferBytes  int
	sessionTitleSeq     uint64
	sessionTitle        string
	sessionNeedsHistory bool
	sessionCatalogDirty bool
	resync              *resyncState
	statusRevision      uint64
	hostStatus          string
	canStreamBaseline   bool
	drafts              map[string]string
	settingsOperation   uint64
	settingsCancel      context.CancelFunc
	restarting          bool

	hostGeneration atomic.Uint64
	shuttingDown   atomic.Bool
	stopped        chan struct{}
	pendingUI      chan struct{}

	logMu   sync.Mutex
	logger  *log.Logger
	logFile *os.File
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
	app.model.SessionPanel.OpenRename = app.OpenSessionRename
	app.model.SessionPanel.SaveRename = app.RenameSession
	app.model.SessionPanel.CancelEdit = app.CancelSessionEdit
	app.model.SessionPanel.SetArchived = app.SetSessionArchived
	app.model.OpenSettings = app.OpenSettings
	app.model.CloseSettings = app.CloseSettings
	app.model.RefreshSettings = app.RefreshSettings
	app.model.ApplySettings = app.ApplySettings
	app.model.SaveDefaultModel = app.SaveDefaultModel
	app.model.SaveCredential = app.SaveCredential
	app.model.ClearCredential = app.ClearCredential
	app.model.Restart = app.Restart
	app.model.OpenLogs = app.OpenLogs
	app.model.Quit = options.Quit
	app.model.CanOpenLogs = options.LogDir != ""
	app.model.ActiveRoute = view.ModelRoute{Provider: options.Provider, Model: options.Model, Effort: options.ReasoningEffort}
	app.openLog()
	app.logf("app created: version=%s protocol=%d dsh=%s", channel.ReleaseVersion, channel.ProtocolVersion, channel.DSHVersion)
	return app
}

// Model is the state the window renders.
func (a *App) Model() *view.Model { return a.model }

// Start spawns the host, begins reading its protocol frames, and initializes
// the runtime. It blocks until initialization finishes, so call it on a
// goroutine.
func (a *App) Start() {
	a.startHost(false, "")
}

func (a *App) startHost(recover bool, resumeID string) {
	generation := a.hostGeneration.Add(1)
	if a.options.Preflight != nil {
		if err := a.options.Preflight(); err != nil {
			summary := a.options.PreflightSummary
			if summary == "" {
				summary = "Could not start the DeepSeek Harness host"
			}
			a.fail(summary, err)
			return
		}
	}
	a.logf("host start: generation=%d recover=%v command=%s", generation, recover, filepath.Base(a.options.Launch.Command))
	supervisor, err := host.Start(context.Background(), a.options.Launch)
	if err != nil {
		summary := "Could not start the DeepSeek Harness host"
		if recover {
			summary = "Could not restart the DeepSeek Harness host"
		}
		a.fail(summary, err)
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
	a.mu.Lock()
	incoming := a.notificationInput
	startNotifications := false
	if incoming == nil {
		incoming = make(chan hostNotification)
		a.notificationInput = incoming
		startNotifications = true
	}
	a.client = client
	if a.shuttingDown.Load() {
		a.mu.Unlock()
		a.stopHost()
		return
	}
	a.mu.Unlock()
	if startNotifications {
		go a.runNotifications(incoming)
	}
	client.OnNotification(func(method string, params json.RawMessage) {
		if a.hostGeneration.Load() != generation {
			return
		}
		select {
		case incoming <- hostNotification{method: method, params: params}:
		case <-a.stopped:
			return
		}
	})
	a.logf("host started: pid=%d generation=%d", supervisor.PID(), generation)

	go a.watchHost(supervisor, generation)
	go func() {
		err := client.Run()
		if errors.Is(err, io.EOF) || errors.Is(err, channel.ErrClosed) || a.shuttingDown.Load() || a.hostGeneration.Load() != generation {
			return
		}
		a.fail("The host connection closed", err)
		a.stopHost()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), initializeTimeout)
	defer cancel()
	initialized, err := a.initialize(ctx, client, resumeID)
	if err != nil {
		if !a.options.SDKProtocol && !recover {
			err = fmt.Errorf("%w\n\nCheck that the mygo profile contains dsh-base and @dsh-mygo/host. Initialize a new development profile with resources/host/setup-profile.mjs --dsh-home <absolute-directory>", err)
		}
		summary := "Could not initialize the host"
		if recover {
			summary = "Could not recover the session"
		}
		a.fail(summary, err)
		a.stopHost()
		return
	}
	if a.hostGeneration.Load() != generation {
		return
	}

	a.logf("session ready: %s", initialized.sessionID)
	a.post(func() {
		if a.shuttingDown.Load() || a.model.Phase == view.PhaseFailed || a.hostGeneration.Load() != generation {
			return
		}
		if a.sessionID != "" && a.sessionID != initialized.sessionID {
			if a.drafts == nil {
				a.drafts = make(map[string]string)
			}
			a.drafts[a.sessionID] = a.model.Draft
			a.model.Draft = a.drafts[initialized.sessionID]
		}
		a.sessionID = initialized.sessionID
		a.nextSeq = initialized.nextSeq
		a.promptID++
		a.promptDone = nil
		a.awaitingTurn = false
		a.sawActivity = false
		a.cancelIdle = false
		a.model.CancelPending = false
		a.clearApprovals()
		a.clearQuestions()
		a.clearStream()
		a.stream = projection.AssistantStream{}
		a.model.ToolInspector = nil
		if initialized.transcript != nil {
			a.transcript = *initialized.transcript
			a.model.ResetTranscript(a.transcript.Items())
		} else if recover {
			a.transcript = projection.Transcript{}
			a.model.ResetTranscript(nil)
		}
		a.model.SessionID = initialized.sessionID
		a.model.SessionCWD = initialized.cwd
		a.model.CanHistory = initialized.canHistory
		a.model.CanResync = initialized.canResync
		a.canStreamBaseline = initialized.canStreamBaseline
		a.statusRevision = 0
		a.hostStatus = ""
		a.model.SessionPanel.CanRename = initialized.canRename
		a.model.SessionPanel.CanArchive = initialized.canArchive
		a.model.SessionPanel.Uncertain = false
		a.model.SessionPanel.ActiveArchived = false
		a.sessionNeedsHistory = false
		a.sessionTitleSeq = 0
		a.sessionTitle = ""
		a.CancelSessionEdit()
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
		a.model.Error = ""
		a.model.ErrorDetail = ""
		a.model.RecoveryOpen = false
		a.model.RecoveryError = ""
		a.model.Restarting = false
		if a.model.Phase == view.PhaseStarting || a.model.Phase == view.PhaseResyncing {
			a.model.Phase = view.PhaseIdle
		}
		a.refreshSessions(true)
	})
}

// Restart stops a failed host and resumes the current session on a new one.
// It runs on the main thread.
func (a *App) Restart() {
	if a.shuttingDown.Load() || a.restarting || !a.model.Recoverable {
		return
	}
	if a.model.Phase != view.PhaseFailed && a.model.Phase != view.PhaseResyncing {
		return
	}
	a.restarting = true
	a.cancelResync()
	// Invalidate the old watchers before stopping so their exit cannot
	// overwrite the new host state.
	a.hostGeneration.Add(1)
	a.model.Restarting = true
	a.model.RecoveryOpen = false
	a.model.RecoveryError = ""
	a.model.Phase = view.PhaseStarting
	a.model.Error = ""
	a.model.ErrorDetail = ""
	a.cancelSessionOperation()
	a.cancelSettingsOperation()
	a.clearApprovals()
	a.clearQuestions()
	a.clearStream()
	a.model.ToolInspector = nil
	a.awaitingTurn = false
	a.sawActivity = false
	a.cancelIdle = false
	a.promptID++
	a.promptDone = nil
	a.logf("restart requested")
	resumeID := a.sessionID
	if a.model.SessionPanel.ActiveArchived {
		resumeID = ""
	}
	go func() {
		a.stopHost()
		a.startHost(true, resumeID)
		a.post(func() {
			a.restarting = false
			a.model.Restarting = false
		})
	}()
}

func (a *App) OpenLogs() {
	if a.options.LogDir == "" {
		return
	}
	if err := platform.Reveal(a.options.LogDir); err != nil {
		a.model.RecoveryError = "Could not open the logs folder"
		return
	}
	a.model.RecoveryError = ""
}

type initializedSession struct {
	sessionID         string
	nextSeq           uint64
	cwd               string
	transcript        *projection.Transcript
	detail            string
	canCancel         bool
	canApprove        bool
	canQuestion       bool
	canStream         bool
	canHistory        bool
	canResync         bool
	canStreamBaseline bool
	canRename         bool
	canArchive        bool
	canModels         bool
	canConfigure      bool
	route             view.ModelRoute
}

func (a *App) initialize(ctx context.Context, client rpcClient, resumeID string) (initializedSession, error) {
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
		return initializedSession{sessionID: a.options.SessionID, cwd: a.options.CWD, detail: strings.TrimSpace(initialized.ServerInfo.Name + " " + initialized.ServerInfo.Version)}, nil
	}
	result, err := client.Call(ctx, "initialize", map[string]any{
		"protocolVersion": channel.ProtocolVersion,
		"client":          map[string]string{"name": "dsh-mygo", "version": channel.ReleaseVersion, "platform": runtime.GOOS, "locale": "zh-CN"},
		"capabilities":    []string{"session.cancel", "approval", "question", "session.assistant-stream", "session.history", "session.resync", "session.assistant-stream-baseline", "session.rename", "session.archive", "model.selection", "settings.configuration"},
	})
	if err != nil {
		return initializedSession{}, err
	}
	initialized, err := channel.DecodeInitialize(result)
	if err != nil {
		return initializedSession{}, err
	}
	canConfigure := slices.Contains(initialized.Capabilities, "settings.configuration")
	canHistory := slices.Contains(initialized.Capabilities, "session.history")
	if resumeID != "" && canHistory {
		return a.resumeSession(ctx, client, resumeID, initialized, canConfigure)
	}
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
		sessionID: created.Session.ID, nextSeq: *created.Session.NextSeq, cwd: a.options.CWD,
		detail:            initialized.Server.Name + " " + initialized.Server.Version + " · DSH " + initialized.Server.DSHVersion,
		canCancel:         slices.Contains(initialized.Capabilities, "session.cancel"),
		canApprove:        slices.Contains(initialized.Capabilities, "approval"),
		canQuestion:       slices.Contains(initialized.Capabilities, "question"),
		canStream:         slices.Contains(initialized.Capabilities, "session.assistant-stream"),
		canHistory:        slices.Contains(initialized.Capabilities, "session.history"),
		canResync:         slices.Contains(initialized.Capabilities, "session.resync"),
		canStreamBaseline: slices.Contains(initialized.Capabilities, "session.assistant-stream-baseline"),
		canRename:         slices.Contains(initialized.Capabilities, "session.rename"),
		canArchive:        slices.Contains(initialized.Capabilities, "session.archive"),
		canModels:         slices.Contains(initialized.Capabilities, "model.selection"),
		canConfigure:      canConfigure, route: selected,
	}, nil
}

// resumeSession restores the previous session and replays its durable history
// after a host restart. A failure leaves the previous projection untouched.
func (a *App) resumeSession(ctx context.Context, client rpcClient, id string, initialized channel.InitializeResult, canConfigure bool) (initializedSession, error) {
	params := map[string]any{"sessionId": id}
	if route := a.model.ActiveRoute; route.Provider != "" && route.Model != "" {
		params["provider"] = route.Provider
		params["model"] = route.Model
		if route.Effort != "" {
			params["effort"] = route.Effort
		}
	} else {
		for key, value := range a.routeParams(canConfigure) {
			params[key] = value
		}
	}
	raw, err := client.Call(ctx, "session/resume", params)
	if err != nil {
		return initializedSession{}, fmt.Errorf("resume session: %w", err)
	}
	var result struct {
		Session json.RawMessage `json:"session"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return initializedSession{}, errors.New("invalid session/resume response")
	}
	summary, err := decodeSessionSummary(result.Session)
	if err != nil {
		return initializedSession{}, err
	}
	if summary.ID != id || summary.Status != "idle" || summary.NextSeq == nil {
		return initializedSession{}, errors.New("invalid restored session identity or status")
	}
	if canConfigure && (summary.Provider == "" || summary.Model == "") {
		return initializedSession{}, errors.New("invalid restored session route")
	}
	transcript, nextSeq, err := readSessionHistory(ctx, client, summary.ID, *summary.NextSeq)
	if err != nil {
		return initializedSession{}, err
	}
	cwd := summary.CWD
	if cwd == "" {
		cwd = a.options.CWD
	}
	selected := view.ModelRoute{}
	if canConfigure {
		selected = view.ModelRoute{Provider: summary.Provider, Model: summary.Model, Effort: summary.Effort}
	}
	return initializedSession{
		sessionID: summary.ID, nextSeq: nextSeq, cwd: cwd, transcript: &transcript,
		detail:            initialized.Server.Name + " " + initialized.Server.Version + " · DSH " + initialized.Server.DSHVersion,
		canCancel:         slices.Contains(initialized.Capabilities, "session.cancel"),
		canApprove:        slices.Contains(initialized.Capabilities, "approval"),
		canQuestion:       slices.Contains(initialized.Capabilities, "question"),
		canStream:         slices.Contains(initialized.Capabilities, "session.assistant-stream"),
		canHistory:        true,
		canResync:         slices.Contains(initialized.Capabilities, "session.resync"),
		canStreamBaseline: slices.Contains(initialized.Capabilities, "session.assistant-stream-baseline"),
		canRename:         slices.Contains(initialized.Capabilities, "session.rename"),
		canArchive:        slices.Contains(initialized.Capabilities, "session.archive"),
		canModels:         slices.Contains(initialized.Capabilities, "model.selection"),
		canConfigure:      canConfigure, route: selected,
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
	if text == "" || a.shuttingDown.Load() || a.model.Phase != view.PhaseIdle || len(a.model.Questions) > 0 || a.model.SessionBusy || a.model.SessionPanel.BlocksInput() || a.model.SettingsBusy || a.model.SettingsOpen || a.model.ModelUncertain || a.model.ConfigurationUncertain {
		return
	}
	if a.model.SessionPanel.Refreshing {
		a.cancelSessionOperation()
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
	a.postAfterResync(func() {
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
	a.postAfterResync(func() {
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
	if a.model.RecoveryOpen {
		return
	}
	a.model.Error = ""
	a.model.ErrorDetail = ""
}

// handleNotification runs on the main thread.
func (a *App) handleNotification(method string, params json.RawMessage) {
	if a.shuttingDown.Load() || a.model.Phase == view.PhaseFailed {
		return
	}
	if a.model.Phase == view.PhaseResyncing {
		if a.resync == nil || a.bufferResyncNotification(method, params) {
			return
		}
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
			if json.Unmarshal(params, &envelope) != nil || envelope.Seq == nil || envelope.Event.Seq == nil || *envelope.Seq != notification.Event.Seq {
				a.setFailure("Invalid session event sequence", errors.New("The host sent an inconsistent session event sequence"))
				go a.stopHost()
				return
			}
			if *envelope.Seq < a.nextSeq {
				return
			}
			if *envelope.Seq > a.nextSeq {
				a.requireResync(errors.New("The host event sequence skipped a frame"))
				if a.resync != nil {
					a.bufferResyncNotification("session/event", params)
				}
				return
			}
			a.nextSeq++
			a.sessionCatalogDirty = true
			if notification.Event.Type == "session/title" {
				var data struct {
					Title string `json:"title"`
				}
				if json.Unmarshal(notification.Event.Data, &data) == nil {
					a.sessionTitleSeq = a.nextSeq
					a.sessionTitle = data.Title
					for index, summary := range a.model.Sessions {
						if summary.ID == a.sessionID {
							a.model.Sessions[index].Title = data.Title
						}
					}
				}
			}
		}
		if isTurnActivity(notification.Event.Type) {
			a.sawActivity = true
		}
		a.stream.Commit(notification.Event)
		a.transcript.Apply(notification.Event)
		a.publishTranscript()
	case "session.status":
		var notification struct {
			SessionID      string  `json:"sessionId"`
			Status         string  `json:"status"`
			StatusRevision *uint64 `json:"statusRevision"`
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
		if a.model.CanResync {
			if notification.StatusRevision == nil || *notification.StatusRevision > maxSafeInteger {
				a.setFailure("Invalid host status revision", errors.New("The host sent an invalid status revision"))
				go a.stopHost()
				return
			}
			if *notification.StatusRevision <= a.statusRevision {
				return
			}
			a.statusRevision = *notification.StatusRevision
		}
		a.hostStatus = notification.Status
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
		if a.resync == nil && a.sessionCatalogDirty && a.model.CanRefreshSessions() && !a.model.SessionPanel.Uncertain {
			a.sessionCatalogDirty = false
			a.refreshSessions(true)
		}
	}
}

// Shutdown stops the host and is safe to call more than once. The deadline
// bounds the protocol shutdown, and the process stop gets its own deadline.
func (a *App) Shutdown(timeout time.Duration) {
	if !a.shuttingDown.CompareAndSwap(false, true) {
		return
	}
	a.logf("shutdown requested")
	close(a.stopped)
	a.post(a.cancelResync)
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
	a.logf("shutdown complete")
	a.closeLog()
}

func (a *App) watchHost(supervisor *host.Supervisor, generation uint64) {
	err := supervisor.WaitError()
	a.logf("host exited: %v", err)
	a.post(func() {
		if a.shuttingDown.Load() || a.hostGeneration.Load() != generation || a.model.Phase == view.PhaseFailed {
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
	a.cancelResync()
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
	a.model.RecoveryOpen = true
	a.model.Recoverable = !a.options.SDKProtocol && !a.shuttingDown.Load()
	a.model.Restarting = false
	a.model.RecoveryError = ""
	a.model.ToolInspector = nil
	a.awaitingTurn = false
	a.sawActivity = false
	a.captureCrash(summary, err)
}

// requireResync negotiates automatic recovery and retains the restart fallback
// for older hosts.
func (a *App) requireResync(err error) {
	if a.model.Phase == view.PhaseFailed || a.model.Phase == view.PhaseResyncing {
		return
	}
	if a.model.CanResync && a.currentClient() != nil && !a.shuttingDown.Load() {
		a.startResync()
		return
	}
	a.model.Phase = view.PhaseResyncing
	a.model.RecoveryOpen = true
	a.model.Recoverable = !a.options.SDKProtocol && !a.shuttingDown.Load()
	a.model.Restarting = false
	a.model.Error = "The host event stream has a gap"
	a.model.ErrorDetail = "Restart the host to recover the session from its durable log. Automatic resynchronization is not available in this version."
	a.model.RecoveryError = ""
	a.awaitingTurn = false
	a.sawActivity = false
	a.clearApprovals()
	a.clearQuestions()
	a.clearStream()
	a.logf("resync required: %v", err)
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

func (a *App) openLog() {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if a.options.LogDir == "" {
		return
	}
	if err := os.MkdirAll(a.options.LogDir, 0o755); err != nil {
		a.logger = log.New(os.Stderr, "", log.LstdFlags|log.LUTC)
		a.logger.Printf("create log directory %s: %v", a.options.LogDir, err)
		return
	}
	path := filepath.Join(a.options.LogDir, "dsh-mygo.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		a.logger = log.New(os.Stderr, "", log.LstdFlags|log.LUTC)
		a.logger.Printf("open log %s: %v", path, err)
		return
	}
	a.logFile = file
	a.logger = log.New(file, "", log.LstdFlags|log.LUTC)
}

func (a *App) logf(format string, args ...any) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if a.logger != nil {
		a.logger.Printf(format, args...)
	}
}

func (a *App) closeLog() {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if a.logFile != nil {
		_ = a.logFile.Close()
		a.logFile = nil
	}
	a.logger = nil
}

// captureCrash snapshots the failure and writes it off the main thread.
func (a *App) captureCrash(summary string, err error) {
	if a.options.LogDir == "" {
		return
	}
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	pid := a.supervisorPID()
	source := "app"
	if pid != 0 {
		source = "host"
	}
	report := crashReport{
		Time:     time.Now().UTC(),
		App:      channel.ReleaseVersion,
		Protocol: channel.ProtocolVersion,
		DSH:      channel.DSHVersion,
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Session:  a.sessionID,
		Source:   source,
		Summary:  summary,
		Detail:   detail,
		PID:      pid,
		Stderr:   a.stderrTail(),
	}
	go func() {
		a.logf("failure: %s", summary)
		if detail != "" {
			a.logf("detail: %s", redactSecrets(detail))
		}
		path, writeErr := writeCrashReport(a.options.LogDir, report)
		if writeErr != nil {
			a.logf("crash report failed: %v", writeErr)
			return
		}
		a.logf("crash report: %s", path)
	}()
}

func (a *App) supervisorPID() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.supervisor == nil {
		return 0
	}
	return a.supervisor.PID()
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
