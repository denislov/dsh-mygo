// Package view renders the dsh-mygo native window from a small UI model.
// It owns no host or session state: the app updates Model and MyGo rebuilds
// the view on the window's main thread.
package view

import (
	"fmt"

	mgo "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

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
	// PhaseAwaitingRuntime means the app needs a DSH runtime before it can start
	// the host. It is appended so the zero value keeps meaning PhaseStarting.
	PhaseAwaitingRuntime
)

// RuntimeState is device-local runtime selection and setup progress. It never
// holds host session facts or MyGo elements.
type RuntimeState struct {
	SetupRequired bool
	Checking      bool
	Busy          bool
	Switching     bool
	CanManage     bool
	Progress      float32
	Status        string
	Error         string
	Notice        string
	RemoveConfirm string
	Current       RuntimeChoice
	Installed     []RuntimeChoice
	Candidates    []RuntimeChoice
	Available     []RuntimeChoice

	Detect       func()
	BrowseDSH    func()
	CheckUpdates func()
	Download     func(id string)
	InstallFile  func()
	UseExternal  func(command string)
	UseInstalled func(id string)
	UseLegacy    func()
	Activate     func(id string)
	Remove       func(id string)
	Manage       func()
	Cancel       func()
}

// RuntimeChoice is a display-only runtime or candidate.
type RuntimeChoice struct {
	ID          string
	Kind        string
	Label       string
	DSH         string
	Node        string
	HostVersion string
	Path        string
	Detail      string
	Compatible  bool
	Reason      string
}

// Model is the window's state. The app mutates it on the main thread and
// keeps the transcript and draft outside the view.
//
// Region states are embedded so existing bindings keep their addresses while
// ownership stays readable: Chat/Composer/Sessions/Settings/Interaction/
// Recovery own their fields, and device resources stay in named substates.
type Model struct {
	ChatState
	ComposerState
	SessionsState
	SettingsState
	ModelRouteState
	InteractionState
	RecoveryState

	Workspace  WorkspaceState
	Shell      ShellState
	Prefs      PreferencesState
	LocalState LocalStateStatus
	Runtime    RuntimeState
	AppVersion string
	Detail     string
}

// NewModel returns a model with a list that follows the newest row and keeps
// row state by item ID.
func NewModel() *Model {
	model := &Model{}
	model.Phase = PhaseStarting
	model.Markdown = markdown.Options{Theme: &markdown.Theme{FontSize: textReading, LineHeight: 1.62}, Reveal: &markdown.Reveal{Unit: markdown.AtOnce, Fade: -1}}
	model.InterruptedMarkdown = markdown.Options{Theme: &markdown.Theme{FontSize: textReading, LineHeight: 1.62}}
	model.ReasoningMarkdown = markdown.Options{Theme: &markdown.Theme{FontSize: 13.5, LineHeight: 1.55}, Reveal: &markdown.Reveal{Unit: markdown.AtOnce, Fade: -1}}
	model.List.FollowEnd = true
	model.Markdown.CodeBlock = nativeCodeBlock
	model.ReasoningMarkdown.CodeBlock = nativeCodeBlock
	model.InterruptedMarkdown.CodeBlock = model.interruptedCodeBlock
	model.Prefs = PreferencesState{Value: preferences.DefaultUI(), Ready: true}
	model.Shell.SidebarOpen = true
	model.Shell.ProgressOpen = true
	model.Shell.FolderOpen = true
	model.Shell.ContextOpen = true
	model.Workspace.ChatPercent = 56
	model.Mention.CaretTarget = -1
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
	if model.Runtime.SetupRequired {
		runtimeSetupView(c, model)
		return
	}
	applicationShortcuts(c, model)
	theme := c.Theme()
	services := c.Services()
	configureMarkdown(c, model, services)
	mgo.Row(c.Key("shell")).Fill().Background(theme.Background).Children(func() {
		if sidebarVisible(c, model) {
			sessionSidebar(c, model)
		}
		mgo.Column(c.Key("conversation")).Grow(1).MinWidth(0).FillHeight().Children(func() {
			titleBar(c, model)
			mgo.Row(c.Key("conversation-body")).Grow(1).MinHeight(0).FillWidth().Children(func() {
				panels := model.workspacePanels()
				width, _ := c.Size()
				chat := func() {
					mgo.Column(c.Key("chat")).Grow(1).MinWidth(0).FillHeight().Children(func() {
						if !model.RecoveryOpen {
							localStateStatus(c, model)
						}
						if model.Error != "" && !model.RecoveryOpen {
							errorBanner(c, model)
						}
						if model.SessionSwitching {
							sessionLoading(c, model)
						} else {
							transcript(c, model)
						}
						if !model.Workspace.Open[PanelRail] {
							taskPanel(c, model)
						}
						composer(c, model)
					})
				}
				conversation := func() { conversationWithRail(c, model, chat) }
				if len(panels) == 0 {
					conversation()
				} else if width < 1000 {
					workspaceDock(c, model, panels, true)
				} else {
					workspaceSplit(c.Key("workspace-split"), model, &model.Workspace.ChatPercent, false, model.T("Resize tools"), conversation, func() { workspaceDock(c, model, panels, false) })
				}
			})
		})
	})
	floatingSessionRail(c, model, c.Resolve(model.Workspace.RailButton))
	approvalModal(c, model)
	questionModal(c, model)
	toolInspectorModal(c, model)
	sessionsModal(c, model)
	settingsCenterModal(c, model)
	recoveryModal(c, model)
	subagentModal(c, model)
	model.runPendingActions(c)
}

