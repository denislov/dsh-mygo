package view

import (
	"strings"

	mgo "github.com/egoist/mygo/ui"
)

// Quick selectors share the settings operation lifecycle and Host acknowledgments.
// Only their presentation differs from the full settings center.
func (m *Model) openQuickSelector(kind string) {
	if !m.CanEditSettings() || m.SettingsOpen || m.OpenSettings == nil {
		return
	}
	m.OpenSettings()
	if !m.SettingsOpen {
		return
	}
	m.QuickSelector, m.QuickSearch = kind, ""
	m.Permission.Confirm = false
	if kind == "permission" {
		m.SettingsSection = SettingsSectionPermissions
	}
}

func quickChip(c *mgo.Context, label, text string) mgo.Element {
	tokens := tokensFor(c.Theme())
	b := mgo.Button(c, "").Label(label).Padding(space4, space6).Gap(space6).Radius(radiusSm).
		Background(mgo.Transparent).Border(0, mgo.Transparent).Transition(quietTransition)
	if b.Hovered() {
		b.Background(tokens.SurfaceHover)
	}
	b.Children(func() {
		mgo.Text(c, text).FontSize(textCaption).TextColor(tokens.TextMuted).SingleLine().Shrink(1)
		mgo.Icon(c, iconChevron).Size(10, 10).TextColor(tokens.TextFaint)
	})
	return b
}

func quickSelector(c *mgo.Context, m *Model, anchor mgo.Element, kind string) {
	if !m.SettingsOpen || m.QuickSelector != kind || m.RecoveryOpen || len(m.Approvals) > 0 || len(m.Questions) > 0 {
		return
	}
	width, height := c.Size()
	tokens := tokensFor(c.Theme())
	panel := mgo.Popover(c.Key("quick-"+kind), anchor, &m.SettingsOpen, func() {
		mgo.Column(c).Width(min(420, width-64)).MaxHeight(min(540, height-90)).Gap(space8).Padding(space8).Children(func() {
			title := "Choose model"
			if kind == "permission" {
				title = "Choose permissions"
			}
			mgo.Row(c).FillWidth().AlignItems(mgo.Center).Children(func() {
				mgo.Text(c, m.T(title)).FontSize(textLabel).Bold().Grow(1)
				iconButton(c.Key("close-quick"), m.T("Close selector"), iconClose).Background(mgo.Transparent).
					Disabled(m.SettingsApplying).OnClick(m.closeSettingsCenter)
			})
			if m.SettingsBusy {
				mgo.Text(c, m.T("Loading settings…")).FontSize(textCaption).TextColor(tokens.TextMuted)
			}
			if m.SettingsError != "" {
				mgo.Text(c, m.T(m.SettingsError)).FontSize(textCaption).TextColor(tokens.Danger)
			}
			if kind == "model" {
				quickModels(c, m, height)
			} else {
				quickPermissions(c, m, height)
			}
			mgo.Row(c).FillWidth().Gap(space8).Children(func() {
				mgo.Button(c.Key("refresh-quick"), m.T("Refresh")).Disabled(m.SettingsBusy || m.RefreshSettings == nil).OnClick(m.RefreshSettings)
				mgo.Spacer(c)
				mgo.Button(c.Key("advanced-settings"), m.T("More settings…")).Disabled(m.SettingsBusy).OnClick(func() {
					m.QuickSelector = ""
					if kind == "model" {
						m.SettingsSection = SettingsSectionConversation
					} else {
						m.SettingsSection = SettingsSectionPermissions
					}
				})
			})
		})
	})
	if panel.Dismissed() {
		if m.SettingsApplying {
			m.SettingsOpen = true
		} else {
			m.closeSettingsCenter()
			m.QuickSelector = ""
		}
	}
}

