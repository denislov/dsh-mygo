package view

import mgo "github.com/egoist/mygo/ui"

type PermissionOption struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	Sandbox              string `json:"sandbox"`
	Approval             string `json:"approval"`
	RequiresConfirmation bool   `json:"requiresConfirmation"`
}
type PermissionSnapshot struct {
	SessionID string             `json:"sessionId"`
	Current   string             `json:"current"`
	Sandbox   string             `json:"sandbox"`
	Approval  string             `json:"approval"`
	NextSeq   uint64             `json:"nextSeq"`
	Options   []PermissionOption `json:"options"`
}
type PermissionState struct {
	Available bool
	Ready     bool
	Uncertain bool
	Snapshot  PermissionSnapshot
	Selected  string
	Confirm   bool
	Writing   bool
	Apply     func()
}

func (state *PermissionState) SelectedOption() *PermissionOption {
	for i := range state.Snapshot.Options {
		if state.Snapshot.Options[i].ID == state.Selected {
			return &state.Snapshot.Options[i]
		}
	}
	return nil
}
func (state *PermissionState) Accept(snapshot PermissionSnapshot) {
	state.Snapshot, state.Selected = snapshot, snapshot.Current
	state.Ready, state.Uncertain, state.Confirm = true, false, false
}

func permissionPresentation(model *Model, option PermissionOption) (string, string) {
	label, description := option.Name, ""
	// Only rename built-in technical labels; custom provider names remain data.
	if option.Name == option.ID {
		switch {
		case option.ID == "read-only" && option.Sandbox == "read-only" && option.Approval == "ask":
			label = model.T("Read only")
		case option.ID == "workspace-write" && option.Sandbox == "workspace-write" && option.Approval == "ask":
			label = model.T("Workspace access")
		case option.ID == "danger-full-access" && option.Sandbox == "danger-full-access" && option.Approval == "never":
			label = model.T("Full access")
		}
	}
	if option.Sandbox == "danger-full-access" && option.Approval == "never" {
		description = model.T("Full file access without approval prompts.")
	} else if option.Sandbox == "workspace-write" && option.Approval == "ask" {
		description = model.T("Allow project file changes; ask when required.")
	} else if option.Sandbox == "read-only" && option.Approval == "ask" {
		description = model.T("Read files without making changes.")
	} else {
		// An unusual/custom bundle must not inherit a misleading built-in description.
		description = option.Sandbox + " / " + option.Approval
	}
	return label, description
}
func permissionControls(c *mgo.Context, model *Model, disabled bool) {
	state := &model.Permission
	if !state.Available {
		return
	}
	mgo.Text(c, model.T("Session permissions")).Bold().FontSize(12)
	mgo.Text(c, model.T("Current conversation only · saved in session history")).FontSize(11).TextColor(c.Theme().TextMuted)
	if !state.Ready {
		mgo.Text(c, model.T("Refresh to load session permissions.")).FontSize(11)
		return
	}
	snapshot := state.Snapshot
	mgo.Text(c, model.T("Current: ")+snapshot.Current+" · "+snapshot.Sandbox+" / "+snapshot.Approval).FontSize(11)
	disabled = disabled || state.Uncertain || model.ModelUncertain || model.ConfigurationUncertain
	choices := make([]string, 0, len(snapshot.Options))
	for _, option := range snapshot.Options {
		choices = append(choices, option.ID)
	}
	mgo.Select(c.Key("permission-preset"), &state.Selected, choices).Label(model.T("Permission preset")).Disabled(disabled).OnChange(func() { state.Confirm = false })
	option := state.SelectedOption()
	if option != nil {
		mgo.Text(c, option.Sandbox+" / "+option.Approval).FontSize(11)
		if option.Description != "" {
			mgo.Text(c, option.Description).FontSize(11)
		}
		if option.RequiresConfirmation {
			mgo.Text(c, model.T("This preset permits unrestricted file access or disables approval prompts. It applies to future operations in this conversation.")).FontSize(11).TextColor(c.Theme().Warning)
			mgo.Checkbox(c.Key("permission-confirm-"+option.ID), &state.Confirm, model.T("I understand this permission change")).Disabled(disabled)
		}
	}
	mgo.Button(c.Key("permission-apply"), model.T("Apply permissions")).Disabled(disabled || option == nil || option.RequiresConfirmation && !state.Confirm || state.Apply == nil).OnClick(func() { state.Apply() })
}
