package i18n

// Named keys for application-owned prompts. The English text stays in this
// catalog so punctuation edits cannot change message identity. Technical error
// details, user text, tool output and model identifiers never use these keys.
const (
	MsgRuntimeInvalid = "app.runtime.invalid"

	MsgHostStart            = "app.host.start"
	MsgHostPrepare          = "app.host.prepare"
	MsgHostRestart          = "app.host.restart"
	MsgHostConnectionClosed = "app.host.connection-closed"
	MsgHostInitialize       = "app.host.initialize"
	MsgHostRecover          = "app.host.recover"
	MsgHostAgentFailure     = "app.host.agent-failure"
	MsgHostExited           = "app.host.exited"
	MsgHostLogsOpenFailed   = "app.host.logs-open-failed"
	MsgHostEventGap         = "app.host.event-gap"
	MsgHostInvalidNotify    = "app.host.invalid-notification"
	MsgHostInvalidEvent     = "app.host.invalid-event"
	MsgHostInvalidSequence  = "app.host.invalid-sequence"
	MsgHostInvalidStatus    = "app.host.invalid-status"
	MsgHostInvalidRevision  = "app.host.invalid-revision"
	MsgHostPromptFailed     = "app.host.prompt-failed"
	MsgHostStopFailed       = "app.host.stop-failed"

	MsgApprovalInvalidNotify = "app.approval.invalid-notification"
	MsgApprovalInvalid       = "app.approval.invalid-request"
	MsgApprovalUnexpected    = "app.approval.unexpected"
	MsgApprovalInvalidQueue  = "app.approval.invalid-queue"
	MsgApprovalSendFailed    = "app.approval.send-failed"
	MsgQuestionInvalidNotify = "app.question.invalid-notification"
	MsgQuestionInvalid       = "app.question.invalid-request"
	MsgQuestionUnexpected    = "app.question.unexpected"
	MsgQuestionInvalidQueue  = "app.question.invalid-queue"
	MsgQuestionSendFailed    = "app.question.send-failed"
	MsgStreamInvalidNotify   = "app.stream.invalid-notification"
	MsgStreamUnexpected      = "app.stream.unexpected"
	MsgStreamInvalid         = "app.stream.invalid"

	MsgSettingsInvalidRoute      = "app.settings.invalid-route"
	MsgSettingsInvalidCredential = "app.settings.invalid-credential"
	MsgSettingsWriteUnconfirmed  = "app.settings.write-unconfirmed"
	MsgSettingsSaved             = "app.settings.saved"
	MsgPermissionsUnconfirmed    = "app.permissions.unconfirmed"
	MsgPermissionsSaved          = "app.permissions.saved"
	MsgPermissionsChanged        = "app.permissions.changed"
	MsgSettingsRefreshRoute      = "app.settings.refresh-route"
	MsgSettingsApplyFailed       = "app.settings.apply-failed"
	MsgPermissionsEventsMissing  = "app.permissions.events-missing"
	MsgModelSelectionMissing     = "app.model.selection-missing"

	MsgSessionOpenFailed         = "app.session.open-failed"
	MsgSessionInvalidResponse    = "app.session.invalid-response"
	MsgSessionInvalidRestore     = "app.session.invalid-restore"
	MsgSessionPermissionBoundary = "app.session.permission-boundary"
	MsgSessionChanged            = "app.session.changed"
	MsgSessionFinishInteraction  = "app.session.finish-interaction"
	MsgSessionConcurrent         = "app.session.concurrent"
	MsgSessionTooManyUpdates     = "app.session.too-many-updates"
	MsgSessionTitleInvalid       = "app.session.title-invalid"
	MsgSessionConfirmFailed      = "app.session.confirm-failed"
	MsgHistoryChanged            = "app.history.changed"
	MsgResyncFailed              = "app.resync.failed"
	MsgSubagentsLoadFailed       = "app.subagents.load-failed"

	MsgRuntimeNoDSH             = "app.runtime.no-dsh"
	MsgRuntimeProbeFailed       = "app.runtime.probe-failed"
	MsgRuntimeInstalled         = "app.runtime.installed"
	MsgRuntimeInstalledSettings = "app.runtime.installed-settings"
	MsgRuntimeNoCompatible      = "app.runtime.no-compatible"
	MsgRuntimeAddedSettings     = "app.runtime.added-settings"
	MsgRuntimeLegacyUnusable    = "app.runtime.legacy-unusable"
	MsgRuntimeMissing           = "app.runtime.missing"
	MsgRuntimeAlreadySelected   = "app.runtime.already-selected"
	MsgRuntimeDirectFixed       = "app.runtime.direct-fixed"
	MsgRuntimeStartFailed       = "app.runtime.start-failed"
	MsgRuntimeSwitchFailed      = "app.runtime.switch-failed"
	MsgRuntimeRemoveCurrent     = "app.runtime.remove-current"
	MsgRuntimeRemoved           = "app.runtime.removed"
	MsgRuntimeCancelled         = "app.runtime.cancelled"
	MsgRuntimeRequired          = "app.runtime.required"
	MsgRuntimeChecksumFailed    = "app.runtime.checksum-failed"
	MsgRuntimeOperationFailed   = "app.runtime.operation-failed"
	MsgRuntimeSwitched          = "app.runtime.switched"
	MsgRuntimeInstalling        = "app.runtime.installing"
	MsgRuntimeCheckingReleases  = "app.runtime.checking-releases"
	MsgRuntimeDownloading       = "app.runtime.downloading"
	MsgRuntimeCheckingInstalled = "app.runtime.checking-installed"
	MsgRuntimeRemoving          = "app.runtime.removing"
	MsgRuntimeManaged           = "app.runtime.managed"
	MsgRuntimeExisting          = "app.runtime.existing"
	MsgRuntimeBundledPrevious   = "app.runtime.bundled-previous"
	MsgRuntimeDevelopment       = "app.runtime.development"
	MsgRuntimeBundleLabel       = "app.runtime.bundle-label"
	MsgRuntimeChooseCommand     = "app.runtime.choose-command"
	MsgRuntimeChooseBundle      = "app.runtime.choose-bundle"

	MsgSetupFolderFailed     = "app.setup.folder-failed"
	MsgLocalPreviousDraft    = "app.local.previous-draft"
	MsgLocalRestoreFailed    = "app.local.restore-failed"
	MsgWorkspaceTooManyFiles = "app.workspace.too-many-folders"
	MsgWorkspaceImageFailed  = "app.workspace.image-preview-failed"
	MsgWorkspaceTerminal     = "app.workspace.terminal-failed"
)

