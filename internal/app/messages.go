package app

import "dsh-mygo/internal/i18n"

// Application-owned prompts use stable message keys; transport/validation
// details stay raw because they are diagnostics, not localized copy.
const (
	msgRuntimeInvalid = i18n.MsgRuntimeInvalid

	msgHostStart            = i18n.MsgHostStart
	msgHostPrepare          = i18n.MsgHostPrepare
	msgHostRestart          = i18n.MsgHostRestart
	msgHostConnectionClosed = i18n.MsgHostConnectionClosed
	msgHostInitialize       = i18n.MsgHostInitialize
	msgHostRecover          = i18n.MsgHostRecover
	msgHostAgentFailure     = i18n.MsgHostAgentFailure
	msgHostExited           = i18n.MsgHostExited
	msgHostLogsOpenFailed   = i18n.MsgHostLogsOpenFailed
	msgHostEventGap         = i18n.MsgHostEventGap
	msgHostInvalidNotify    = i18n.MsgHostInvalidNotify
	msgHostInvalidEvent     = i18n.MsgHostInvalidEvent
	msgHostInvalidSequence  = i18n.MsgHostInvalidSequence
	msgHostInvalidStatus    = i18n.MsgHostInvalidStatus
	msgHostInvalidRevision  = i18n.MsgHostInvalidRevision
	msgHostPromptFailed     = i18n.MsgHostPromptFailed
	msgHostStopFailed       = i18n.MsgHostStopFailed

	msgApprovalInvalidNotify = i18n.MsgApprovalInvalidNotify
	msgApprovalInvalid       = i18n.MsgApprovalInvalid
	msgApprovalUnexpected    = i18n.MsgApprovalUnexpected
	msgApprovalInvalidQueue  = i18n.MsgApprovalInvalidQueue
	msgApprovalSendFailed    = i18n.MsgApprovalSendFailed
	msgQuestionInvalidNotify = i18n.MsgQuestionInvalidNotify
	msgQuestionInvalid       = i18n.MsgQuestionInvalid
	msgQuestionUnexpected    = i18n.MsgQuestionUnexpected
	msgQuestionInvalidQueue  = i18n.MsgQuestionInvalidQueue
	msgQuestionSendFailed    = i18n.MsgQuestionSendFailed
	msgStreamInvalidNotify   = i18n.MsgStreamInvalidNotify
	msgStreamUnexpected      = i18n.MsgStreamUnexpected
	msgStreamInvalid         = i18n.MsgStreamInvalid

	msgSettingsInvalidRoute      = i18n.MsgSettingsInvalidRoute
	msgSettingsInvalidCredential = i18n.MsgSettingsInvalidCredential
	msgSettingsWriteUnconfirmed  = i18n.MsgSettingsWriteUnconfirmed
	msgSettingsSaved             = i18n.MsgSettingsSaved
	msgPermissionsUnconfirmed    = i18n.MsgPermissionsUnconfirmed
	msgPermissionsSaved          = i18n.MsgPermissionsSaved
	msgPermissionsChanged        = i18n.MsgPermissionsChanged
	msgSettingsRefreshRoute      = i18n.MsgSettingsRefreshRoute
	msgSettingsApplyFailed       = i18n.MsgSettingsApplyFailed
	msgPermissionsEventsMissing  = i18n.MsgPermissionsEventsMissing
	msgModelSelectionMissing     = i18n.MsgModelSelectionMissing

	msgSessionOpenFailed         = i18n.MsgSessionOpenFailed
	msgSessionInvalidResponse    = i18n.MsgSessionInvalidResponse
	msgSessionInvalidRestore     = i18n.MsgSessionInvalidRestore
	msgSessionPermissionBoundary = i18n.MsgSessionPermissionBoundary
	msgSessionChanged            = i18n.MsgSessionChanged
	msgSessionFinishInteraction  = i18n.MsgSessionFinishInteraction
	msgSessionConcurrent         = i18n.MsgSessionConcurrent
	msgSessionTooManyUpdates     = i18n.MsgSessionTooManyUpdates
	msgSessionTitleInvalid       = i18n.MsgSessionTitleInvalid
	msgSessionConfirmFailed      = i18n.MsgSessionConfirmFailed
	msgHistoryChanged            = i18n.MsgHistoryChanged
	msgResyncFailed              = i18n.MsgResyncFailed
	msgSubagentsLoadFailed       = i18n.MsgSubagentsLoadFailed

	msgRuntimeNoDSH             = i18n.MsgRuntimeNoDSH
	msgRuntimeProbeFailed       = i18n.MsgRuntimeProbeFailed
	msgRuntimeInstalled         = i18n.MsgRuntimeInstalled
	msgRuntimeInstalledSettings = i18n.MsgRuntimeInstalledSettings
	msgRuntimeNoCompatible      = i18n.MsgRuntimeNoCompatible
	msgRuntimeAddedSettings     = i18n.MsgRuntimeAddedSettings
	msgRuntimeLegacyUnusable    = i18n.MsgRuntimeLegacyUnusable
	msgRuntimeMissing           = i18n.MsgRuntimeMissing
	msgRuntimeAlreadySelected   = i18n.MsgRuntimeAlreadySelected
	msgRuntimeDirectFixed       = i18n.MsgRuntimeDirectFixed
	msgRuntimeStartFailed       = i18n.MsgRuntimeStartFailed
	msgRuntimeSwitchFailed      = i18n.MsgRuntimeSwitchFailed
	msgRuntimeRemoveCurrent     = i18n.MsgRuntimeRemoveCurrent
	msgRuntimeRemoved           = i18n.MsgRuntimeRemoved
	msgRuntimeCancelled         = i18n.MsgRuntimeCancelled
	msgRuntimeRequired          = i18n.MsgRuntimeRequired
	msgRuntimeChecksumFailed    = i18n.MsgRuntimeChecksumFailed
	msgRuntimeOperationFailed   = i18n.MsgRuntimeOperationFailed
	msgRuntimeSwitched          = i18n.MsgRuntimeSwitched
	msgRuntimeInstalling        = i18n.MsgRuntimeInstalling
	msgRuntimeCheckingReleases  = i18n.MsgRuntimeCheckingReleases
	msgRuntimeDownloading       = i18n.MsgRuntimeDownloading
	msgRuntimeCheckingInstalled = i18n.MsgRuntimeCheckingInstalled
	msgRuntimeRemoving          = i18n.MsgRuntimeRemoving
	msgRuntimeManaged           = i18n.MsgRuntimeManaged
	msgRuntimeExisting          = i18n.MsgRuntimeExisting
	msgRuntimeBundledPrevious   = i18n.MsgRuntimeBundledPrevious
	msgRuntimeDevelopment       = i18n.MsgRuntimeDevelopment
	msgRuntimeBundleLabel       = i18n.MsgRuntimeBundleLabel
	msgRuntimeChooseCommand     = i18n.MsgRuntimeChooseCommand
	msgRuntimeChooseBundle      = i18n.MsgRuntimeChooseBundle

	msgSetupFolderFailed     = i18n.MsgSetupFolderFailed
	msgLocalPreviousDraft    = i18n.MsgLocalPreviousDraft
	msgLocalRestoreFailed    = i18n.MsgLocalRestoreFailed
	msgWorkspaceTooManyFiles = i18n.MsgWorkspaceTooManyFiles
	msgWorkspaceImageFailed  = i18n.MsgWorkspaceImageFailed
	msgWorkspaceTerminal     = i18n.MsgWorkspaceTerminal
)
