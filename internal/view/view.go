// Package view renders the dsh-mygo native window from a small UI model.
// It owns no host or session state: the app updates Model and MyGo rebuilds
// the view on the window's main thread.
package view

import (
	"fmt"
	"strings"

	mgo "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"dsh-mygo/internal/interaction"
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
	// PhaseStopped means the host was shut down.
	PhaseStopped
	// PhaseFailed means the host could not start or stopped unexpectedly.
	PhaseFailed
)

// Model is the window's state. The app mutates it on the main thread and
// keeps the transcript and draft outside the view.
type Model struct {
	Items                []projection.Item
	Draft                string
	Phase                Phase
	ModelLabel           string
	Detail               string
	Error                string
	ErrorDetail          string
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
		Markdown:            markdown.Options{Theme: &markdown.Theme{FontSize: 13}, Reveal: &markdown.Reveal{Unit: markdown.AtOnce, Fade: -1}},
		InterruptedMarkdown: markdown.Options{Theme: &markdown.Theme{FontSize: 13}, CodeBlock: interruptedCodeBlock},
	}
	model.List.FollowEnd = true
	model.List.Key = func(row int) any { return model.Items[row].ID }
	return model
}

// View builds the whole window.
func View(c *mgo.Context, model *Model) {
	theme := c.Theme()
	services := c.Services()
	model.Markdown.OnLink = func(address string) {
		if safeMarkdownLink(address) {
			services.OpenURL(address)
		}
	}
	model.InterruptedMarkdown.OnLink = model.Markdown.OnLink
	mgo.Column(c).Fill().Background(theme.Background).Children(func() {
		header(c, model)
		if model.Error != "" {
			errorBanner(c, model)
		}
		transcript(c, model)
		composer(c, model)
	})
	approvalModal(c, model)
	questionModal(c, model)
	toolInspectorModal(c, model)
	sessionsModal(c, model)
	settingsModal(c, model)
}

func header(c *mgo.Context, model *Model) {
	theme := c.Theme()
	mgo.Row(c).Padding(14, 20).Gap(12).AlignItems(mgo.Center).Children(func() {
		mgo.Text(c, "dsh-mygo").FontSize(15).Bold()
		if model.CanHistory && model.OpenSessions != nil {
			mgo.Button(c.Key("sessions"), "Sessions").Disabled(!model.CanChooseSession()).OnClick(model.OpenSessions)
		}
		if model.CanModels && model.OpenSettings != nil {
			mgo.Button(c.Key("settings"), "Settings").Disabled(!model.CanEditSettings()).OnClick(model.OpenSettings)
		}
		if model.ModelLabel != "" && !model.CanModels {
			mgo.Text(c, model.ModelLabel).FontSize(12).TextColor(theme.TextMuted)
		}
		mgo.Spacer(c)
		if model.Detail != "" && !model.CanModels {
			mgo.Text(c, model.Detail).FontSize(11).TextColor(theme.TextMuted)
		}
		mgo.Text(c, phaseLabel(model.Phase)).FontSize(12).TextColor(phaseColor(model.Phase, theme))
	})
	if model.CanModels {
		mgo.Row(c.Key("model-route")).Padding(0, 20, 8, 20).Gap(12).Children(func() {
			mgo.Text(c, model.ModelLabel).FontSize(12).TextColor(theme.TextMuted).SingleLine().Grow(1)
			mgo.Text(c, model.Detail).FontSize(11).TextColor(theme.TextMuted).SingleLine()
		})
	}
	if model.CanHistory && model.SessionID != "" {
		mgo.Row(c.Key("active-session")).Padding(0, 20, 8, 20).Gap(12).Children(func() {
			mgo.Text(c, sessionLabel(SessionSummary{ID: model.SessionID})).FontSize(11).TextColor(theme.TextMuted)
			mgo.Text(c, model.SessionCWD).FontSize(11).TextColor(theme.TextMuted).SingleLine()
		})
	}
	mgo.Divider(c)
}

func errorBanner(c *mgo.Context, model *Model) {
	theme := c.Theme()
	mgo.Column(c).Margin(12, 20).Padding(10, 12).Gap(4).Radius(8).
		Background(theme.Surface).Border(1, theme.Danger).Children(func() {
		mgo.Row(c).Gap(8).AlignItems(mgo.Center).Children(func() {
			mgo.Text(c, "Host error").FontSize(12).Bold().TextColor(theme.Danger)
			mgo.Spacer(c)
			if model.Dismiss != nil && mgo.Button(c, "Dismiss").Clicked() {
				model.Dismiss()
			}
		})
		banner := mgo.Column(c).Gap(4).Selectable()
		banner.Children(func() {
			mgo.Text(c, model.Error).FontSize(13)
			if model.ErrorDetail != "" {
				mgo.Text(c, model.ErrorDetail).FontSize(11).TextColor(theme.TextMuted)
			}
		})
	})
}

