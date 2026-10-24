package view

import (
	"strings"

	"dsh-mygo/internal/preferences"
	mgo "github.com/egoist/mygo/ui"
)

type SettingsSection int

const (
	// SettingsSectionConversation keeps the zero value that existing callers
	// of OpenSettings land on.
	SettingsSectionConversation SettingsSection = iota
	SettingsSectionPermissions
	SettingsSectionConnection
	SettingsSectionGeneral
	SettingsSectionShortcuts
	SettingsSectionAbout
)

func (model *Model) settingsSections() []SettingsSection {
	sections := []SettingsSection{SettingsSectionGeneral}
	if model.CanModels {
		sections = append(sections, SettingsSectionConversation)
	}
	if model.Permission.Available {
		sections = append(sections, SettingsSectionPermissions)
	}
	if model.CanConfigure {
		sections = append(sections, SettingsSectionConnection)
	}
	sections = append(sections, SettingsSectionShortcuts, SettingsSectionAbout)
	return sections
}

func (model *Model) settingsSectionAvailable(section SettingsSection) bool {
	switch section {
	case SettingsSectionGeneral, SettingsSectionShortcuts, SettingsSectionAbout:
		return true
	case SettingsSectionConversation:
		return model.CanModels
	case SettingsSectionPermissions:
		return model.Permission.Available
	case SettingsSectionConnection:
		return model.CanConfigure
	default:
		return false
	}
}

func (model *Model) activeSettingsSection() SettingsSection {
	if model.SettingsConnection {
		return SettingsSectionConnection
	}
	if model.settingsSectionAvailable(model.SettingsSection) {
		return model.SettingsSection
	}
	if model.CanModels {
		return SettingsSectionConversation
	}
	if model.Permission.Available {
		return SettingsSectionPermissions
	}
	if model.CanConfigure {
		return SettingsSectionConnection
	}
	return SettingsSectionGeneral
}

// OpenSettingsAt opens the center directly on a section that does not need a
// Host round trip (General, Shortcuts, About).
func (model *Model) OpenSettingsAt(section SettingsSection) {
	if model.SettingsOpen || model.RecoveryOpen || model.SessionsOpen || model.Inspection.Open ||
		model.LocalState.Closing || len(model.Approvals) > 0 || len(model.Questions) > 0 ||
		!model.settingsSectionAvailable(section) {
		return
	}
	if model.Prefs.Ready {
		model.Prefs.Draft = model.Prefs.Value
		model.Prefs.Error = ""
	}
	model.SettingsSection = section
	model.SettingsConnection = false
	model.SettingsOpen = true
}

func (model *Model) selectSettingsSection(section SettingsSection) {
	if section == model.activeSettingsSection() || !model.settingsSectionAvailable(section) {
		return
	}
	if model.SettingsApplying || model.SettingsWritingConfiguration || model.Permission.Writing {
		return
	}
	model.SettingsSection = section
	model.SettingsConnection = false
	if section != SettingsSectionConnection {
		model.ClearCredentialDraft()
		model.ConfirmCredentialClear = false
	}
	if section == SettingsSectionConversation || section == SettingsSectionPermissions || section == SettingsSectionConnection {
		if model.RefreshSettings != nil {
			model.RefreshSettings()
		}
	}
}

func (model *Model) closeSettingsCenter() {
	if model.SettingsApplying || model.SettingsWritingConfiguration || model.Permission.Writing {
		return
	}
	model.Prefs.Draft = model.Prefs.Value
	model.Prefs.Error = ""
	model.ClearCredentialDraft()
	model.ConfirmCredentialClear = false
	model.Permission.Confirm = false
	if model.CloseSettings != nil {
		model.CloseSettings()
	} else {
		model.SettingsOpen = false
		model.SettingsConnection = false
	}
	model.SettingsSection = SettingsSectionConversation
}

func settingsSectionLabel(model *Model, section SettingsSection) string {
	switch section {
	case SettingsSectionConversation:
		return model.T("Conversation model")
	case SettingsSectionPermissions:
		return model.T("Permissions")
	case SettingsSectionConnection:
		return model.T("Connection")
	case SettingsSectionGeneral:
		return model.T("General")
	case SettingsSectionShortcuts:
		return model.T("Shortcuts")
	case SettingsSectionAbout:
		return model.T("About")
	default:
		return model.T("Settings")
	}
}

func settingsSectionKey(section SettingsSection) string {
	switch section {
	case SettingsSectionConversation:
		return "conversation"
	case SettingsSectionPermissions:
		return "permissions"
	case SettingsSectionConnection:
		return "connection"
	case SettingsSectionGeneral:
		return "general"
	case SettingsSectionShortcuts:
		return "shortcuts"
	case SettingsSectionAbout:
		return "about"
	default:
		return "settings"
	}
}

