package view

import (
	"dsh-mygo/internal/interaction"
	"dsh-mygo/internal/projection"
	mgo "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// ChatState is the rendered conversation. It mirrors Host projections only;
// no field here is written back into history.
type ChatState struct {
	Items               []projection.Item
	Activity            map[string]*ActivityState
	History             HistoryState
	Usage               projection.UsageView
	Context             projection.ContextView
	Tasks               []projection.Task
	PlanActive          bool
	Goal                *projection.Goal
	Subagents           []SubagentState
	Inspection          SubagentPanelState
	List                mgo.ListState
	ToolCards           map[string]*ToolCardState
	Reasoning           map[string]*ActivityState
	ToolInspector       *ToolInspectorState
	Markdown            markdown.Options
	InterruptedMarkdown markdown.Options
	ReasoningMarkdown   markdown.Options
	CopiedMessageID     string
	CopiedMessageText   string
	CanHistory          bool
	CanResync           bool
}

// ComposerState owns the unsent input and the capabilities that gate it.
type ComposerState struct {
	Draft         string
	Attachments   AttachmentState
	Mention       MentionState
	CanCancel     bool
	CanStream     bool
	CancelPending bool
	ComposerActions
}

// SessionsState owns conversation navigation, catalog and management forms.
type SessionsState struct {
	SessionPanel     SessionPanelState
	SessionID        string
	SessionCWD       string
	Setup            FirstUseState
	Sessions         []SessionSummary
	SessionsOpen     bool
	SessionBusy      bool
	SessionSwitching bool
	SessionsError    string
	SessionList      mgo.ListState
	SessionActions
}

// SettingsState owns settings dialogs, negotiated edit capabilities and the
// credential form. Capabilities gate writes; a successful read is not a write
// confirmation.
type SettingsState struct {
	SettingsOpen                 bool
	SettingsSearch               string
	SettingsProviderEditing      bool
	QuickSelector                string
	QuickSearch                  string
	SettingsSection              SettingsSection
	SettingsConnection           bool
	SettingsBusy                 bool
	SettingsApplying             bool
	ModelUncertain               bool
	SettingsError                string
	SettingsProvider             string
	SettingsModel                string
	SettingsCatalogModel         string
	SettingsEffort               string
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
	SettingsActions
}

// ModelRouteState is the conversation's visible model route and the catalog
// offered by the Host.
type ModelRouteState struct {
	ModelLabel  string
	CanModels   bool
	ActiveRoute ModelRoute
	Catalog     ModelCatalog
}

// InteractionState owns approval and question queues. These take priority over
// navigation, settings and subagent inspection.
type InteractionState struct {
	CanApprove   bool
	ApprovalOpen bool
	Approvals    []interaction.Approval
	CanQuestion  bool
	QuestionOpen bool
	Questions    []*interaction.QuestionRequest
	InteractionActions
}

// RecoveryState owns the visible lifecycle and the recovery dialog.
type RecoveryState struct {
	Phase         Phase
	Error         string
	ErrorDetail   string
	RecoveryOpen  bool
	Recoverable   bool
	Restarting    bool
	RecoveryError string
	CanOpenLogs   bool
	RecoveryActions
}
