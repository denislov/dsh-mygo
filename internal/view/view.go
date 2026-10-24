// Package view renders the dsh-mygo native window from a small UI model.
// It owns no host or session state: the app updates Model and MyGo rebuilds
// the view on the window's main thread.
package view

import (
	"fmt"

	mgo "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"dsh-mygo/internal/interaction"
	"dsh-mygo/internal/preferences"
	"dsh-mygo/internal/projection"
)

// Phase is the connection state the window shows.
type Phase int

const (
	// PhaseStarting means the host process is starting or initializing.
	PhaseStarting Phase = iota
	// PhaseIdle means the host is ready and accepts a prompt.
	PhaseIdle
	// PhaseRunning means a prompt is in flight.
	PhaseRunning
	// PhaseResyncing means the event stream has a gap and sends are paused.
	PhaseResyncing
	// PhaseStopped means the host was shut down.
	PhaseStopped
	// PhaseFailed means the host could not start or stopped unexpectedly.
	PhaseFailed
)

// Model is the window's state. The app mutates it on the main thread and
// keeps the transcript and draft outside the view.
type Model struct {
	Shell                ShellState
	Prefs                PreferencesState
	Attachments          AttachmentState
	Mention              MentionState
	QueryMentions        func(prefix string)
	Items                []projection.Item
	History              HistoryState
	Draft                string
	DraftChanged         func()
	LocalState           LocalStateStatus
	Phase                Phase
	ModelLabel           string
	Detail               string
	Usage                projection.UsageView
	Context              projection.ContextView
	Tasks                []projection.Task
	PlanActive           bool
	Goal                 *projection.Goal
	Subagents            []SubagentState
	Error                string
	ErrorDetail          string
	RecoveryOpen         bool
	Recoverable          bool
	Restarting           bool
	RecoveryError        string
	Restart              func()
	OpenLogs             func()
	CanOpenLogs          bool
	Quit                 func()
	Send                 func(text string)
	Dismiss              func()
	Cancel               func()
	CanCancel            bool
	CanStream            bool
	CancelPending        bool
	CanApprove           bool
	ApprovalOpen         bool
	Approvals            []interaction.Approval
	RespondApproval      func(requestID, decision string)
	CanQuestion          bool
	QuestionOpen         bool
	Questions            []*interaction.QuestionRequest
	RespondQuestion      func(requestID string, cancel bool)
	List                 mgo.ListState
	ToolCards            map[string]*ToolCardState
	ToolInspector        *ToolInspectorState
	Markdown             markdown.Options
	InterruptedMarkdown  markdown.Options
	CopiedMessageID      string
	CopiedMessageText    string
	CanHistory           bool
	CanResync            bool
	SessionPanel         SessionPanelState
	SessionID            string
	SessionCWD           string
	Sessions             []SessionSummary
	SessionsOpen         bool
	SessionBusy          bool
	SessionSwitching     bool
	SessionsError        string
	SessionList          mgo.ListState
	OpenSessions         func()
	RefreshSessions      func()
	CloseSessions        func()
	NewSession           func()
	SwitchSession        func(string)
	CanModels            bool
	ActiveRoute          ModelRoute
	Catalog              ModelCatalog
	SettingsOpen         bool
	SettingsBusy         bool
	SettingsApplying     bool
	ModelUncertain       bool
	SettingsError        string
	SettingsProvider     string
	SettingsModel        string
	SettingsCatalogModel string
	SettingsEffort       string
	OpenSettings         func()
	CloseSettings        func()
	RefreshSettings      func()
	ApplySettings        func()

	CanConfigure                 bool
	Permission                   PermissionState
	Configuration                Configuration
	ConfigurationReady           bool
	ConfigurationUncertain       bool
	SettingsWritingConfiguration bool
	CredentialDraft              CredentialDraft
	CredentialVersion            uint64
	ConfirmCredentialClear       bool
	SettingsNotice               string
	SaveDefaultModel             func()
	SaveCredential               func()
	ClearCredential              func()
}

// NewModel returns a model with a list that follows the newest row and keeps
// row state by item ID.
func NewModel() *Model {
	model := &Model{Phase: PhaseStarting,
		Markdown:            markdown.Options{Theme: &markdown.Theme{FontSize: 15, LineHeight: 1.55}, Reveal: &markdown.Reveal{Unit: markdown.AtOnce, Fade: -1}},
		InterruptedMarkdown: markdown.Options{Theme: &markdown.Theme{FontSize: 15, LineHeight: 1.55}},
	}
	model.List.FollowEnd = true
	model.InterruptedMarkdown.CodeBlock = model.interruptedCodeBlock
	model.Prefs = PreferencesState{Value: preferences.DefaultUI(), Ready: true}
	model.Mention.CaretTarget = -1
	model.List.Key = func(row int) any { return model.Items[row].ID }
	return model
}