func settingsSectionTitle(model *Model, section SettingsSection) string {
	if section == SettingsSectionConnection {
		if model.SettingsConnection {
			return model.T("Connect DeepSeek")
		}
		return model.T("Connection")
	}
	if section == SettingsSectionGeneral {
		return model.T("Interface preferences")
	}
	if section == SettingsSectionShortcuts {
		return model.T("Keyboard shortcuts")
	}
	if section == SettingsSectionConversation {
		return model.T("Model settings")
	}
	if section == SettingsSectionPermissions {
		return model.T("Permissions")
	}
	return model.T("About dsh-mygo")
}

func settingsCenterModal(c *mgo.Context, model *Model) {
	if !model.SettingsOpen || model.RecoveryOpen || len(model.Approvals) > 0 || len(model.Questions) > 0 {
		return
	}
	width, height := c.Size()
	contentWidth := min(float32(760), width-80)
	contentHeight := min(float32(560), height-80)
	section := model.activeSettingsSection()
	disabled := !model.CanEditSettings()

	modal := mgo.Modal(c.Key("settings-center"), &model.SettingsOpen, func() {
		mgo.Column(c.Key("settings-panel")).Width(contentWidth).Height(contentHeight).Gap(space12).Children(func() {
			mgo.Row(c.Key("settings-header")).FillWidth().AlignItems(mgo.Center).Children(func() {
				mgo.Text(c.Key("settings-title"), settingsSectionTitle(model, section)).
					FontSize(textTitle).Bold().SingleLine()
			})
			mgo.Row(c.Key("settings-layout")).Grow(1).MinHeight(0).FillWidth().Gap(space16).
				AlignItems(mgo.Start).Children(func() {
				if !model.SettingsConnection {
					settingsNav(c, model, section)
				}
				mgo.Column(c.Key("settings-content")).Grow(1).MinWidth(0).FillHeight().Gap(space6).Children(func() {
					settingsStatus(c, model)
					mgo.Scroll(c.Key("settings-body-scroll")).Grow(1).MinHeight(0).FillWidth().Children(func() {
						switch section {
						case SettingsSectionGeneral:
							generalSettingsPage(c, model)
						case SettingsSectionShortcuts:
							shortcutSettingsPage(c, model)
						case SettingsSectionPermissions:
							permissionSettingsPage(c, model, disabled)
						case SettingsSectionConnection:
							connectionSettingsPage(c, model, disabled)
						case SettingsSectionAbout:
							aboutSettingsPage(c, model)
						default:
							conversationSettingsPage(c, model, disabled)
						}
					})
				})
			})
			settingsMessages(c, model, section)
			settingsFooter(c, model, section, disabled)
		})
	})
	if modal.Dismissed() {
		if model.SettingsApplying || model.SettingsWritingConfiguration || model.Permission.Writing {
			model.SettingsOpen = true
		} else {
			model.closeSettingsCenter()
		}
	}
}

func settingsNav(c *mgo.Context, model *Model, active SettingsSection) {
	tokens := tokensFor(c.Theme())
	mgo.Column(c.Key("settings-nav")).Width(150).Shrink(0).Gap(space2).AlignItems(mgo.Stretch).Children(func() {
		for _, section := range model.settingsSections() {
			background, foreground, border := mgo.Transparent, tokens.TextMuted, float32(0)
			if section == active {
				background, foreground, border = tokens.AccentSoft, tokens.Accent, 2
			}
			button := mgo.Button(c.Key("settings-nav-"+settingsSectionKey(section)), "").
				Label(settingsSectionLabel(model, section)).FillWidth().Padding(space6, space8).
				Radius(radiusSm).Background(background).TextColor(foreground).
				BorderWidth(0, 0, 0, border).BorderColor(tokens.Accent).
				Disabled(model.SettingsApplying || model.SettingsWritingConfiguration || model.Permission.Writing).
				OnClick(func() { model.selectSettingsSection(section) })
			button.Children(func() {
				mgo.Row(c).FillWidth().Justify(mgo.Start).Children(func() {
					mgo.Text(c, settingsSectionLabel(model, section)).FontSize(textCaption).SingleLine()
				})
			})
		}
	})
}

