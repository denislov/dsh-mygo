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

func configurationControls(c *mgo.Context, model *Model, disabled bool) {
	if !model.CanConfigure {
		return
	}
	disabled = disabled || !model.ConfigurationReady || model.ConfigurationUncertain
	mgo.Text(c, "Persistent profile default").Bold().FontSize(12)
	if model.ConfigurationReady {
		selection := model.Configuration.Defaults
		mgo.Text(c, selection.Provider+" / "+selection.Model+" / "+effortChoice(selection.Effort)).FontSize(11)
	}
	mgo.Text(c, "New conversations and cold restores use this default unless route flags override it. Current sessions stay unchanged.").FontSize(11).TextColor(c.Theme().TextMuted)
	mgo.Button(c.Key("settings-save-default"), "Save selection as default").Disabled(disabled || model.SaveDefaultModel == nil || model.ModelUncertain).OnClick(func() { model.SaveDefaultModel() })
	mgo.Text(c, "DeepSeek API credential").Bold().FontSize(12)
	credential := model.Configuration.Credential
	if !model.ConfigurationReady {
		mgo.Text(c, "Refresh to load credential status.").FontSize(11)
		return
	}
	if !credential.Available {
		mgo.Text(c, "This profile does not expose a supported DeepSeek credential binding.").FontSize(11)
		return
	}
	status := "Not configured"
	if credential.Configured {
		status = "Configured (not validated)"
	}
	mgo.Text(c, credential.Reference+" · "+status+" · "+credential.Source).FontSize(11)
	mgo.Text(c, "Saved locally by DSH, not an encrypted OS vault. Clear removes only the managed value; environment/.env fallback may remain.").FontSize(11).TextColor(c.Theme().Warning)
	mgo.TextInput(c.Key(fmt.Sprintf("credential-input-%d", model.CredentialVersion)), &model.CredentialDraft.value).Password().Label("New API key").Placeholder("New API key (write only)").Disabled(disabled || !credential.Writable)
	mgo.Row(c).Gap(8).Children(func() {
		mgo.Button(c.Key("credential-save"), "Save key").Disabled(disabled || !credential.Writable || model.SaveCredential == nil || model.CredentialDraft.value == "").OnClick(func() { model.SaveCredential() })
		mgo.Button(c.Key("credential-clear"), "Clear managed key").Disabled(disabled || !credential.Writable || model.ClearCredential == nil || !model.ConfirmCredentialClear).OnClick(func() { model.ClearCredential() })
	})
	mgo.Checkbox(c.Key("credential-confirm-clear"), &model.ConfirmCredentialClear, "Confirm removing the managed key").Disabled(disabled || !credential.Writable)
}