var english = map[string]string{
	MsgRuntimeInvalid: "The selected DSH runtime could not be used. Choose another runtime or install a new one.",

	MsgHostStart:            "Could not start the DeepSeek Harness host",
	MsgHostPrepare:          "Could not prepare the DeepSeek Harness profile",
	MsgHostRestart:          "Could not restart the DeepSeek Harness host",
	MsgHostConnectionClosed: "The host connection closed",
	MsgHostInitialize:       "Could not initialize the host",
	MsgHostRecover:          "Could not recover the session",
	MsgHostAgentFailure:     "The host reported an agent failure",
	MsgHostExited:           "The DeepSeek Harness host exited",
	MsgHostLogsOpenFailed:   "Could not open the logs folder",
	MsgHostEventGap:         "The host event stream has a gap",
	MsgHostInvalidNotify:    "Invalid host notification",
	MsgHostInvalidEvent:     "Invalid host event",
	MsgHostInvalidSequence:  "Invalid session event sequence",
	MsgHostInvalidStatus:    "Invalid host status",
	MsgHostInvalidRevision:  "Invalid host status revision",
	MsgHostPromptFailed:     "Could not deliver the prompt",
	MsgHostStopFailed:       "Could not stop the turn",

	MsgApprovalInvalidNotify: "Invalid approval notification",
	MsgApprovalInvalid:       "Invalid approval request",
	MsgApprovalUnexpected:    "Unexpected approval request",
	MsgApprovalInvalidQueue:  "Invalid approval queue",
	MsgApprovalSendFailed:    "Could not send the decision. Retry or stop the turn.",
	MsgQuestionInvalidNotify: "Invalid question notification",
	MsgQuestionInvalid:       "Invalid question request",
	MsgQuestionUnexpected:    "Unexpected question request",
	MsgQuestionInvalidQueue:  "Invalid question queue",
	MsgQuestionSendFailed:    "Could not send the answer. Retry or cancel the question.",
	MsgStreamInvalidNotify:   "Invalid stream notification",
	MsgStreamUnexpected:      "Unexpected assistant stream",
	MsgStreamInvalid:         "Invalid assistant stream",

	MsgSettingsInvalidRoute:      "Choose a valid provider, model ID and effort",
	MsgSettingsInvalidCredential: "Enter a non-empty API key without whitespace (maximum 8192 bytes)",
	MsgSettingsWriteUnconfirmed:  "Could not confirm the configuration write. Refresh before continuing or retrying",
	MsgSettingsSaved:             "Configuration saved. Existing session model is unchanged.",
	MsgPermissionsUnconfirmed:    "Could not confirm permissions. Refresh before continuing or retrying",
	MsgPermissionsSaved:          "Permissions saved for this conversation.",
	MsgPermissionsChanged:        "Session permissions changed. Refresh to retry",
	MsgSettingsRefreshRoute:      "Refresh to confirm the active route before retrying",
	MsgSettingsApplyFailed:       "Could not apply the model. Refresh to confirm the active route before retrying",
	MsgPermissionsEventsMissing:  "Permission events are missing",
	MsgModelSelectionMissing:     "Model selection event is missing",

	MsgSessionOpenFailed:         "Could not open the session. Check ownership, working directory and model route, then retry",
	MsgSessionInvalidResponse:    "Invalid session response",
	MsgSessionInvalidRestore:     "Invalid restored session identity or route",
	MsgSessionPermissionBoundary: "Permission boundary changed while loading the session",
	MsgSessionChanged:            "The session changed while history was loading",
	MsgSessionFinishInteraction:  "Finish the current interaction before switching sessions",
	MsgSessionConcurrent:         "Concurrent session activity is not supported",
	MsgSessionTooManyUpdates:     "Too many session updates while loading history",
	MsgSessionTitleInvalid:       "Enter a title of up to 1024 bytes without control characters",
	MsgSessionConfirmFailed:      "Could not confirm the change. Refresh sessions before continuing",
	MsgHistoryChanged:            "History changed. Refresh the latest messages to retry",
	MsgResyncFailed:              "Could not synchronize session history",
	MsgSubagentsLoadFailed:       "Could not load subagents. Refresh to retry",

	MsgRuntimeNoDSH:             "No installed DSH was found. Download a runtime bundle or choose a dsh command.",
	MsgRuntimeProbeFailed:       "The selected dsh command could not be used. Check Node.js 24+ and the DSH version.",
	MsgRuntimeInstalled:         "Runtime installed.",
	MsgRuntimeInstalledSettings: "Runtime installed. Use it to switch.",
	MsgRuntimeNoCompatible:      "No compatible runtime bundle is available for this app version.",
	MsgRuntimeAddedSettings:     "Runtime added. Use it to switch.",
	MsgRuntimeLegacyUnusable:    "The runtime bundled with the previous installation is no longer usable.",
	MsgRuntimeMissing:           "The selected runtime is no longer installed.",
	MsgRuntimeAlreadySelected:   "This runtime is already selected.",
	MsgRuntimeDirectFixed:       "The development runtime is fixed for this launch.",
	MsgRuntimeStartFailed:       "Could not start this runtime. Choose another runtime or install a new one.",
	MsgRuntimeSwitchFailed:      "Could not switch runtime. The previous runtime was restored.",
	MsgRuntimeRemoveCurrent:     "Switch to another runtime before removing this one.",
	MsgRuntimeRemoved:           "Runtime removed.",
	MsgRuntimeCancelled:         "Runtime operation cancelled.",
	MsgRuntimeRequired:          "A DSH runtime is required before the host can start.",
	MsgRuntimeChecksumFailed:    "The runtime file failed verification. Try again or import a local bundle.",
	MsgRuntimeOperationFailed:   "Could not complete the runtime operation. Check the logs and try again.",
	MsgRuntimeSwitched:          "Runtime switched.",
	MsgRuntimeInstalling:        "Installing runtime",
	MsgRuntimeCheckingReleases:  "Checking runtime releases",
	MsgRuntimeDownloading:       "Downloading runtime",
	MsgRuntimeCheckingInstalled: "Checking installed DSH",
	MsgRuntimeRemoving:          "Removing runtime",
	MsgRuntimeManaged:           "Managed runtime",
	MsgRuntimeExisting:          "Existing DSH installation",
	MsgRuntimeBundledPrevious:   "Runtime bundled with a previous installation",
	MsgRuntimeDevelopment:       "Development runtime",
	MsgRuntimeBundleLabel:       "Runtime bundle",
	MsgRuntimeChooseCommand:     "Choose dsh command",
	MsgRuntimeChooseBundle:      "Choose runtime bundle",

	MsgSetupFolderFailed:     "Could not open the project folder. Choose an existing folder and retry",
	MsgLocalPreviousDraft:    "Previous draft copied here. Review it before sending.",
	MsgLocalRestoreFailed:    "Could not reopen the last conversation. Its draft is kept; use Sessions to retry or continue in this new conversation.",
	MsgWorkspaceTooManyFiles: "Too many open folders",
	MsgWorkspaceImageFailed:  "Image cannot be previewed",
	MsgWorkspaceTerminal:     "Could not start terminal",
}
