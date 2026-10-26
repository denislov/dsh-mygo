package view

// CanSendPrompt reports whether business state allows a new prompt. Callers
// separately check transport availability and the current text or attachments.
func (model *Model) CanSendPrompt() bool {
	return model.Phase == PhaseIdle && !model.Runtime.SetupRequired && !model.SetupBlocked() &&
		!model.LocalState.Closing && !model.Inspection.Open && len(model.Questions) == 0 &&
		!model.SessionBusy && !model.History.Detached && !model.SessionPanel.BlocksInput() &&
		!model.SettingsOpen && !model.SettingsBusy && !model.ModelUncertain &&
		!model.ConfigurationUncertain && !model.Permission.Uncertain
}
