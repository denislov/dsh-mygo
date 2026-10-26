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
	// SettingsSectionRuntime is appended to keep existing sections stable. It
	// is always available and never asks the Host for state.
	SettingsSectionRuntime
)

func (model *Model) settingsSections() []SettingsSection {
	sections := []SettingsSection{SettingsSectionGeneral, SettingsSectionRuntime}
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
	case SettingsSectionGeneral, SettingsSectionShortcuts, SettingsSectionAbout, SettingsSectionRuntime:
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
	model.QuickSelector = ""
	model.SettingsSearch = ""
	model.SettingsProviderEditing = false
	model.SettingsOpen = true
}

func (model *Model) selectSettingsSection(section SettingsSection) {
	if section == model.activeSettingsSection() || !model.settingsSectionAvailable(section) {
		return
	}
	if model.SettingsApplying || model.SettingsWritingConfiguration || model.Permission.Writing ||
		model.Runtime.Busy || model.Runtime.Switching {
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
	if model.SettingsApplying || model.SettingsWritingConfiguration || model.Permission.Writing ||
		model.Runtime.Busy || model.Runtime.Switching {
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
	model.QuickSelector = ""
	model.SettingsProviderEditing = false
}

func settingsSectionLabel(model *Model, section SettingsSection) string {
	switch section {
	case SettingsSectionConversation:
		return model.T("Models")
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
	case SettingsSectionRuntime:
		return model.T("Runtime")
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
	case SettingsSectionRuntime:
		return "runtime"
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
		return model.T("Model providers")
	}
	if section == SettingsSectionPermissions {
		return model.T("Permissions")
	}
	if section == SettingsSectionRuntime {
		return model.T("Runtime")
	}
	return model.T("About dsh-mygo")
}

func settingsCenterModal(c *mgo.Context, model *Model) {
	if !model.SettingsOpen || model.QuickSelector != "" || model.RecoveryOpen || len(model.Approvals) > 0 || len(model.Questions) > 0 {
		return
	}
	width, height := c.Size()
	tokens := tokensFor(c.Theme())
	section := model.activeSettingsSection()
	contentWidth := min(float32(1240), width-32)
	contentHeight := min(float32(840), height-titleBarHeight(c)-32)
	writing := settingsWriting(model)
	modal := mgo.Modal(c.Key("settings-center"), &model.SettingsOpen, func() {
		mgo.Row(c.Key("settings-layout")).Width(contentWidth).Height(contentHeight).MinHeight(0).Children(func() {
			settingsNav(c, model, section)
			mgo.Column(c.Key("settings-content")).Grow(1).MinWidth(0).FillHeight().Children(func() {
				mgo.Row(c.Key("settings-header")).Height(settingsHeaderHeight).FillWidth().Padding(space6, space12).AlignItems(mgo.Center).Children(func() {
					mgo.Spacer(c)
					iconButton(c.Key("settings-close"), model.T("Close settings"), iconClose).Disabled(writing).OnClick(model.closeSettingsCenter)
				})
				padding := space28
				if width < 760 {
					padding = space16
				}
				mgo.Scroll(c.Key("settings-body-scroll-"+settingsSectionKey(section))).Grow(1).MinHeight(0).FillWidth().Padding(space8, padding, space28, padding).Children(func() {
					mgo.Column(c.Key("settings-page")).FillWidth().Gap(space24).Children(func() {
						mgo.Row(c).FillWidth().AlignItems(mgo.Center).Gap(space8).Children(func() {
							mgo.Text(c.Key("settings-title"), settingsSectionTitle(model, section)).FontSize(textTitle).Bold().Grow(1)
							if (section == SettingsSectionConversation || section == SettingsSectionPermissions || section == SettingsSectionConnection) && model.RefreshSettings != nil {
								iconButton(c.Key("settings-refresh"), model.T("Refresh"), iconRefresh).Disabled(!model.CanEditSettings() || writing).OnClick(model.RefreshSettings)
							}
						})
						settingsStatus(c, model)
						settingsMessages(c, model, section)
						switch section {
						case SettingsSectionGeneral:
							generalSettingsPage(c, model)
						case SettingsSectionShortcuts:
							shortcutSettingsPage(c, model)
						case SettingsSectionPermissions:
							permissionSettingsPage(c, model, !model.CanEditSettings())
						case SettingsSectionConnection:
							connectionSettingsPage(c, model, !model.CanEditSettings())
						case SettingsSectionRuntime:
							if model.Runtime.Busy && !model.Runtime.Switching && model.Runtime.Cancel != nil {
								mgo.Button(c.Key("runtime-operation-cancel"), model.T("Cancel")).OnClick(model.Runtime.Cancel)
							}
							runtimeSettingsPage(c, model)
						case SettingsSectionAbout:
							aboutSettingsPage(c, model)
						default:
							conversationSettingsPage(c, model, !model.CanEditSettings())
						}
					})
				})
			})
		})
	}).Padding(0).Gap(0).Margin(titleBarHeight(c), 0, 0, 0).MaxWidth(width-24).MaxHeight(height-titleBarHeight(c)-24).Radius(radiusLg).Border(1, tokens.Border)
	if modal.Dismissed() {
		if writing {
			model.SettingsOpen = true
		} else {
			model.closeSettingsCenter()
		}
	}
}

func settingsWriting(model *Model) bool {
	return model.SettingsApplying || model.SettingsWritingConfiguration || model.Permission.Writing || model.Runtime.Busy || model.Runtime.Switching
}

func settingsNav(c *mgo.Context, model *Model, active SettingsSection) {
	tokens := tokensFor(c.Theme())
	width, _ := c.Size()
	navWidth := float32(220)
	if width < 760 {
		navWidth = 144
	}
	mgo.Column(c.Key("settings-nav")).Width(navWidth).FillHeight().Shrink(0).Background(tokens.Sidebar).BorderWidth(0, 1, 0, 0).BorderColor(tokens.Border).Padding(space12).Gap(space16).Children(func() {
		mgo.SearchField(c.Key("settings-search"), &model.SettingsSearch).Label(model.T("Search settings")).Placeholder(model.T("Search settings")).FillWidth()
		mgo.Scroll(c.Key("settings-nav-scroll")).Grow(1).MinHeight(0).FillWidth().Children(func() {
			mgo.Column(c).FillWidth().Gap(space4).Children(func() {
				group := ""
				for _, section := range []SettingsSection{SettingsSectionGeneral, SettingsSectionConversation, SettingsSectionPermissions, SettingsSectionConnection, SettingsSectionRuntime, SettingsSectionShortcuts, SettingsSectionAbout} {
					if !model.settingsSectionAvailable(section) {
						continue
					}
					label := settingsSectionLabel(model, section)
					if model.SettingsSearch != "" && !strings.Contains(strings.ToLower(label+" "+settingsSectionTitle(model, section)), strings.ToLower(model.SettingsSearch)) {
						continue
					}
					next := "Settings"
					if section == SettingsSectionRuntime || section == SettingsSectionShortcuts || section == SettingsSectionAbout {
						next = "This computer"
					}
					if next != group {
						group = next
						mgo.Text(c, model.T(group)).FontSize(textMicro).TextColor(tokens.TextFaint).Margin(space12, space4, space6, space4)
					}
					background := mgo.Transparent
					foreground := tokens.TextMuted
					if section == active {
						background = tokens.SurfaceHover
						foreground = tokens.Text
					}
					button := mgo.Button(c.Key("settings-nav-"+settingsSectionKey(section)), "").Label(label).FillWidth().Padding(space8).Radius(radiusSm).Background(background).TextColor(foreground).Border(0, mgo.Transparent).Disabled(settingsWriting(model)).OnClick(func() { model.selectSettingsSection(section) })
					if section != active && button.Hovered() {
						button.Background(tokens.SurfaceHover)
					}
					button.Children(func() {
						mgo.Row(c).FillWidth().Gap(space8).AlignItems(mgo.Center).Children(func() {
							if width >= 760 {
								mgo.Icon(c, settingsSectionIcon(section)).Size(iconControl, iconControl)
							}
							mgo.Text(c, label).FontSize(textLabel).SingleLine().Grow(1).MinWidth(0)
						})
					})
				}
			})
		})
	})
}

func settingsSectionIcon(section SettingsSection) *mgo.SVG {
	switch section {
	case SettingsSectionGeneral:
		return iconSettings
	case SettingsSectionConversation:
		return iconSpark
	case SettingsSectionPermissions:
		return iconKey
	case SettingsSectionConnection:
		return iconExternal
	case SettingsSectionRuntime:
		return iconTerminal
	case SettingsSectionShortcuts:
		return iconTerminal
	default:
		return iconInfo
	}
}

func settingsStatus(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	if model.Runtime.Switching {
		mgo.Text(c.Key("settings-status"), model.T("Switching runtime…")).FontSize(textMicro).TextColor(tokens.TextMuted)
		return
	}
	if model.Runtime.Busy && model.Runtime.Status != "" {
		mgo.Text(c.Key("settings-status"), model.T(model.Runtime.Status)).FontSize(textMicro).TextColor(tokens.TextMuted)
		return
	}
	if !model.SettingsBusy {
		return
	}
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
	if model.Prefs.Dirty && model.LocalState.Error != "" {
		mgo.Text(c.Key("preferences-save-error"), model.T(model.LocalState.Error)).FontSize(textCaption).TextColor(tokens.Danger)
	}
}

func settingsRow(c *mgo.Context, title, description string, body func()) {
	tokens := tokensFor(c.Theme())
	mgo.Column(c).FillWidth().Padding(space12, 0).Gap(space8).BorderWidth(0, 0, 1, 0).BorderColor(tokens.Border).Children(func() {
		label := func() {
			mgo.Text(c, title).FontSize(textBody)
			if description != "" {
				mgo.Text(c, description).FontSize(textCaption).TextColor(tokens.TextMuted)
			}
		}
		width, _ := c.Size()
		if width >= 760 {
			mgo.Row(c).FillWidth().Gap(space24).AlignItems(mgo.Center).Children(func() {
				mgo.Column(c).Grow(1).MinWidth(0).Gap(space6).Children(label)
				mgo.Column(c).MaxWidth(280).Shrink(0).Children(body)
			})
		} else {
			label()
			body()
		}
	})
}

func generalSettingsPage(c *mgo.Context, model *Model) {
	p := &model.Prefs
	tokens := tokensFor(c.Theme())
	mgo.Text(c, model.T("Appearance")).FontSize(textBody).Bold()
	settingsRow(c, model.T("Theme"), "", func() {
		mgo.Row(c.Key("theme-control")).Gap(space4).Children(func() {
			for _, theme := range []struct {
				value, label string
				icon         *mgo.SVG
			}{{"system", "System theme", iconMonitor}, {"light", "Light theme", iconSun}, {"dark", "Dark theme", iconMoon}} {
				button := iconButton(c.Key(theme.value), model.T(theme.label), theme.icon).Border(1, tokens.Border).OnClick(func() { p.Draft.Theme = theme.value; model.ApplyPreferences() })
				if p.Value.Theme == theme.value {
					button.Background(tokens.SurfaceHover)
				}
			}
		})
	})
	settingsRow(c, model.T("Language"), model.T("Host language changes on its next start."), func() {
		preferenceChoice(c.Key("locale"), model, &p.Draft.Locale, []string{"en-US", "zh-CN"}, "Language").OnChange(model.ApplyPreferences)
	})
	if !p.Persisted {
		mgo.Text(c, model.T("Preferences cannot be saved in this mode.")).FontSize(textMicro).TextColor(tokens.Warning)
	}
}

func shortcutSettingsPage(c *mgo.Context, model *Model) {
	p := &model.Prefs
	settingsRow(c, model.T("Send shortcut"), model.T("Shift+Enter always adds a line. Commands pause while a dialog is open."), func() {
		mgo.Select(c.Key("send-key"), &p.Draft.Send, []string{"Enter", "Ctrl+Enter"}).Width(220).Label(model.T("Send shortcut")).OnChange(model.ApplyPreferences)
	})
	for _, row := range []struct {
		key, label string
		value      *string
	}{{"new", "New conversation shortcut", &p.Draft.NewSession}, {"prefs", "Preferences shortcut", &p.Draft.Preferences}, {"stop", "Stop shortcut", &p.Draft.Stop}} {
		settingsRow(c, model.T(row.label), "", func() {
			preferenceChoice(c.Key(row.key), model, row.value, preferences.CommandChoices(), row.label).OnChange(model.ApplyPreferences)
		})
	}
}

func modelRouteEditor(c *mgo.Context, model *Model, disabled bool) {
	tokens := tokensFor(c.Theme())
	if model.Catalog.Reason != "" {
		mgo.Text(c, model.Catalog.Reason).FontSize(textMicro).TextColor(tokens.Warning)
	}
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
	preferenceChoice(c.Key("settings-effort"), model, &model.SettingsEffort, efforts, "Reasoning effort").Disabled(disabled)
	mgo.Text(c, model.T("Manual model IDs are validated by the Host. Apply model affects only this session.")).
		FontSize(textMicro).TextColor(tokens.TextMuted)
	mgo.Row(c).FillWidth().Justify(mgo.End).Children(func() {
		mgo.PrimaryButton(c.Key("settings-apply"), model.T("Apply model")).Disabled(disabled || model.ApplySettings == nil || model.ModelUncertain || model.ConfigurationUncertain || model.Permission.Uncertain || strings.TrimSpace(model.SettingsProvider) == "" || strings.TrimSpace(model.SettingsModel) == "").OnClick(func() {
			if model.ApplySettings != nil {
				model.ApplySettings()
			}
		})
	})
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
		detailField(c, model.T("Version"), model.AppVersion)
		detailField(c, model.T("Host"), model.Detail)
		detailField(c, model.T("Session ID"), model.SessionID)
		mgo.Text(c, model.T("Native UI · no webview")).FontSize(textMicro).TextColor(tokens.TextMuted)
	})
}