// configureMarkdown refreshes theme and link handling for this build. The
// callback is persistent on the model, but it captures the current window
// Services; no frame-scoped Element or Context is retained.
func configureMarkdown(c *mgo.Context, model *Model, services mgo.Services) {
	model.Markdown.Theme = markdownTheme(c)
	model.InterruptedMarkdown.Theme = model.Markdown.Theme
	model.ReasoningMarkdown.Theme = reasoningMarkdownTheme(c)
	model.Markdown.OnLink = func(address string) {
		if safeMarkdownLink(address) {
			services.OpenURL(address)
		}
	}
	model.InterruptedMarkdown.OnLink = model.Markdown.OnLink
	model.ReasoningMarkdown.OnLink = model.Markdown.OnLink
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
	rows := buildTranscriptRows(model.Items)
	if len(rows) == 0 && model.History.Start == 0 {
		mgo.Scroll(c.Key("empty-transcript-"+model.SessionID)).Grow(1).MinHeight(0).FillWidth().Padding(space20, space24).Children(func() { emptyConversation(c, model) })
		return
	}
	model.List.Key = func(row int) any { return rows[row].key }
	mgo.List(c.Key("transcript-"+model.SessionID), &model.List, len(rows), func(row int) {
		if group := rows[row].activity; group != nil {
			mgo.Row(c.Key("activity-row-" + group.key)).FillWidth().Justify(mgo.Center).Children(func() { activityCard(c, model, group) })
			return
		}
		message(c, model, *rows[row].item)
	}).Grow(1).MinHeight(0).TrackScroll(&model.History.scroll).Gap(space24).Padding(space20, space24).Children(func() {
		if len(rows) == 0 {
			if model.History.Start > 0 {
				mgo.Text(c, model.T("No visible messages in this history window.")).FontSize(13)
			} else {
				emptyConversation(c, model)
			}
		}
	})
	autoLoadHistory(c, model, len(rows))
}

func message(c *mgo.Context, model *Model, item projection.Item) {
	mgo.Row(c.Key(item.ID)).FillWidth().Justify(mgo.Center).Children(func() {
		mgo.Column(c).FillWidth().MaxWidth(readingWidth).Children(func() { transcriptItem(c, model, item) })
	})
}