func settingsStatus(c *mgo.Context, model *Model) {
	if !model.SettingsBusy {
		return
	}
	tokens := tokensFor(c.Theme())
	label := "Loading model catalog…"
	if model.SettingsConnection {
		label = "Loading connection settings…"
	}
	if model.SettingsApplying {
		label = "Applying model…"
	}
	if model.SettingsWritingConfiguration {
		label = "Saving configuration…"
	}
	if model.Permission.Writing {
		label = "Applying permissions…"
	}
	mgo.Text(c.Key("settings-status"), model.T(label)).FontSize(textMicro).TextColor(tokens.TextMuted)
}

func settingsMessages(c *mgo.Context, model *Model, section SettingsSection) {
	tokens := tokensFor(c.Theme())
	if model.SettingsError != "" {
		mgo.Text(c.Key("settings-error"), model.T(model.SettingsError)).FontSize(textMicro).TextColor(tokens.Danger)
	}
	if model.SettingsNotice != "" {
		mgo.Text(c.Key("settings-notice"), model.T(model.SettingsNotice)).FontSize(textMicro).TextColor(tokens.TextMuted)
	}
	if (section == SettingsSectionGeneral || section == SettingsSectionShortcuts) && model.Prefs.Error != "" {
		mgo.Text(c.Key("preferences-error"), model.T(model.Prefs.Error)).FontSize(textMicro).TextColor(tokens.Danger)
	}
}

func settingsFooter(c *mgo.Context, model *Model, section SettingsSection, disabled bool) {
	writing := model.SettingsApplying || model.SettingsWritingConfiguration || model.Permission.Writing
	closeButton := func() {
		close := mgo.Button(c.Key("settings-close"), model.T("Close")).Disabled(writing)
		if close.Clicked() {
			model.closeSettingsCenter()
		}
	}
	refreshButton := func() {
		refresh := mgo.Button(c.Key("settings-refresh"), model.T("Refresh")).
			Disabled(disabled || writing || model.RefreshSettings == nil)
		if refresh.Clicked() && model.RefreshSettings != nil {
			model.RefreshSettings()
		}
	}
	mgo.Row(c.Key("settings-footer")).FillWidth().Gap(space8).AlignItems(mgo.Center).Children(func() {
		switch section {
		case SettingsSectionGeneral, SettingsSectionShortcuts:
			if mgo.Button(c.Key("reset"), model.T("Reset preferences")).Clicked() {
				model.Prefs.Draft = preferences.DefaultUI()
				model.Prefs.Error = ""
			}
			mgo.Spacer(c)
			if mgo.Button(c.Key("cancel"), model.T("Cancel")).Clicked() {
				model.closeSettingsCenter()
			}
			if mgo.PrimaryButton(c.Key("apply"), model.T("Apply preferences")).Clicked() {
				model.ApplyPreferences()
			}
		case SettingsSectionConversation:
			closeButton()
			mgo.Spacer(c)
			refreshButton()
			apply := mgo.PrimaryButton(c.Key("settings-apply"), model.T("Apply model")).
				Disabled(disabled || writing || model.ModelUncertain || model.ConfigurationUncertain ||
					model.Permission.Uncertain || model.ApplySettings == nil ||
					strings.TrimSpace(model.SettingsProvider) == "" || strings.TrimSpace(model.SettingsModel) == "")
			if apply.Clicked() && !writing && model.ApplySettings != nil {
				model.ApplySettings()
			}
		case SettingsSectionPermissions:
			refreshButton()
			mgo.Spacer(c)
			closeButton()
		case SettingsSectionConnection:
			refreshButton()
			mgo.Spacer(c)
			closeButton()
		default:
			mgo.Spacer(c)
			closeButton()
		}
	})
}

func generalSettingsPage(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	p := &model.Prefs
	mgo.Column(c).Gap(space6).Children(func() {
		mgo.Text(c, model.T("Appearance")).FontSize(textCaption).Bold()
		mgo.Text(c, model.T("Theme")).FontSize(textMicro).TextColor(tokens.TextMuted)
		preferenceChoice(c.Key("theme"), model, &p.Draft.Theme, []string{"system", "light", "dark"}, "Theme")
		mgo.Text(c, model.T("Language")).FontSize(textMicro).TextColor(tokens.TextMuted)
		preferenceChoice(c.Key("locale"), model, &p.Draft.Locale, []string{"en-US", "zh-CN"}, "Language")
		mgo.Text(c, model.T("Host language changes on its next start.")).FontSize(textMicro).TextColor(tokens.TextMuted)
		if !p.Persisted {
			mgo.Text(c, model.T("Preferences cannot be saved in this mode.")).FontSize(textMicro).TextColor(tokens.Warning)
		}
	})
}

