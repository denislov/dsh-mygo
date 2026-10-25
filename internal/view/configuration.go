package view

import (
	"fmt"

	mgo "github.com/egoist/mygo/ui"
)

type CredentialStatus struct {
	Available  bool   `json:"available"`
	Reference  string `json:"reference"`
	Configured bool   `json:"configured"`
	Writable   bool   `json:"writable"`
	Source     string `json:"source"`
}

type Configuration struct {
	Revision   uint64           `json:"revision"`
	Defaults   ModelRoute       `json:"defaults"`
	Credential CredentialStatus `json:"credential"`
}

type CredentialDraft struct{ value string }

func (CredentialDraft) String() string   { return "[redacted]" }
func (CredentialDraft) GoString() string { return "[redacted]" }

func (model *Model) TakeCredential() string {
	value := model.CredentialDraft.value
	model.ClearCredentialDraft()
	return value
}

func (model *Model) ClearCredentialDraft() {
	model.CredentialDraft.value = ""
	model.CredentialVersion++
}

func defaultModelControls(c *mgo.Context, model *Model, disabled bool) {
	if !model.CanConfigure {
		return
	}
	disabled = disabled || !model.ConfigurationReady || model.ConfigurationUncertain || model.Permission.Uncertain
	mgo.Text(c, model.T("Persistent profile default")).Bold().FontSize(12)
	if model.ConfigurationReady {
		selection := model.Configuration.Defaults
		mgo.Text(c, selection.Provider+" / "+selection.Model+" / "+model.preferenceChoiceLabel(effortChoice(selection.Effort))).FontSize(11)
	}
	mgo.Text(c, model.T("New conversations and cold restores use this default unless route flags override it. Current sessions stay unchanged.")).FontSize(11).TextColor(c.Theme().TextMuted)
	mgo.Row(c).FillWidth().Justify(mgo.End).Children(func() {
		mgo.Button(c.Key("settings-save-default"), model.T("Save selection as default")).Disabled(disabled || model.SaveDefaultModel == nil || model.ModelUncertain).OnClick(func() { model.SaveDefaultModel() })
	})
}

func credentialControls(c *mgo.Context, model *Model, disabled bool) {
	if !model.CanConfigure {
		return
	}
	disabled = disabled || !model.ConfigurationReady || model.ConfigurationUncertain || model.Permission.Uncertain
	if !model.SettingsConnection {
		mgo.Text(c, model.T("DeepSeek API credential")).Bold().FontSize(12)
	}
	credential := model.Configuration.Credential
	if !model.ConfigurationReady {
		mgo.Text(c, model.T("Refresh to load credential status.")).FontSize(11)
		return
	}
	if !credential.Available {
		mgo.Text(c, model.T("This profile does not expose a supported DeepSeek credential binding.")).FontSize(11)
		return
	}
	status := "Not configured"
	if credential.Configured {
		status = "Configured (not validated)"
	}
	statusText := model.T(status)
	if !model.SettingsConnection {
		statusText = credential.Reference + " · " + statusText + " · " + credential.Source
	}
	mgo.Text(c, statusText).FontSize(11)
	if !model.SettingsConnection {
		mgo.Text(c, model.T("Saved locally by DSH, not an encrypted OS vault. Clear removes only the managed value; environment/.env fallback may remain.")).FontSize(11).TextColor(c.Theme().Warning)
	}
	mgo.TextInput(c.Key(fmt.Sprintf("credential-input-%d", model.CredentialVersion)), &model.CredentialDraft.value).Password().Label(model.T("New API key")).Placeholder(model.T("New API key (write only)")).Disabled(disabled || !credential.Writable)
	mgo.Row(c).Gap(8).Children(func() {
		mgo.Button(c.Key("credential-save"), model.T("Save key")).Disabled(disabled || !credential.Writable || model.SaveCredential == nil || model.CredentialDraft.value == "").OnClick(func() { model.SaveCredential() })
		if model.SettingsConnection {
			services := c.Services()
			mgo.Button(c.Key("credential-get-key"), model.T("Get API key")).OnClick(func() { services.OpenURL("https://platform.deepseek.com/") })
		}
		if !model.SettingsConnection {
			mgo.Button(c.Key("credential-clear"), model.T("Clear managed key")).Disabled(disabled || !credential.Writable || model.ClearCredential == nil || !model.ConfirmCredentialClear).OnClick(func() { model.ClearCredential() })
		}
	})
	if model.SettingsConnection {
		mgo.Text(c, model.T("Stored on this device by DSH. This is not an encrypted key vault.")).FontSize(11).TextColor(c.Theme().TextMuted)
		return
	}
	mgo.Checkbox(c.Key("credential-confirm-clear"), &model.ConfirmCredentialClear, model.T("Confirm removing the managed key")).Disabled(disabled || !credential.Writable)
}