func quickModels(c *mgo.Context, m *Model, height float32) {
	tokens := tokensFor(c.Theme())
	disabled := m.SettingsBusy || m.ModelUncertain || m.ConfigurationUncertain || m.Permission.Uncertain
	providers := make([]string, 0, len(m.Catalog.Providers))
	for _, p := range m.Catalog.Providers {
		providers = append(providers, p.ID)
	}
	if len(providers) > 1 {
		mgo.Select(c.Key("quick-provider"), &m.SettingsProvider, providers).Label(m.T("Provider route")).FillWidth().Disabled(m.SettingsBusy).OnChange(func() {
			m.SettingsModel, m.SettingsCatalogModel, m.SettingsEffort = "", "", effortChoice("")
			if m.RefreshSettings != nil {
				m.RefreshSettings()
			}
		})
	}
	mgo.SearchField(c.Key("quick-model-search"), &m.QuickSearch).Label(m.T("Search models")).Placeholder(m.T("Search models")).FillWidth()
	mgo.Scroll(c.Key("quick-model-list")).Height(max(70, min(320, height-290))).FillWidth().Children(func() {
		mgo.Column(c).FillWidth().Gap(space2).Children(func() {
			count := 0
			for _, entry := range m.Catalog.Models {
				if !strings.Contains(strings.ToLower(entry.Name+" "+entry.ID), strings.ToLower(m.QuickSearch)) {
					continue
				}
				count++
				selected := m.Catalog.Provider == m.ActiveRoute.Provider && entry.ID == m.ActiveRoute.Model
				b := mgo.Button(c.Key("quick-model-"+entry.ID), "").Label(m.T("Use model · ")+entry.Name).
					FillWidth().Padding(space8).Radius(radiusSm).Background(mgo.Transparent).Border(0, mgo.Transparent).
					Disabled(disabled || !entry.Available || m.ApplySettings == nil)
				if selected || b.Hovered() {
					b.Background(tokens.SurfaceHover)
				}
				b.Children(func() {
					mgo.Column(c).Grow(1).MinWidth(0).Gap(space2).Children(func() {
						mgo.Text(c, entry.Name).FontSize(textLabel).SingleLine()
						mgo.Text(c, entry.ID).FontSize(textMicro).TextColor(tokens.TextMuted).SingleLine()
						if !entry.Available && entry.Reason != "" {
							mgo.Text(c, m.T(entry.Reason)).FontSize(textMicro).TextColor(tokens.Warning)
						}
					})
					if selected {
						mgo.Icon(c, iconCheck).Size(14, 14).TextColor(tokens.Accent)
					}
				})
				b.OnClick(func() {
					effort := ""
					if m.Catalog.Provider == m.ActiveRoute.Provider {
						for _, e := range entry.Efforts {
							if e.ID == m.ActiveRoute.Effort {
								effort = e.ID
							}
						}
					}
					m.PrepareSettings(ModelRoute{Provider: m.Catalog.Provider, Model: entry.ID, Effort: effort})
					m.ApplySettings()
				})
			}
			if count == 0 && !m.SettingsBusy {
				mgo.Text(c, m.T("No matching models.")).FontSize(textCaption).TextColor(tokens.TextMuted)
			}
		})
	})
	for _, entry := range m.Catalog.Models {
		if m.Catalog.Provider != m.ActiveRoute.Provider || entry.ID != m.ActiveRoute.Model || len(entry.Efforts) == 0 {
			continue
		}
		mgo.Row(c).Gap(space8).FillWidth().AlignItems(mgo.Center).Children(func() {
			mgo.Text(c, m.T("Reasoning effort")).FontSize(textCaption).TextColor(tokens.TextMuted)
			choices := []string{effortChoice("")}
			for _, e := range entry.Efforts {
				choices = append(choices, effortChoice(e.ID))
			}
			mgo.Select(c.Key("quick-effort"), &m.SettingsEffort, choices).Label(m.T("Reasoning effort")).Disabled(disabled).OnChange(func() {
				m.SettingsProvider, m.SettingsModel = m.ActiveRoute.Provider, m.ActiveRoute.Model
				if m.ApplySettings != nil {
					m.ApplySettings()
				}
			})
		})
	}
}

func quickPermissions(c *mgo.Context, m *Model, height float32) {
	s := &m.Permission
	disabled := m.SettingsBusy || !s.Ready || s.Uncertain || m.ModelUncertain || m.ConfigurationUncertain
	mgo.Scroll(c.Key("quick-permission-list")).MaxHeight(max(70, min(250, height-330))).FillWidth().Children(func() {
		mgo.Column(c).FillWidth().Gap(space4).Children(func() {
			for _, option := range s.Snapshot.Options {
				b := quickChip(c.Key("permission-"+option.ID), m.T("Permission option · ")+option.Name, option.Name).FillWidth().Disabled(disabled)
				if s.Selected == option.ID {
					b.Background(tokensFor(c.Theme()).SurfaceHover)
				}
				b.Tooltip(option.Sandbox + " / " + option.Approval).OnClick(func() { s.Selected = option.ID; s.Confirm = false })
			}
		})
	})
	if option := s.SelectedOption(); option != nil {
		mgo.Text(c, option.Sandbox+" / "+option.Approval).FontSize(textMicro).TextColor(c.Theme().TextMuted)
		if option.RequiresConfirmation {
			mgo.Text(c, m.T("This preset permits unrestricted file access or disables approval prompts. It applies to future operations in this conversation.")).FontSize(textCaption).TextColor(c.Theme().Warning)
			mgo.Checkbox(c.Key("quick-permission-confirm-"+option.ID), &s.Confirm, m.T("I understand this permission change")).Disabled(disabled)
		}
		mgo.PrimaryButton(c.Key("quick-apply-permission"), m.T("Apply permissions")).Disabled(disabled || s.Apply == nil || option.RequiresConfirmation && !s.Confirm).OnClick(s.Apply)
	}
}
