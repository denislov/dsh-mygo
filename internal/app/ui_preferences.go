package app

import "dsh-mygo/internal/preferences"

func (a *App) currentLocale() string {
	if value := a.hostLocale.Load(); value != nil {
		return value.(string)
	}
	return "en-US"
}
func (a *App) acceptUIPreferences(settings preferences.UISettings) {
	a.model.Prefs.Value = settings
	a.hostLocale.Store(settings.Locale)
	if a.options.ApplyTheme != nil {
		a.options.ApplyTheme(settings.Theme)
	}
	if a.options.ApplyLocale != nil {
		a.options.ApplyLocale(settings.Locale)
	}
}
func (a *App) ApplyUIPreferences() {
	if !a.model.SettingsOpen || a.model.SettingsConnection || a.model.LocalState.Closing || a.model.RecoveryOpen || len(a.model.Approvals) > 0 || len(a.model.Questions) > 0 {
		return
	}
	if err := a.model.Prefs.Draft.Validate(); err != nil {
		a.model.Prefs.Error = err.Error()
		return
	}
	a.model.Prefs.Dirty = true
	a.acceptUIPreferences(a.model.Prefs.Draft)
	a.model.SettingsOpen = false
	a.model.SettingsConnection = false
	a.SaveLocalState()
}
