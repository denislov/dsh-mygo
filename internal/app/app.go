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
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"dsh-mygo/internal/attachment"
	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/host"
	"dsh-mygo/internal/interaction"
	"dsh-mygo/internal/platform"
	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/runtimes"
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
	ApplyTheme  func(string)
	ApplyLocale func(string)
	// Launch is the resolved host process configuration. Runtime mode resolves
	// a launch plan from Runtimes for every start/restart instead.
	Launch host.Config
	// Runtimes is the runtime store when the app uses the split runtime model.
	Runtimes *runtimes.Manager
	// Profile and DSHHome are used to resolve a runtime launch.
	Profile string
	DSHHome string
	// PrepareProfile seeds or upgrades $DSH_HOME/profiles/<profile> from the
	// selected runtime's Host bundle.
	PrepareProfile func(hostBundleDir string) error
	// RuntimeLaunch overrides runtime resolution for tests.
	RuntimeLaunch func() (RuntimeLaunchPlan, error)
	// CWD is recorded on the Host session header.
	CWD string
	// RequireWorkspace asks users of a packaged empty conversation to confirm its folder.
	RequireWorkspace bool
	// PickDirectory overrides the native folder dialog for tests.
	PickDirectory func(string) (string, error)
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
	// PreferencesPath is an isolated device-local editing file; empty disables persistence.
	PreferencesPath        string
	PreferencesUnavailable bool
	// Quit exits the application from the recovery dialog.
	Quit func()
	// Post runs fn on the window's main thread. A nil Post runs it inline,
	// which tests use.
	Post func(fn func())
}

// RuntimeLaunchPlan is the resolved host process for one selected runtime.
type RuntimeLaunchPlan struct {
	Config        host.Config
	HostBundleDir string
	Identity      channel.HostIdentity
	Runtime       runtimes.Runtime
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
	workspace  workspaceResources
	hostLocale atomic.Value
	options    Options
	model      *view.Model
	transcript projection.Transcript
	approvals  interaction.ApprovalQueue
	questions  interaction.QuestionQueue
	stream     projection.AssistantStream

	mu                sync.Mutex
	host              *hostInstance
	lastHost          *hostInstance
	notificationInput chan hostNotification

	// These fields belong to the main thread.
	turn                turnTracker
	sessionID           string
	nextSeq             uint64
	batchUpdating       bool
	transcriptDirty     bool
	sessionSlot         operationSlot
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
	canHistoryWindow    bool
	canRelease          bool
	attachmentLimits    *attachment.Limits
	mentionSeq          atomic.Uint64
	retiredSessions     map[string]bool
	drafts              map[string]string
	local               *localEditingState
	quitReady           bool
	settingsSlot        operationSlot
	runtimeGeneration   uint64
	runtimeCancel       context.CancelFunc
	switchMu            sync.Mutex
	switchRuntime       *runtimeSwitch
	inspectionRead      operationSlot
	restarting          bool

	hostGeneration atomic.Uint64
	shuttingDown   atomic.Bool
	stopped        chan struct{}
	pendingUI      chan struct{}

	diag *diagnostics
}