// View builds the whole window.
func View(c *mgo.Context, model *Model) {
	switch model.Prefs.Value.Theme {
	case "light":
		c.SetTheme(lightTheme)
	case "dark":
		c.SetTheme(darkTheme)
	default:
		if c.Theme().Dark {
			c.SetTheme(darkTheme)
		} else {
			c.SetTheme(lightTheme)
		}
	}
	if model.Prefs.Value.Locale == "zh-CN" {
		if c.Theme().Dark {
			c.SetTheme(darkChineseTheme)
		} else {
			c.SetTheme(lightChineseTheme)
		}
	}
	if model.SettingsOpen || model.Prefs.Open || model.SessionsOpen || model.ToolInspector != nil || model.RecoveryOpen || len(model.Approvals) > 0 || len(model.Questions) > 0 {
		model.Shell.DetailsOpen = false
	}
	applicationShortcuts(c, model)
	theme := c.Theme()
	services := c.Services()
	model.Markdown.OnLink = func(address string) {
		if safeMarkdownLink(address) {
			services.OpenURL(address)
		}
	}
	model.InterruptedMarkdown.OnLink = model.Markdown.OnLink
	mgo.Row(c.Key("shell")).Fill().Background(theme.Background).Children(func() {
		width, _ := c.Size()
		if width >= 820 && model.CanHistory && !model.SessionsOpen {
			sessionSidebar(c, model)
		}
		mgo.Column(c.Key("conversation")).Grow(1).MinWidth(0).FillHeight().Children(func() {
			header(c, model)
			if !model.RecoveryOpen {
				localStateStatus(c, model)
			}
			if model.Error != "" && !model.RecoveryOpen {
				errorBanner(c, model)
			}
			historyControls(c, model)
			transcript(c, model)
			taskPanel(c, model)
			composer(c, model)
		})
	})
	approvalModal(c, model)
	questionModal(c, model)
	toolInspectorModal(c, model)
	sessionsModal(c, model)
	settingsModal(c, model)
	recoveryModal(c, model)
	preferenceModal(c, model)
}

func errorBanner(c *mgo.Context, model *Model) {
	theme := c.Theme()
	mgo.Column(c.Key("host-error-banner")).Margin(12, 20).Padding(10, 12).Gap(4).Radius(8).Transition(panelTransition).
		Background(theme.Surface).Border(1, theme.Danger).Children(func() {
		mgo.Row(c).Gap(8).AlignItems(mgo.Center).Children(func() {
			mgo.Text(c, model.T("Host error")).FontSize(12).Bold().TextColor(theme.Danger)
			mgo.Spacer(c)
			if model.Dismiss != nil && mgo.Button(c, model.T("Dismiss")).Clicked() {
				model.Dismiss()
			}
		})
		banner := mgo.Column(c).Gap(4).Selectable()
		banner.Children(func() {
			mgo.Text(c, model.T(model.Error)).FontSize(13)
			if model.ErrorDetail != "" {
				mgo.Text(c, model.ErrorDetail).FontSize(11).TextColor(theme.TextMuted)
			}
		})
	})
}

func transcript(c *mgo.Context, model *Model) {
	mgo.List(c.Key("transcript-"+model.SessionID), &model.List, len(model.Items), func(row int) {
		message(c, model, model.Items[row])
	}).Grow(1).Gap(22).Padding(24, 24).Children(func() {
		if len(model.Items) == 0 {
			if model.History.Start > 0 {
				mgo.Text(c, model.T("No visible messages in this history window.")).FontSize(13)
			} else {
				emptyConversation(c, model)
			}
		}
	})
}

func message(c *mgo.Context, model *Model, item projection.Item) {
	mgo.Row(c.Key(item.ID)).FillWidth().Justify(mgo.Center).Children(func() {
		mgo.Column(c).FillWidth().MaxWidth(readingWidth).Children(func() { transcriptItem(c, model, item) })
	})
}

func transcriptItem(c *mgo.Context, model *Model, item projection.Item) {
	switch item.Kind {
	case projection.KindUser:
		mgo.Row(c).Key(item.ID).FillWidth().Justify(mgo.End).Children(func() {
			card(c, model, item, true)
		})
	case projection.KindAssistant:
		mgo.Row(c).Key(item.ID).FillWidth().Children(func() {
			card(c, model, item, false)
		})
	case projection.KindTool:
		mgo.Row(c).Key(item.ID).FillWidth().Children(func() {
			toolCard(c, model, item)
		})
	default:
		theme := c.Theme()
		color := theme.TextMuted
		if item.IsError {
			color = theme.Danger
		}
		mgo.Text(c, model.T(item.Text)).Key(item.ID).FontSize(12).TextColor(color).
			TextAlign(mgo.Center).FillWidth()
	}
}

