package view

import mgo "github.com/egoist/mygo/ui"

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

func firstUseControls(c *mgo.Context, m *Model) {
	mgo.Column(c.Key("first-use")).FillWidth().MaxWidth(readingWidth).Gap(8).Children(func() {
		mgo.Text(c, m.T("Working folder")).FontSize(11).TextColor(c.Theme().TextMuted)
		mgo.Text(c, m.SessionCWD).FontSize(12).SingleLine().Tooltip(m.SessionCWD)
		mgo.Row(c).Gap(8).Children(func() {
			mgo.Button(c.Key("choose-folder"), m.T("Choose project folder")).Disabled(!m.CanChooseSession() || m.Setup.ChooseFolder == nil).OnClick(m.Setup.ChooseFolder)
			if m.Setup.WorkspaceRequired {
				mgo.Button(c.Key("use-folder"), m.T("Use this folder")).Disabled(!m.CanChooseSession() || m.Setup.UseFolder == nil).OnClick(m.Setup.UseFolder)
			}
		})
		if m.Setup.Picking {
			mgo.Text(c, m.T("Choosing a project folder…")).FontSize(11)
		}
		errorText := m.Setup.Error
		if errorText == "" {
			errorText = m.SessionsError
		}
		if errorText != "" {
			mgo.Text(c, m.T(errorText)).FontSize(11).TextColor(c.Theme().Danger)
		}
		if m.CanConfigure && m.Setup.Connect != nil {
			mgo.Row(c).Gap(8).AlignItems(mgo.Center).Children(func() {
				mgo.Button(c.Key("connect"), m.T("Connect DeepSeek")).Disabled(!m.CanEditSettings()).OnClick(m.Setup.Connect)
				status := "Add an API key to start"
				if m.ConfigurationReady && m.Configuration.Credential.Configured {
					status = "Key saved · ready to try"
				}
				if !m.ConfigurationReady {
					status = "Check your connection settings"
				}
				mgo.Text(c, m.T(status)).FontSize(11).TextColor(c.Theme().TextMuted)
			})
		}
	})
}