// New creates an app. The caller starts it once the window exists.
func New(options Options) *App {
	if options.Post == nil {
		options.Post = func(fn func()) { fn() }
	}
	if options.Profile == "" {
		options.Profile = "mygo"
	}
	app := &App{options: options, sessionID: options.SessionID, stopped: make(chan struct{}), pendingUI: make(chan struct{}, maxPendingUIUpdates), diag: newDiagnostics(options.LogDir)}
	app.model = view.NewModel()
	app.configureWorkspace()
	app.model.AppVersion = channel.ReleaseVersion
	app.model.Setup.ChooseFolder = app.ChooseProjectFolder
	app.model.Setup.UseFolder = app.UseWorkingFolder
	app.model.Setup.Connect = app.OpenConnectionSettings
	app.hostLocale.Store("en-US")
	app.model.Prefs.Apply = app.ApplyUIPreferences
	app.model.Inspection.Show = app.OpenSubagents
	app.model.Inspection.Close = app.CloseSubagents
	app.model.Inspection.Refresh = app.RefreshSubagents
	app.model.Inspection.Select = app.SelectSubagent
	app.model.Inspection.EarlierPage = app.EarlierSubagentHistory
	if options.Provider != "" && options.Model != "" {
		app.model.SetActiveRoute(view.ModelRoute{Provider: options.Provider, Model: options.Model, Effort: options.ReasoningEffort})
	}
	app.model.Send = app.Send
	app.model.Attachments.AttachImage = app.attachImage
	app.model.Attachments.AttachFiles = app.AttachFiles
	app.model.Attachments.PickAttachment = app.pickAttachment
	app.model.QueryMentions = app.QueryMentions
	app.model.DraftChanged = app.DraftChanged
	app.model.Dismiss = app.DismissError
	app.model.Cancel = app.Cancel
	app.model.RespondApproval = app.RespondApproval
	app.model.RespondQuestion = app.RespondQuestion
	app.model.OpenSessions = app.OpenSessions
	app.model.CloseSessions = app.CloseSessions
	app.model.RefreshSessions = app.RefreshSessions
	app.model.NewSession = app.NewSession
	app.model.NewProjectSession = app.NewProjectSession
	app.model.SwitchSession = app.SwitchSession
	app.model.History.LoadEarlier = app.LoadEarlierHistory
	app.model.History.Latest = app.ReturnToLatest
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
	app.model.Permission.Apply = app.ApplyPermission
	app.model.Runtime.Detect = app.DetectRuntimes
	app.model.Runtime.BrowseDSH = app.BrowseDSH
	app.model.Runtime.CheckUpdates = app.CheckRuntimeUpdates
	app.model.Runtime.Download = app.DownloadRuntime
	app.model.Runtime.InstallFile = app.InstallRuntimeFile
	app.model.Runtime.UseExternal = app.UseExternalRuntime
	app.model.Runtime.UseInstalled = app.UseInstalledRuntime
	app.model.Runtime.UseLegacy = app.UseLegacyRuntime
	app.model.Runtime.Activate = app.ActivateRuntime
	app.model.Runtime.Remove = app.RemoveRuntime
	app.model.Runtime.Manage = app.OpenRuntimeSettings
	app.model.Runtime.Cancel = app.CancelRuntimeOperation
	app.model.Runtime.CanManage = options.Runtimes != nil
	app.model.Restart = app.Restart
	app.model.OpenLogs = app.OpenLogs
	app.model.Quit = options.Quit
	app.model.CanOpenLogs = options.LogDir != ""
	app.model.ActiveRoute = view.ModelRoute{Provider: options.Provider, Model: options.Model, Effort: options.ReasoningEffort}
	app.configureLocalState()
	app.logf("app created: version=%s protocol=%d dsh=%s", channel.ReleaseVersion, channel.ProtocolVersion, channel.DSHVersion)
	return app
}

// Model is the state the window renders.
func (a *App) Model() *view.Model { return a.model }

// Start spawns the host, begins reading its protocol frames, and initializes
// the runtime. It blocks until initialization finishes, so call it on a
// goroutine.
func (a *App) Start() {
	recent := a.loadLocalState()
	if a.shuttingDown.Load() {
		return
	}
	var runtimeErr error
	if a.options.Runtimes != nil {
		runtimeErr = a.options.Runtimes.Load()
		a.options.Runtimes.CleanupStaging()
		if _, ok := a.options.Runtimes.Selected(); !ok {
			a.awaitRuntime(runtimeErr)
			a.post(a.DetectRuntimes)
			return
		}
		if runtimeErr != nil {
			a.logf("runtime state notice: %v", runtimeErr)
		}
		a.post(a.refreshRuntimeChoicesLocked)
	}
	a.startHost(false, recent, view.ModelRoute{})
}

// awaitRuntime shows the pre-host setup state. The full onboarding view lives
// in internal/view; the app only publishes the state.
func (a *App) awaitRuntime(err error) {
	message := ""
	if err != nil {
		message = "The selected DSH runtime could not be used. Choose another runtime or install a new one."
		a.logf("runtime setup required: %v", err)
	}
	a.post(func() {
		if a.shuttingDown.Load() {
			return
		}
		a.model.Runtime.SetupRequired = true
		a.model.Runtime.Busy = false
		a.model.Runtime.Error = message
		a.model.Phase = view.PhaseAwaitingRuntime
		a.model.Error = ""
		a.model.ErrorDetail = ""
		a.model.RecoveryOpen = false
		a.model.Recoverable = false
	})
}

func (a *App) resolveLaunchPlan() (RuntimeLaunchPlan, error) {
	if a.options.RuntimeLaunch != nil {
		return a.options.RuntimeLaunch()
	}
	if a.options.Runtimes != nil {
		profile := a.options.Profile
		if profile == "" {
			profile = "mygo"
		}
		var (
			config     host.Config
			hostBundle string
			runtime    runtimes.Runtime
			err        error
		)
		if target, switching := a.pendingRuntimeSwitch(); switching {
			config, hostBundle, runtime, err = a.options.Runtimes.LaunchRuntime(target, profile, a.options.CWD, a.options.DSHHome)
		} else {
			config, hostBundle, runtime, err = a.options.Runtimes.Launch(profile, a.options.CWD, a.options.DSHHome)
		}
		if err != nil {
			return RuntimeLaunchPlan{}, err
		}
		return RuntimeLaunchPlan{
			Config:        config,
			HostBundleDir: hostBundle,
			Identity:      channel.HostIdentity{Version: runtime.HostVersion, DSHVersion: runtime.DSH},
			Runtime:       runtime,
		}, nil
	}
	return RuntimeLaunchPlan{
		Config:   a.options.Launch,
		Identity: channel.HostIdentity{Version: channel.ReleaseVersion, DSHVersion: channel.DSHVersion},
	}, nil
}