func markdownTheme(c *mgo.Context) *markdown.Theme {
	theme := c.Theme()
	tokens := tokensFor(theme)
	options := markdown.DefaultTheme(theme)
	options.FontSize = textReading
	options.LineHeight = 1.62
	options.BlockSpacing = 0.9
	options.CodeBackground = tokens.CodeBackground
	options.CodeBlockBackground = tokens.CodeBackground
	options.CodeBlockBorder = tokens.Border
	options.CodeBlockRadius = radiusMd
	options.QuoteBar = tokens.Border
	options.QuoteBarWidth = 3
	options.Rule = tokens.Border
	options.Mark = tokens.AccentSoft
	return options
}

// reasoningMarkdownTheme keeps thinking text secondary to the answer while
// retaining the same safe link and code treatment.
func reasoningMarkdownTheme(c *mgo.Context) *markdown.Theme {
	theme := c.Theme()
	tokens := tokensFor(theme)
	options := markdown.DefaultTheme(theme)
	options.FontSize = 13.5
	options.LineHeight = 1.55
	options.BlockSpacing = 0.75
	options.Text = tokens.ReasoningText
	options.Muted = tokens.TextFaint
	options.CodeBackground = tokens.CodeBackground
	options.CodeBlockBackground = tokens.CodeBackground
	options.CodeBlockBorder = tokens.Border
	options.CodeBlockRadius = radiusSm
	options.QuoteBar = tokens.ReasoningRail
	options.Rule = tokens.Border
	options.Mark = tokens.AccentSoft
	return options
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
			toolCard(c, model, item, false)
		})
	case projection.KindReasoning:
		mgo.Row(c).Key(item.ID).FillWidth().Children(func() {
			reasoningBlock(c, model, item)
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
	tokens := tokensFor(theme)
	container := mgo.Column(c).FillWidth().MaxWidth(readingWidth).Gap(space4)
	if fromUser {
		container.AlignItems(mgo.End)
	}
	container.Label(model.T(item.Title))
	var action mgo.Element
	hasAction := false
	container.Children(func() {
		body := mgo.Column(c.Key("message-body")).Gap(space8)
		if fromUser {
			body.MaxWidth(620).Padding(space10, space16).Radius(radiusBubble).Background(tokens.UserBubble).Selectable()
		} else {
			body.FillWidth()
		}
		body.Children(func() {
			if item.Detail != "" {
				mgo.Row(c).Gap(space6).AlignItems(mgo.Center).Children(func() {
					if item.State == "streaming" {
						mgo.Spinner(c.Key("streaming-spinner-"+item.ID)).Size(10, 10)
					}
					color := tokens.TextMuted
					if item.State == "interrupted" {
						color = tokens.Warning
					}
					mgo.Text(c, model.T(item.Detail)).FontSize(textMicro).TextColor(color).
						ContentTransition(mgo.ContentTransition{})
				})
			}
			if len(item.Attachments) > 0 {
				mgo.Row(c).Wrap().Gap(space4).Children(func() {
					for _, attachment := range item.Attachments {
						mgo.Badge(c, attachmentLabel(model, attachment)).FontSize(textMicro).
							Padding(space2, space8).Radius(radiusSm).
							Background(tokens.Surface).Border(1, tokens.Border).TextColor(tokens.TextMuted)
					}
				})
			}
			if item.Truncated {
				mgo.Text(c, model.T("Truncated message: only the visible prefix is available.")).
					FontSize(textMicro).TextColor(tokens.Warning)
			}
			if fromUser || item.Truncated {
				if item.Text != "" {
					color := theme.Text
					if fromUser {
						color = tokens.UserBubbleText
					}
					mgo.Text(c, item.Text).FontSize(textBody).LineHeight(1.55).TextColor(color).Selectable()
				}
			} else if item.State == "streaming" {
				markdown.Stream(c.Key("assistant-markdown-"+item.ID), item.Text, &model.Markdown).FillWidth()
			} else if item.State == "interrupted" {
				markdown.View(c.Key("assistant-markdown-"+item.ID), item.Text, &model.InterruptedMarkdown).FillWidth()
			} else {
				markdown.View(c.Key("assistant-markdown-"+item.ID), item.Text, &model.Markdown).FillWidth()
			}
		})
		if item.Text != "" {
			align := mgo.Start
			if fromUser {
				align = mgo.End
			}
			mgo.Row(c.Key("message-actions")).FillWidth().Justify(align).Children(func() {
				action = messageCopyButton(c, model, item, fromUser)
				hasAction = true
			})
		}
	})
	if hasAction && !container.Hovered() && !container.FocusWithin() {
		action.Opacity(0)
	}
}

func toolCard(c *mgo.Context, model *Model, item projection.Item, nested bool) {
	tokens := tokensFor(c.Theme())
	state := model.toolCardState(item.ID)
	summary := toolSummary(item)
	var card mgo.Element
	if nested {
		card = mgo.Column(c.Key("tool-card-"+item.ID)).FillWidth().Padding(space4, space8).Gap(space4).
			Radius(radiusSm).Transition(quietTransition).Selectable()
		if card.Hovered() {
			card.Background(tokens.ActivityHover)
		}
	} else {
		card = surfaceCard(c, "tool-card-"+item.ID).FillWidth().MaxWidth(readingWidth).
			Padding(space8, space12).Gap(space4).Selectable()
	}
	card.Children(func() {
		disclosure := mgo.CollapsibleBase(c, &state.Expanded)
		disclosure.Trigger.FillWidth().Gap(space8).AlignItems(mgo.Center).Padding(space2, 0).
			Label(model.toolTitle(item.Title)).Tooltip(summary)
		disclosure.Trigger.Children(func() {
			mgo.Icon(c, toolIcon(item.Title)).Size(15, 15).TextColor(tokens.TextMuted)
			mgo.Text(c, model.T(toolAction(item.Title))).FontSize(textCaption).TextColor(tokens.TextMuted).SingleLine()
			if summary != "" {
				mgo.Text(c, "·").FontSize(textCaption).TextColor(tokens.TextFaint)
				mgo.Text(c, summary).FontSize(textCaption).TextColor(tokens.TextMuted).SingleLine().Grow(1).MinWidth(0)
			} else {
				mgo.Spacer(c)
			}
			if item.IsError || item.State == "error" {
				toolStatusPill(c, model, item)
			} else if item.State == "running" {
				mgo.Spinner(c.Key("tool-running-"+item.ID)).Size(10, 10)
			}
			mgo.Icon(c, iconChevronRight).Size(iconSmall, iconSmall).TextColor(tokens.TextMuted).
				Rotate(90 * disclosure.Progress())
		})
		disclosure.Panel(func() {
			mgo.Column(c).Gap(space4).Padding(space8, 0, space2, 0).Children(func() {
				if item.Text != "" {
					mgo.Text(c, item.Text).FontSize(textMicro).TextColor(tokens.TextMuted)
				}
				if item.Detail != "" {
					mgo.Text(c, item.Detail).FontSize(textCaption).TextColor(tokens.Text)
				}
				details := mgo.Button(c.Key("tool-details-"+item.ID), model.T("View details")).Padding(space2, space6).
					Radius(radiusXs).Background(mgo.Transparent).TextColor(tokens.TextMuted).Transition(quietTransition)
				if details.Hovered() {
					details.Background(tokens.SurfaceHover)
				}
				if details.Clicked() {
					model.OpenToolDetails(item.ID)
				}
			})
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
	case PhaseAwaitingRuntime:
		return "Setup required"
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
	case PhaseStarting, PhaseResyncing, PhaseAwaitingRuntime:
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