func transcript(c *mgo.Context, model *Model) {
	mgo.List(c.Key("transcript-"+model.SessionID), &model.List, len(model.Items), func(row int) {
		message(c, model, model.Items[row])
	}).Grow(1).Gap(10).Padding(16, 20).Justify(mgo.End).Children(func() {
		if len(model.Items) == 0 {
			theme := c.Theme()
			mgo.Column(c).Gap(4).Children(func() {
				mgo.Text(c, "Start a conversation").FontSize(14).Bold()
				mgo.Text(c, "Enter sends, Shift+Enter adds a line.").FontSize(12).TextColor(theme.TextMuted)
			})
		}
	})
}

func message(c *mgo.Context, model *Model, item projection.Item) {
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
		mgo.Text(c, item.Text).Key(item.ID).FontSize(12).TextColor(color).
			TextAlign(mgo.Center).FillWidth()
	}
}

func card(c *mgo.Context, model *Model, item projection.Item, fromUser bool) {
	theme := c.Theme()
	border := float32(0)
	borderColor := theme.Border
	if fromUser {
		border = 1
		borderColor = theme.Accent
	}
	container := mgo.Column(c).MaxWidth(680).Padding(10, 14).Gap(4).Radius(10).
		Background(theme.Surface).Border(border, borderColor)
	if fromUser {
		container.Selectable()
	}
	container.Children(func() {
		mgo.Row(c).Gap(8).Children(func() {
			mgo.Text(c, item.Title).FontSize(11).Bold().TextColor(theme.TextMuted)
			if item.Detail != "" {
				mgo.Text(c, item.Detail).FontSize(11).TextColor(theme.TextMuted)
			}
			mgo.Spacer(c)
			messageCopyButton(c, model, item, fromUser)
		})
		if item.Truncated {
			mgo.Text(c, "Truncated message: only the visible prefix is available.").FontSize(11).TextColor(theme.Warning)
		}
		if fromUser || item.Truncated {
			mgo.Text(c, item.Text).FontSize(13).Selectable()
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
	mgo.Column(c).MaxWidth(680).Padding(8, 12).Gap(4).Radius(8).
		Background(theme.Surface).Border(1, theme.Border).Selectable().Children(func() {
		mgo.Row(c).Gap(8).AlignItems(mgo.Center).Children(func() {
			mgo.Collapsible(c.Key("tool-disclosure"), item.Title, &model.toolCardState(item.ID).Expanded, func() {
				if item.Text != "" {
					mgo.Text(c, item.Text).FontSize(11).TextColor(theme.TextMuted)
				}
				if item.Detail != "" {
					mgo.Text(c, item.Detail).FontSize(12)
				}
				if mgo.Button(c.Key("tool-details"), "View details").Clicked() {
					model.OpenToolDetails(item.ID)
				}
			}).Grow(1)
			mgo.Spacer(c)
			if item.State != "" {
				mgo.Text(c, toolState(item.State)).FontSize(11).TextColor(stateColor)
			}
		})
	})
}

func composer(c *mgo.Context, model *Model) {
	theme := c.Theme()
	stopped := model.Phase == PhaseStopped || model.Phase == PhaseFailed || model.SessionSwitching
	canSend := model.Phase == PhaseIdle && model.Send != nil && len(model.Questions) == 0 && !model.SessionBusy && !model.SettingsOpen && !model.SettingsBusy && !model.ModelUncertain && !model.ConfigurationUncertain
	mgo.Column(c).Padding(12, 20, 16, 20).Gap(8).Children(func() {
		mgo.TextArea(c.Key("composer-"+model.SessionID), &model.Draft).Lines(1, 8).
			Label("Message").Placeholder("Message DeepSeek…").AutoFocus().Disabled(stopped).
			OnTextKey(func(key mgo.TextKey) bool {
				if key.Key != mgo.KeyEnter || key.Mods != 0 {
					return false
				}
				text := strings.TrimSpace(key.Text())
				if !canSend || text == "" {
					return false
				}
				model.Send(text)
				key.SetText("")
				return true
			})
		mgo.Row(c).Gap(8).AlignItems(mgo.Center).Children(func() {
			mgo.Text(c, composerHint(model)).FontSize(11).TextColor(theme.TextMuted)
			mgo.Spacer(c)
			if model.Phase == PhaseRunning && model.CanCancel && model.Cancel != nil {
				label := "Stop"
				if model.CancelPending {
					label = "Stopping…"
				}
				if mgo.Button(c.Key("stop"), label).Disabled(model.CancelPending).Clicked() && !model.CancelPending {
					model.Cancel()
				}
			} else {
				send := mgo.PrimaryButton(c.Key("send"), "Send").
					Disabled(!canSend || strings.TrimSpace(model.Draft) == "")
				if send.Clicked() {
					text := strings.TrimSpace(model.Draft)
					if text != "" && canSend {
						model.Send(text)
					}
				}
			}
		})
	})
}

func composerHint(model *Model) string {
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
	case PhaseStarting:
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