func (a *App) startHost(recover bool, resumeID string, route view.ModelRoute) {
	generation := a.hostGeneration.Add(1)
	plan, err := a.resolveLaunchPlan()
	if err != nil {
		if a.hasPendingRuntimeSwitch() {
			a.failRuntimeSwitch(err)
			return
		}
		if a.options.Runtimes != nil {
			a.awaitRuntime(err)
			return
		}
		a.fail("Could not start the DeepSeek Harness host", err)
		return
	}
	if a.options.Runtimes != nil {
		if plan.HostBundleDir != "" && a.options.PrepareProfile != nil {
			if err := a.options.PrepareProfile(plan.HostBundleDir); err != nil {
				if a.hasPendingRuntimeSwitch() {
					a.failRuntimeSwitch(err)
					return
				}
				a.fail("Could not prepare the DeepSeek Harness profile", err)
				return
			}
		}
	} else if a.options.Preflight != nil {
		if err := a.options.Preflight(); err != nil {
			summary := a.options.PreflightSummary
			if summary == "" {
				summary = "Could not start the DeepSeek Harness host"
			}
			a.fail(summary, err)
			return
		}
	}
	a.logf("host start: generation=%d recover=%v command=%s", generation, recover, filepath.Base(plan.Config.Command))
	supervisor, err := host.Start(context.Background(), plan.Config)
	if err != nil {
		if a.hasPendingRuntimeSwitch() {
			a.failRuntimeSwitch(err)
			return
		}
		summary := "Could not start the DeepSeek Harness host"
		if recover {
			summary = "Could not restart the DeepSeek Harness host"
		}
		a.fail(summary, err)
		return
	}
	client := channel.NewClient(supervisor.Stdout(), supervisor.Stdin(), a.options.MaxFrame)
	instance := &hostInstance{generation: generation, client: client, supervisor: supervisor}
	if !a.installHost(instance) {
		instance.stop(stopTimeout)
		return
	}
	a.mu.Lock()
	incoming := a.notificationInput
	startNotifications := false
	if incoming == nil {
		incoming = make(chan hostNotification)
		a.notificationInput = incoming
		startNotifications = true
	}
	a.mu.Unlock()
	if startNotifications {
		go a.runNotifications(incoming)
	}
	client.OnNotification(func(method string, params json.RawMessage) {
		a.forwardHostNotification(instance, incoming, method, params)
	})
	a.logf("host started: pid=%d generation=%d", supervisor.PID(), generation)

	go a.watchHost(instance)
	go func() {
		err := client.Run()
		if errors.Is(err, io.EOF) || errors.Is(err, channel.ErrClosed) || a.shuttingDown.Load() || !a.isCurrentHost(instance) {
			return
		}
		a.failHost(instance, "The host connection closed", err)
		a.stopHostInstance(instance)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), initializeTimeout)
	defer cancel()
	initialized, err := a.initializeSession(ctx, client, sessionInitialization{
		resumeID: resumeID, route: route, identity: plan.Identity,
		allowFallback: !recover && resumeID != "" && !a.switchNeedsResume(),
	})
	if err != nil {
		if a.hasPendingRuntimeSwitch() {
			a.failRuntimeSwitch(err)
			return
		}
		if !a.options.SDKProtocol && !recover {
			err = fmt.Errorf("%w\n\nCheck that the mygo profile contains dsh-base and @dsh-mygo/host. Initialize a new development profile with resources/host/setup-profile.mjs --dsh-home <absolute-directory>", err)
		}
		summary := "Could not initialize the host"
		if recover {
			summary = "Could not recover the session"
		}
		a.failHost(instance, summary, err)
		a.stopHostInstance(instance)
		return
	}
	if !a.isCurrentHost(instance) {
		a.stopHostInstance(instance)
		return
	}
	switchedID := ""
	if a.hasPendingRuntimeSwitch() {
		target, err := a.commitRuntimeSwitch()
		if err != nil {
			a.stopHostInstance(instance)
			a.failRuntimeSwitch(err)
			return
		}
		switchedID = target.ID
	}

	a.logf("session ready: %s", initialized.sessionID)
	var initialConfiguration *view.Configuration
	if initialized.caps.Configure {
		configurationCtx, configurationCancel := context.WithTimeout(ctx, 5*time.Second)
		if snapshot, err := readConfiguration(configurationCtx, client); err == nil {
			initialConfiguration = &snapshot
		}
		configurationCancel()
	}
	a.post(func() {
		if a.shuttingDown.Load() || a.model.Phase == view.PhaseFailed || !a.isCurrentHost(instance) {
			return
		}
		previousID := a.sessionID
		if a.sessionID != "" && a.sessionID != initialized.sessionID {
			if a.drafts == nil {
				a.drafts = make(map[string]string)
			}
			a.drafts[a.sessionID] = a.model.Draft
			a.model.Draft = a.drafts[initialized.sessionID]
		}
		a.sessionID = initialized.sessionID
		if a.local != nil && a.local.ready {
			if previousID != initialized.sessionID {
				a.model.Draft = a.drafts[initialized.sessionID]
			}
			a.local.lastSession = initialized.sessionID
		}
		if initialized.restoreNotice != "" {
			a.model.LocalState.Notice = initialized.restoreNotice
			a.offerPreviousDraft(resumeID)
		}
		a.nextSeq = initialized.nextSeq
		a.turn.reset()
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
		a.model.Subagents = nil
		a.CloseSubagents()
		a.model.Inspection.Available = initialized.caps.InspectSubagents
		a.model.SessionCWD = initialized.cwd
		a.syncWorkspace()
		a.model.CanHistory = initialized.caps.History
		a.canHistoryWindow = initialized.caps.HistoryWindow
		a.canRelease = initialized.caps.Release
		a.retiredSessions = nil
		a.model.History.Available = initialized.caps.HistoryWindow
		a.acceptHistoryWindow(initialized.history)
		a.model.CanResync = initialized.caps.Resync
		a.model.Permission.Available = initialized.permissions != nil
		a.model.Permission.Ready = false
		a.model.Permission.Uncertain = false
		a.model.Permission.Confirm = false
		if initialized.permissions != nil {
			a.model.Permission.Accept(*initialized.permissions)
		}
		a.canStreamBaseline = initialized.caps.StreamBaseline
		a.acceptAttachments(initialized.attachments)
		a.statusRevision = 0
		a.hostStatus = ""
		a.model.SessionPanel.CanRename = initialized.caps.Rename
		a.model.SessionPanel.CanArchive = initialized.caps.Archive
		a.model.SessionPanel.Uncertain = false
		a.model.SessionPanel.ActiveArchived = false
		a.sessionNeedsHistory = false
		a.sessionTitleSeq = 0
		a.sessionTitle = ""
		a.CancelSessionEdit()
		a.model.CanModels = initialized.caps.Models
		a.model.CanConfigure = initialized.caps.Configure
		a.model.ConfigurationReady = initialConfiguration != nil
		if initialConfiguration != nil {
			a.model.Configuration = *initialConfiguration
		}
		a.model.Setup.Picking, a.model.Setup.Error = false, ""
		a.model.Setup.WorkspaceRequired = a.options.RequireWorkspace && len(a.model.Items) == 0
		a.model.Runtime.SetupRequired = false
		a.model.Runtime.Switching = false
		a.model.Runtime.Busy = false
		a.model.Runtime.Progress = 0
		a.model.Runtime.Status = ""
		a.model.Runtime.Error = ""
		a.model.Runtime.Notice = ""
		if switchedID != "" {
			a.model.SettingsNotice = "Runtime switched."
			a.refreshRuntimeChoicesLocked()
		}
		if initialized.route.Provider != "" {
			a.model.SetActiveRoute(initialized.route)
		}
		a.model.CanCancel = initialized.caps.Cancel
		a.model.CanApprove = initialized.caps.Approval
		a.model.CanQuestion = initialized.caps.Question
		a.model.CanStream = initialized.caps.Stream
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
		a.persistLocalState()
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
	a.model.Setup.Picking = false
	a.CloseSubagents()
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
	a.turn.reset()
	a.logf("restart requested")
	resumeID := a.sessionID
	if a.model.SessionPanel.ActiveArchived {
		resumeID = ""
	}
	route := a.model.ActiveRoute
	previous := a.detachHost()
	go func() {
		a.stopHostInstance(previous)
		a.startHost(true, resumeID, route)
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
	caps          protocol.Capabilities
	history       historySnapshot
	sessionID     string
	nextSeq       uint64
	cwd           string
	transcript    *projection.Transcript
	detail        string
	permissions   *view.PermissionSnapshot
	restoreNotice string
	route         view.ModelRoute
	attachments   *attachment.Limits
}

func (a *App) initialize(ctx context.Context, client rpcClient, resumeID string) (initializedSession, error) {
	return a.initializeSession(ctx, client, sessionInitialization{resumeID: resumeID})
}

// sessionInitialization is an immutable input to one startup worker. Recovery
// callers capture route on the main thread; identity comes from its launch plan.
type sessionInitialization struct {
	resumeID      string
	route         view.ModelRoute
	identity      channel.HostIdentity
	allowFallback bool
}

func (a *App) initializeSession(ctx context.Context, client rpcClient, request sessionInitialization) (initializedSession, error) {
	if a.options.SDKProtocol {
		params := protocol.SDKInitializeParams{
			CWD: a.options.CWD, Provider: a.options.Provider, Model: a.options.Model,
			ReasoningEffort: a.options.ReasoningEffort,
		}
		result, err := client.Call(ctx, protocol.MethodInitialize, params)
		if err != nil {
			return initializedSession{}, err
		}
		var initialized protocol.SDKInitializeResult
		if json.Unmarshal(result, &initialized) != nil || initialized.ServerInfo.Name == "" {
			return initializedSession{}, errors.New("invalid SDK host handshake")
		}
		return initializedSession{sessionID: a.options.SessionID, cwd: a.options.CWD, detail: strings.TrimSpace(initialized.ServerInfo.Name + " " + initialized.ServerInfo.Version)}, nil
	}
	result, err := client.Call(ctx, protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersion: channel.ProtocolVersion,
		Client:          protocol.ClientInfo{Name: "dsh-mygo", Version: channel.ReleaseVersion, Platform: runtime.GOOS, Locale: a.currentLocale()},
		Capabilities:    protocol.ClientCapabilities(),
	})
	if err != nil {
		return initializedSession{}, err
	}
	var initialized channel.InitializeResult
	if request.identity.Version != "" {
		initialized, err = channel.DecodeInitializeExpecting(result, request.identity)
	} else {
		initialized, err = channel.DecodeInitialize(result)
	}
	if err != nil {
		return initializedSession{}, err
	}
	caps := protocol.ParseCapabilities(initialized.Capabilities)
	restoreNotice := ""
	if request.resumeID != "" && caps.History {
		route := request.route
		if request.allowFallback {
			route = view.ModelRoute{}
		}
		restored, err := a.resumeSession(ctx, client, request.resumeID, initialized, caps, route)
		if err == nil || !request.allowFallback {
			return restored, err
		}
		restoreNotice = localRestoreNotice()
	} else if request.resumeID != "" && request.allowFallback {
		restoreNotice = localRestoreNotice()
	}
	result, err = client.Call(ctx, protocol.MethodSessionCreate, protocol.SessionCreateParams{
		CWD:          a.options.CWD,
		SessionRoute: a.sessionRoute(caps.Configure),
	})
	if err != nil {
		return initializedSession{}, err
	}
	var response protocol.SessionResult
	if json.Unmarshal(result, &response) != nil {
		return initializedSession{}, errors.New("invalid session/create response")
	}
	created, err := decodeSessionSummary(response.Session)
	if err != nil || created.Status != "idle" || created.NextSeq == nil {
		return initializedSession{}, errors.New("invalid session/create response")
	}
	var selected view.ModelRoute
	var permissions *view.PermissionSnapshot
	if caps.Permissions {
		snapshot, err := readPermissions(ctx, client, created.ID)
		if err != nil {
			return initializedSession{}, err
		}
		if snapshot.NextSeq != *created.NextSeq {
			return initializedSession{}, errors.New("Permission boundary changed during initialization")
		}
		permissions = &snapshot
	}
	if caps.Configure {
		if !validModelLabel(created.Provider, false) || !validModelLabel(created.Model, false) || !validModelLabel(created.Effort, true) {
			return initializedSession{}, errors.New("Invalid created session route")
		}
		selected = view.ModelRoute{Provider: created.Provider, Model: created.Model, Effort: created.Effort}
	}
	return initializedSession{
		sessionID: created.ID, nextSeq: *created.NextSeq, cwd: a.options.CWD,
		caps:          caps,
		restoreNotice: restoreNotice,
		permissions:   permissions,
		route:         selected,
		detail:        initialized.Server.Name + " " + initialized.Server.Version + " · DSH " + initialized.Server.DSHVersion,
		attachments:   attachmentLimitsOf(initialized),
	}, nil
}

// resumeSession restores the previous session and replays its durable history
// after a host restart. A failure leaves the previous projection untouched.
func (a *App) resumeSession(ctx context.Context, client rpcClient, id string, initialized channel.InitializeResult, caps protocol.Capabilities, route view.ModelRoute) (initializedSession, error) {
	requestRoute := a.sessionRoute(caps.Configure)
	if route.Provider != "" && route.Model != "" {
		requestRoute = protocol.ExplicitRoute(route.Provider, route.Model, route.Effort)
	}
	raw, err := client.Call(ctx, protocol.MethodSessionResume, protocol.SessionResumeParams{SessionID: id, SessionRoute: requestRoute})
	if err != nil {
		return initializedSession{}, fmt.Errorf("resume session: %w", err)
	}
	var result protocol.SessionResult
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
	if caps.Configure && (summary.Provider == "" || summary.Model == "") {
		return initializedSession{}, errors.New("invalid restored session route")
	}
	history, err := readOpenHistory(ctx, client, summary.ID, *summary.NextSeq, caps.HistoryWindow)
	transcript, nextSeq := history.Transcript, history.NextSeq
	if err != nil {
		return initializedSession{}, err
	}
	cwd := summary.CWD
	if cwd == "" {
		cwd = a.options.CWD
	}
	selected := view.ModelRoute{}
	var permissions *view.PermissionSnapshot
	if caps.Permissions {
		snapshot, err := readPermissions(ctx, client, id)
		if err != nil {
			return initializedSession{}, err
		}
		if snapshot.NextSeq != nextSeq {
			return initializedSession{}, errors.New("Permission boundary changed during restore")
		}
		permissions = &snapshot
	}
	if caps.Configure {
		selected = view.ModelRoute{Provider: summary.Provider, Model: summary.Model, Effort: summary.Effort}
	}
	return initializedSession{
		sessionID: summary.ID, nextSeq: nextSeq, cwd: cwd, transcript: &transcript,
		caps:        caps,
		history:     history,
		permissions: permissions,
		route:       selected,
		detail:      initialized.Server.Name + " " + initialized.Server.Version + " · DSH " + initialized.Server.DSHVersion,
		attachments: attachmentLimitsOf(initialized),
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
	original := originalSubmittedText(a.model.Draft, text)
	text = strings.TrimSpace(text)
	if text == "" && len(a.model.Attachments.Pending) == 0 || a.shuttingDown.Load() || !a.model.CanSendPrompt() {
		return
	}
	client := a.currentClient()
	if client == nil {
		return
	}
	blocks, err := a.promptBlocks(text)
	if err != nil {
		a.model.Error = err.Error()
		a.model.ErrorDetail = ""
		return
	}
	if a.model.SessionPanel.Refreshing || a.model.History.Busy {
		a.cancelSessionOperation()
	}
	a.model.Draft = ""
	a.model.Error = ""
	a.model.ErrorDetail = ""
	a.model.Phase = view.PhaseRunning
	a.clearStream()
	a.model.CancelPending = false
	delivery := make(chan error, 1)
	promptID := a.turn.submit(delivery)
	a.rememberSubmittedDraft(promptID, original)
	request := a.promptRequest(a.sessionID, blocks)
	go a.prompt(client, request, text, promptID, delivery)
}

// promptRequest selects the custom channel or the explicit legacy SDK field
// spelling. The session identity is captured on the main thread.
func (a *App) promptRequest(sessionID string, blocks []protocol.ContentBlock) any {
	if a.options.SDKProtocol {
		return protocol.SDKPromptParams{SessionID: sessionID, ContentBlocks: blocks}
	}
	return protocol.SessionPromptParams{SessionID: sessionID, Content: blocks}
}

func (a *App) prompt(client rpcClient, request any, submitted string, promptID uint64, delivery chan error) {
	defer close(delivery)
	ctx, cancel := context.WithTimeout(context.Background(), promptTimeout)
	defer cancel()
	result, err := client.Call(ctx, protocol.MethodSessionPrompt, request)
	if err == nil && a.local != nil {
		var acknowledged protocol.PromptResult
		if json.Unmarshal(result, &acknowledged) != nil || !validModelLabel(acknowledged.MessageID, false) {
			err = errors.New("Invalid prompt delivery acknowledgement")
		}
	}
	a.postAfterResync(func() {
		if !a.shuttingDown.Load() {
			a.settleSubmittedDraft(promptID, err)
		}
		if err == nil && !a.shuttingDown.Load() && a.turn.current(promptID) {
			a.model.Attachments.Clear()
		}
		if a.shuttingDown.Load() || err == nil || a.model.Phase == view.PhaseFailed || !a.turn.current(promptID) {
			return
		}
		a.turn.deliveryFailed()
		a.model.Phase = view.PhaseIdle
		a.model.CancelPending = false
		a.model.Error = "Could not deliver the prompt"
		a.model.ErrorDetail = clipDetail(err.Error()+"\n\nThe prompt may not have reached the host. Check before retrying.", maxErrorRunes)
		if a.model.Draft == "" {
			a.model.Draft = submitted
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
	a.turn.beginCancel()
	a.model.Error = ""
	a.model.ErrorDetail = ""
	go a.cancelTurn(client, a.turn.prompt, a.sessionID, a.turn.deliveryBarrier())
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
		result, err = client.Call(ctx, protocol.MethodSessionCancel, protocol.SessionCancelParams{SessionID: sessionID})
	}
	a.postAfterResync(func() {
		if a.shuttingDown.Load() || !a.turn.current(promptID) || a.model.Phase != view.PhaseRunning || !a.model.CancelPending {
			return
		}
		a.model.CancelPending = false
		if a.turn.cancelIdle {
			a.turn.finishTurn()
			a.model.Phase = view.PhaseIdle
			return
		}
		var stopped protocol.SessionCancelResult
		if err == nil && (json.Unmarshal(result, &stopped) != nil || stopped.Cancelled == nil || stopped.Status != "idle") {
			err = errors.New("invalid session/cancel response")
		}
		if err != nil {
			var rpcError *channel.RPCError
			if errors.As(err, &rpcError) && protocol.ErrorKind(rpcError.Data) == protocol.ErrorKindSessionCancelTimeout {
				a.turn.cancelTimedOut()
			}
			a.model.Error = "Could not stop the turn"
			a.model.ErrorDetail = clipDetail(err.Error(), maxErrorRunes)
			return
		}
		a.turn.finishTurn()
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

// handleNotification runs on the main thread for tests and direct callers that
// do not carry a Host generation.
func (a *App) handleNotification(method string, params json.RawMessage) {
	a.handleNotificationFrom(nil, method, params)
}

// handleNotificationFrom commits one notification. source is the Host instance
// that emitted it, or nil for SDK/test callers and local state.
func (a *App) handleNotificationFrom(source *hostInstance, method string, params json.RawMessage) {
	if a.shuttingDown.Load() || a.model.Phase == view.PhaseFailed {
		return
	}
	if a.model.Phase == view.PhaseResyncing {
		if a.resync == nil || a.bufferResyncNotificationFrom(source, method, params) {
			return
		}
	}
	if !a.options.SDKProtocol {
		if a.bufferSessionNotificationFrom(source, method, params) {
			return
		}
		switch method {
		case protocol.NotificationSessionAssistantStream:
			a.handleStreamNotificationFrom(source, params)
			return
		case protocol.NotificationQuestionRequest, protocol.NotificationQuestionClosed:
			a.handleQuestionNotification(method, params)
			return
		case protocol.NotificationApprovalRequest, protocol.NotificationApprovalClosed:
			a.handleApprovalNotification(method, params)
			return
		case protocol.NotificationSubagentStarted:
			a.handleSubagentNotification(params, false)
			return
		case protocol.NotificationSubagentFinished:
			a.handleSubagentNotification(params, true)
			return
		case protocol.NotificationSessionEvent:
			method = protocol.NotificationSDKSessionEvent
		case protocol.NotificationSessionStatus:
			method = protocol.NotificationSDKSessionStatus
		case protocol.NotificationHostFatal:
			a.setFailure("The host reported an agent failure", errors.New("Restart the host before retrying"))
			go a.stopHost()
			return
		case protocol.NotificationSDKSessionEvent, protocol.NotificationSDKSessionStatus:
			a.setFailure("Invalid host notification", errors.New("SDK notifications are not valid on the mygo channel"))
			go a.stopHost()
			return
		}
	}
	switch method {
	case protocol.NotificationSDKSessionEvent:
		a.handleSessionEventNotification(source, params)
	case protocol.NotificationSDKSessionStatus:
		a.handleSessionStatusNotification(params)
	}
}

// handleSessionEventNotification validates the durable envelope and commits one
// event to the projection.
func (a *App) handleSessionEventNotification(source *hostInstance, params json.RawMessage) {
	var envelope protocol.SessionEventNotification
	if json.Unmarshal(params, &envelope) != nil || envelope.SessionID == "" {
		if !a.options.SDKProtocol {
			a.setFailure("Invalid host event", errors.New("The host sent an invalid session event"))
			go a.stopHost()
		}
		return
	}
	event, eventSeq, err := projection.DecodeEvent(envelope.Event)
	if err != nil {
		if !a.options.SDKProtocol {
			a.setFailure("Invalid host event", errors.New("The host sent an invalid session event"))
			go a.stopHost()
		}
		return
	}
	if envelope.SessionID != a.sessionID {
		return
	}
	if !a.options.SDKProtocol {
		if envelope.Seq == nil || eventSeq == nil || *envelope.Seq != event.Seq {
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
				a.bufferResyncNotificationFrom(source, protocol.NotificationSessionEvent, params)
			}
			return
		}
		a.nextSeq++
		a.sessionCatalogDirty = true
		if event.Type == "permission/preset" || event.Type == "sandbox/mode" || event.Type == "approval/policy" {
			a.model.Permission.Ready = false
			a.model.Permission.Confirm = false
		}
		if event.Type == "session/title" {
			var data struct {
				Title string `json:"title"`
			}
			if json.Unmarshal(event.Data, &data) == nil {
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
	if isTurnActivity(event.Type) {
		a.turn.noteActivity()
	}
	a.stream.Commit(event)
	a.transcript.Apply(event)
	if a.canHistoryWindow {
		if start, trimmed := a.transcript.TrimRecent(); trimmed {
			a.model.History.Start = start
		}
	}
	a.publishTranscript()
}

// handleSessionStatusNotification validates status identity and revision before
// applying the lifecycle transition.
func (a *App) handleSessionStatusNotification(params json.RawMessage) {
	var notification protocol.SessionStatusNotification
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

func (a *App) applyStatus(status string) {
	switch status {
	case "running":
		if a.model.Phase == view.PhaseStarting || a.model.Phase == view.PhaseFailed {
			return
		}
		a.model.Phase = view.PhaseRunning
		a.turn.noteRunning()
	case "idle":
		if a.model.Phase == view.PhaseStarting || a.model.Phase == view.PhaseFailed {
			return
		}
		// An idle status can be emitted before the queued prompt is claimed.
		// Only the idle after turn activity ends our turn.
		if !a.turn.acknowledgeIdle() {
			return
		}
		a.questions.RemoveSession(a.sessionID)
		a.clearStream()
		a.publishQuestions()
		if a.model.CancelPending {
			a.clearApprovals()
			a.turn.noteIdleWhileCancelling()
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
	a.cancelWorkspaceReads()
	a.closeWorkspaceTerminals(true)
	close(a.stopped)
	if a.runtimeCancel != nil {
		a.runtimeCancel()
		a.runtimeCancel = nil
	}
	if a.local != nil && a.local.writer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		_ = a.local.writer.Flush(ctx)
		cancel()
		a.local.writer.Stop()
	}
	a.post(a.cancelResync)
	a.post(a.clearApprovals)
	a.post(a.clearQuestions)
	a.post(a.clearStream)
	a.post(a.cancelSessionOperation)
	a.post(a.cancelSettingsOperation)
	a.post(a.CloseSubagents)
	instance := a.detachHost()

	if instance != nil && instance.client != nil {
		callTimeout := timeout
		if callTimeout > 3*time.Second {
			callTimeout = 3 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		_, _ = instance.client.Call(ctx, "shutdown", map[string]any{})
		cancel()
	}
	a.stopHostInstance(instance)
	a.logf("shutdown complete")
	a.closeLog()
}

func (a *App) watchHost(instance *hostInstance) {
	err := instance.supervisor.WaitError()
	a.logf("host exited: %v", err)
	detail := "host exited"
	if err != nil {
		detail = err.Error()
	}
	a.failHost(instance, "The DeepSeek Harness host exited", errors.New(detail))
}

// failHost publishes a failure only if the captured Host is still current when
// the callback reaches the main thread.
func (a *App) failHost(instance *hostInstance, summary string, err error) {
	a.post(func() {
		if a.shuttingDown.Load() || a.model.Phase == view.PhaseFailed {
			return
		}
		// A failure may stop its own instance before this callback runs. A
		// newer generation, however, owns the failure path from then on.
		if !a.isCurrentHost(instance) && a.hostGeneration.Load() != instance.generation {
			return
		}
		a.setFailure(summary, err)
	})
}

func (a *App) fail(summary string, err error) {
	a.post(func() { a.setFailure(summary, err) })
}

func (a *App) setFailure(summary string, err error) {
	a.model.Setup.Picking = false
	a.CloseSubagents()
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
	a.model.Runtime.Switching = false
	a.switchMu.Lock()
	a.switchRuntime = nil
	a.switchMu.Unlock()
	a.model.RecoveryError = ""
	a.model.ToolInspector = nil
	a.turn.clear()
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
	a.turn.clear()
	a.clearApprovals()
	a.clearQuestions()
	a.clearStream()
	a.logf("resync required: %v", err)
}

func (a *App) stderrTail() string {
	supervisor := a.currentSupervisor()
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

func (a *App) logf(format string, args ...any) {
	a.diag.logf(format, args...)
}

func (a *App) closeLog() {
	a.diag.close()
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
	a.diag.capture(report)
}

func (a *App) supervisorPID() int {
	supervisor := a.currentSupervisor()
	if supervisor == nil {
		return 0
	}
	return supervisor.PID()
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
