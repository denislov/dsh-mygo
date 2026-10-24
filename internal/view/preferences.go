package view

import (
	"strings"

	"dsh-mygo/internal/i18n"
	"dsh-mygo/internal/preferences"
	mgo "github.com/egoist/mygo/ui"
)

type PreferencesState struct {
	Value, Draft                  preferences.UISettings
	Open, Ready, Persisted, Dirty bool
	Error                         string
	Apply                         func()
}

var lightTheme = applicationTheme(false)
var darkTheme = applicationTheme(true)
var lightChineseTheme = chineseTheme(lightTheme)
var darkChineseTheme = chineseTheme(darkTheme)

func (model *Model) CanOpenPreferences() bool {
	return model.Prefs.Ready && !model.Setup.Picking && !model.LocalState.Closing && !model.Inspection.Open && !model.Prefs.Open && !model.SettingsOpen && !model.SessionsOpen && !model.RecoveryOpen && !model.SessionBusy && !model.SettingsBusy && model.ToolInspector == nil && len(model.Approvals) == 0 && len(model.Questions) == 0
}
func (model *Model) OpenPreferences() {
	if !model.CanOpenPreferences() {
		return
	}
	model.Prefs.Draft = model.Prefs.Value
	model.Prefs.Error = ""
	model.Prefs.Open = true
}
func (model *Model) ApplyPreferences() {
	if err := model.Prefs.Draft.Validate(); err != nil {
		model.Prefs.Error = err.Error()
		return
	}
	if model.Prefs.Apply != nil {
		model.Prefs.Apply()
		return
	}
	model.Prefs.Value = model.Prefs.Draft
	model.Prefs.Open = false
}

func preferenceModal(c *mgo.Context, model *Model) {
	p := &model.Prefs
	if !p.Open || model.RecoveryOpen || len(model.Approvals) > 0 || len(model.Questions) > 0 {
		return
	}
	modal := mgo.Modal(c.Key("ui-preferences"), &p.Open, func() {
		mgo.Text(c, model.T("Interface preferences")).FontSize(18).Bold()
		mgo.Scroll(c.Key("preferences-body")).MaxHeight(220).FillWidth().Children(func() {
			mgo.Column(c).Gap(7).Children(func() {
				mgo.Text(c, model.T("Theme")).FontSize(12).Bold()
				preferenceChoice(c.Key("theme"), model, &p.Draft.Theme, []string{"system", "light", "dark"}, "Theme")
				mgo.Text(c, model.T("Language")).FontSize(12).Bold()
				preferenceChoice(c.Key("locale"), model, &p.Draft.Locale, []string{"en-US", "zh-CN"}, "Language")
				mgo.Text(c, model.T("Host language changes on its next start.")).FontSize(11)
				mgo.Text(c, model.T("Send shortcut")).FontSize(12).Bold()
				mgo.Select(c.Key("send-key"), &p.Draft.Send, []string{"Enter", "Ctrl+Enter"}).Label(model.T("Send shortcut"))
				for _, row := range []struct {
					key, label string
					value      *string
				}{
					{"new", "New conversation shortcut", &p.Draft.NewSession}, {"prefs", "Preferences shortcut", &p.Draft.Preferences}, {"stop", "Stop shortcut", &p.Draft.Stop},
				} {
					mgo.Text(c, model.T(row.label)).FontSize(12)
					preferenceChoice(c.Key(row.key), model, row.value, preferences.CommandChoices(), row.label)
				}
				mgo.Text(c, model.T("Shift+Enter always adds a line. Commands pause while a dialog is open.")).FontSize(11)
				if !p.Persisted {
					mgo.Text(c, model.T("Preferences cannot be saved in this mode.")).FontSize(11).TextColor(c.Theme().Warning)
				}
			})
		})
		if p.Error != "" {
			mgo.Text(c, model.T(p.Error)).FontSize(11).TextColor(c.Theme().Danger)
		}
		mgo.Row(c).Gap(8).Children(func() {
			mgo.Button(c.Key("reset"), model.T("Reset preferences")).OnClick(func() { p.Draft = preferences.DefaultUI(); p.Error = "" })
			mgo.Spacer(c)
			mgo.Button(c.Key("cancel"), model.T("Cancel")).OnClick(func() { p.Open = false })
			mgo.PrimaryButton(c.Key("apply"), model.T("Apply preferences")).OnClick(model.ApplyPreferences)
		})
	})
	if modal.Dismissed() {
		p.Open = false
	}
}

func commandKey(value string) (mgo.Modifiers, mgo.Key) {
	mods := mgo.Ctrl
	if strings.Contains(value, "Shift+") {
		mods |= mgo.Shift
	}
	switch value {
	case "Ctrl+N", "Ctrl+Shift+N":
		return mods, mgo.KeyN
	case "Ctrl+,":
		return mods, mgo.KeyComma
	case "Ctrl+Shift+P":
		return mods, mgo.KeyP
	case "Ctrl+.":
		return mods, mgo.KeyPeriod
	case "Ctrl+Shift+X":
		return mods, mgo.KeyX
	}
	return 0, mgo.KeyUnknown
}
func applicationShortcuts(c *mgo.Context, model *Model) {
	if model.Prefs.Open || model.SettingsOpen || model.SessionsOpen || model.Inspection.Open || model.RecoveryOpen || model.ToolInspector != nil || model.LocalState.Closing || len(model.Approvals) > 0 || len(model.Questions) > 0 {
		return
	}
	bind := func(value string, enabled bool, fn func()) {
		mods, key := commandKey(value)
		if enabled && fn != nil && key != mgo.KeyUnknown {
			c.OnShortcut(mods, key, fn)
		}
	}
	bind(model.Prefs.Value.NewSession, model.CanChooseSession(), model.NewSession)
	bind(model.Prefs.Value.Preferences, model.CanOpenPreferences(), model.OpenPreferences)
	bind(model.Prefs.Value.Stop, model.Phase == PhaseRunning && model.CanCancel && !model.CancelPending, model.Cancel)
}

func (model *Model) T(text string) string { return i18n.Text(model.Prefs.Value.Locale, text) }

// Bind stable values directly; only their labels change with the locale.
func preferenceChoice(c *mgo.Context, model *Model, value *string, options []string, label string) mgo.Element {
	theme := c.Theme()
	choice := mgo.SelectBase(c, value)
	choice.Trigger.Label(model.T(label)+model.T(" selection")).Padding(theme.Space(2), theme.Space(3)).MinWidth(140).Border(1, theme.Border).Radius(theme.Radius).Background(theme.Surface).Children(func() {
		mgo.Text(c, model.preferenceChoiceLabel(*value)).SingleLine()
		mgo.Text(c, "▾").TextColor(theme.TextMuted)
	})
	choice.Popup(func(panel mgo.Element) {
		panel.Padding(4).Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius)
		mgo.Scroll(c).MaxHeight(180).Children(func() {
			for _, option := range options {
				item := choice.Item(option).Label(model.T(option)).Padding(theme.Space(1.5), theme.Space(2.5)).Radius(theme.Radius)
				if item.Highlighted() {
					item.Background(theme.Accent).TextColor(theme.AccentText)
				}
				item.Children(func() { mgo.Text(c, model.preferenceChoiceLabel(option)).SingleLine() })
			}
		})
	})
	return choice.Trigger
}

func (model *Model) preferenceChoiceLabel(value string) string {
	switch value {
	case "system":
		return model.T("Follow system")
	case "light":
		return model.T("Light")
	case "dark":
		return model.T("Dark")
	case "en-US":
		return "English"
	case "zh-CN":
		return "简体中文"
	}
	return model.T(value)
}