func card(c *mgo.Context, model *Model, item projection.Item, fromUser bool) {
	theme := c.Theme()
	container := mgo.Column(c).MaxWidth(readingWidth).Gap(10)
	if fromUser {
		container = container.MaxWidth(600).Padding(12, 16).Radius(16).Background(theme.Surface).Selectable()
	} else {
		container = container.FillWidth()
	}
	container.Children(func() {
		mgo.Row(c).Gap(8).Children(func() {
			if !fromUser {
				mgo.Icon(c, iconSpark).Size(16, 16).TextColor(theme.Accent)
			}
			mgo.Text(c, model.T(item.Title)).FontSize(11).Bold().TextColor(theme.TextMuted)
			if item.Detail != "" {
				mgo.Text(c, model.T(item.Detail)).FontSize(11).TextColor(theme.TextMuted)
			}
			mgo.Spacer(c)
			if item.Text != "" {
				mgo.Toolbar(c.Key("message-actions"), func() { messageCopyButton(c, model, item, fromUser) }).Padding(0)
			}
		})
		if len(item.Attachments) > 0 {
			mgo.Column(c).Gap(4).Children(func() {
				for _, attachment := range item.Attachments {
					mgo.Text(c, attachmentLabel(model, attachment)).FontSize(11).TextColor(theme.TextMuted).SingleLine()
				}
			})
		}
		if item.Truncated {
			mgo.Text(c, model.T("Truncated message: only the visible prefix is available.")).FontSize(11).TextColor(theme.Warning)
		}
		if fromUser || item.Truncated {
			if item.Text != "" {
				mgo.Text(c, item.Text).FontSize(15).LineHeight(1.5).Selectable()
			}
		} else if item.State == "streaming" {
			markdown.Stream(c.Key("assistant-markdown"), item.Text, &model.Markdown).FillWidth()
		} else if item.State == "interrupted" {
			markdown.View(c.Key("assistant-markdown"), item.Text, &model.InterruptedMarkdown).FillWidth()
		} else {
			markdown.View(c.Key("assistant-markdown"), item.Text, &model.Markdown).FillWidth()
		}
	})
}

func toolCard(c *mgo.Context, model *Model, item projection.Item) {
	theme := c.Theme()
	stateColor := theme.TextMuted
	if item.IsError {
		stateColor = theme.Danger
	}
	mgo.Column(c).FillWidth().MaxWidth(readingWidth).Padding(8, 12).Gap(4).Radius(10).
		Background(theme.Surface).Border(1, theme.Border).Selectable().Children(func() {
		mgo.Row(c).Gap(8).AlignItems(mgo.Center).Children(func() {
			mgo.Collapsible(c.Key("tool-disclosure"), model.toolTitle(item.Title), &model.toolCardState(item.ID).Expanded, func() {
				if item.Text != "" {
					mgo.Text(c, item.Text).FontSize(11).TextColor(theme.TextMuted)
				}
				if item.Detail != "" {
					mgo.Text(c, item.Detail).FontSize(12)
				}
				if mgo.Button(c.Key("tool-details"), model.T("View details")).Clicked() {
					model.OpenToolDetails(item.ID)
				}
			}).Grow(1)
			mgo.Spacer(c)
			if item.State != "" {
				mgo.Text(c, model.T(toolState(item.State))).FontSize(11).TextColor(stateColor)
			}
		})
	})
}

func composerHint(model *Model) string {
	if model.Phase == PhaseIdle && model.Prefs.Value.Send == "Ctrl+Enter" && !model.Permission.Uncertain && !model.ModelUncertain && !model.ConfigurationUncertain && !model.SessionSwitching {
		return "Ctrl+Enter sends · Enter adds a line"
	}
	if model.Permission.Uncertain && model.Phase == PhaseIdle {
		return "Permissions unconfirmed · open Settings to refresh"
	}
	if (model.ModelUncertain || model.ConfigurationUncertain) && model.Phase == PhaseIdle {
		return "Model selection unconfirmed · open Settings to refresh"
	}
	if model.SessionSwitching {
		return "Loading session history…"
	}
	if model.CancelPending {
		return "Stopping…"
	}
	switch model.Phase {
	case PhaseStarting:
		return "Starting the host…"
	case PhaseRunning:
		return "Working…"
	case PhaseResyncing:
		return "Syncing history…"
	case PhaseIdle:
		return "Enter sends · Shift+Enter adds a line"
	default:
		return "Host is not running"
	}
}

func phaseLabel(phase Phase) string {
	switch phase {
	case PhaseStarting:
		return "Starting"
	case PhaseIdle:
		return "Ready"
	case PhaseRunning:
		return "Working"
	case PhaseResyncing:
		return "Syncing"
	case PhaseStopped:
		return "Stopped"
	case PhaseFailed:
		return "Host error"
	default:
		return fmt.Sprintf("Unknown (%d)", phase)
	}
}

func phaseColor(phase Phase, theme *mgo.Theme) mgo.Color {
	switch phase {
	case PhaseIdle:
		return theme.Success
	case PhaseRunning:
		return theme.Accent
	case PhaseStarting, PhaseResyncing:
		return theme.Warning
	case PhaseFailed:
		return theme.Danger
	default:
		return theme.TextMuted
	}
}

func toolState(state string) string {
	switch state {
	case "running":
		return "running…"
	case "done":
		return "done"
	case "error":
		return "error"
	default:
		return state
	}
}
