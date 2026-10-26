package view

// Action structs are the optional callbacks each region consumes. They stay
// small enough to fake independently; capability fields remain separate so a
// missing callback can never imply that a Host write is allowed.

// ComposerActions are explicit user actions from the input area.
type ComposerActions struct {
	DraftChanged  func()
	QueryMentions func(prefix string)
	Send          func(text string)
	Cancel        func()
}

// SessionActions navigate and manage Host-owned conversations.
type SessionActions struct {
	OpenSessions      func()
	RefreshSessions   func()
	CloseSessions     func()
	NewSession        func()
	NewProjectSession func(string)
	SwitchSession     func(string)
}

// SettingsActions read or write Host settings; each remains gated by its
// negotiated capability and Ready/Uncertain state.
type SettingsActions struct {
	OpenSettings     func()
	CloseSettings    func()
	RefreshSettings  func()
	ApplySettings    func()
	SaveDefaultModel func()
	SaveCredential   func()
	ClearCredential  func()
}

// InteractionActions answer Host approval and question requests.
type InteractionActions struct {
	RespondApproval func(requestID, decision string)
	RespondQuestion func(requestID string, cancel bool)
}

// RecoveryActions leave or recover from a failed Host.
type RecoveryActions struct {
	Restart  func()
	OpenLogs func()
	Quit     func()
	Dismiss  func()
}