func shortcutSettingsPage(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	p := &model.Prefs
	mgo.Column(c).Gap(space6).Children(func() {
		mgo.Text(c, model.T("Keyboard shortcuts")).FontSize(textCaption).Bold()
		mgo.Text(c, model.T("Send shortcut")).FontSize(textMicro).TextColor(tokens.TextMuted)
		mgo.Select(c.Key("send-key"), &p.Draft.Send, []string{"Enter", "Ctrl+Enter"}).Label(model.T("Send shortcut"))
		for _, row := range []struct {
			key, label string
			value      *string
		}{
			{"new", "New conversation shortcut", &p.Draft.NewSession},
			{"prefs", "Preferences shortcut", &p.Draft.Preferences},
			{"stop", "Stop shortcut", &p.Draft.Stop},
		} {
			mgo.Text(c, model.T(row.label)).FontSize(textMicro).TextColor(tokens.TextMuted)
			preferenceChoice(c.Key(row.key), model, row.value, preferences.CommandChoices(), row.label)
		}
		mgo.Text(c, model.T("Shift+Enter always adds a line. Commands pause while a dialog is open.")).
			FontSize(textMicro).TextColor(tokens.TextMuted)
	})
}

func conversationSettingsPage(c *mgo.Context, model *Model, disabled bool) {
	tokens := tokensFor(c.Theme())
	if model.Catalog.Reason != "" {
		mgo.Text(c, model.Catalog.Reason).FontSize(textMicro).TextColor(tokens.Warning)
	}
	providers := make([]string, 0, len(model.Catalog.Providers))
	for _, provider := range model.Catalog.Providers {
		providers = append(providers, provider.ID)
	}
	mgo.Text(c, model.T("Provider route")).FontSize(textMicro).TextColor(tokens.TextMuted)
	mgo.Select(c.Key("settings-provider"), &model.SettingsProvider, providers).Label(model.T("Provider")).Disabled(disabled).OnChange(func() {
		model.SettingsModel = ""
		model.SettingsCatalogModel = ""
		model.SettingsEffort = effortChoice("")
		if model.RefreshSettings != nil {
			model.RefreshSettings()
		}
	})
	models := []string{}
	efforts := []string{effortChoice("")}
	for _, entry := range model.Catalog.Models {
		if !entry.Available {
			mgo.Text(c, "Unavailable: "+entry.ID+" · "+entry.Reason).FontSize(textMicro).TextColor(tokens.Warning)
		}
		if entry.Available {
			models = append(models, entry.ID)
		}
		if entry.ID == model.SettingsModel && entry.Available {
			for _, effort := range entry.Efforts {
				efforts = append(efforts, effortChoice(effort.ID))
			}
		}
	}
	if len(models) > 0 {
		mgo.Text(c, model.T("Catalog suggestion (optional)")).FontSize(textMicro).TextColor(tokens.TextMuted)
		mgo.Select(c.Key("settings-model-catalog"), &model.SettingsCatalogModel, models).Label(model.T("Catalog model")).Disabled(disabled).OnChange(func() {
			model.SettingsModel = model.SettingsCatalogModel
			model.SettingsEffort = effortChoice("")
		})
	}
	mgo.Text(c, model.T("Model ID (applied)")).FontSize(textMicro).TextColor(tokens.TextMuted)
	mgo.TextInput(c.Key("settings-model-id"), &model.SettingsModel).Label(model.T("Model ID")).Disabled(disabled).OnChange(func() { model.SettingsEffort = effortChoice("") })
	mgo.Text(c, model.T("Effort")).FontSize(textMicro).TextColor(tokens.TextMuted)
	mgo.Select(c.Key("settings-effort"), &model.SettingsEffort, efforts).Label(model.T("Reasoning effort")).Disabled(disabled)
	mgo.Text(c, model.T("Manual model IDs are validated by the Host. Apply model affects only this session.")).
		FontSize(textMicro).TextColor(tokens.TextMuted)
	defaultModelControls(c, model, disabled)
}

func permissionSettingsPage(c *mgo.Context, model *Model, disabled bool) {
	permissionControls(c, model, disabled)
}

func connectionSettingsPage(c *mgo.Context, model *Model, disabled bool) {
	credentialControls(c, model, disabled)
}

func aboutSettingsPage(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	mgo.Column(c).Gap(space8).Children(func() {
		mgo.Text(c, model.T("About dsh-mygo")).FontSize(textCaption).Bold()
		detailField(c, model.T("Version"), model.AppVersion)
		detailField(c, model.T("Host"), model.Detail)
		detailField(c, model.T("Session ID"), model.SessionID)
		mgo.Text(c, model.T("Native UI · no webview")).FontSize(textMicro).TextColor(tokens.TextMuted)
	})
}
