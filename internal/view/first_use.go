package view

// FirstUseState holds presentation and picker state, never credentials or history.
type FirstUseState struct {
	WorkspaceRequired, Picking       bool
	Error                            string
	ChooseFolder, UseFolder, Connect func()
}

func (m *Model) NeedsCredential() bool {
	return m.CanConfigure && m.ConfigurationReady && m.ActiveRoute.Provider == "deepseek-official" && m.Configuration.Credential.Available && !m.Configuration.Credential.Configured
}

func (m *Model) SetupBlocked() bool {
	return m.Setup.WorkspaceRequired || m.Setup.Picking || m.NeedsCredential()
}
