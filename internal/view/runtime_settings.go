package view

import (
	mgo "github.com/egoist/mygo/ui"
)

// CanActivateRuntime reports whether switching runtimes can stop and restart
// the host now. The app repeats the same checks before it commits a switch.
func (model *Model) CanActivateRuntime() bool {
	if !model.Runtime.CanManage || model.Runtime.SetupRequired || model.Runtime.Busy ||
		model.Runtime.Checking || model.Runtime.Switching {
		return false
	}
	if model.LocalState.Closing || model.SessionBusy || model.SessionSwitching ||
		model.SettingsApplying || model.SettingsWritingConfiguration || model.Permission.Writing ||
		model.RecoveryOpen || len(model.Approvals) > 0 || len(model.Questions) > 0 {
		return false
	}
	return model.Phase == PhaseIdle || model.Phase == PhaseFailed || model.Phase == PhaseResyncing
}

func runtimeSettingsPage(c *mgo.Context, model *Model) {
	tokens := tokensFor(c.Theme())
	current := model.Runtime.Current
	mgo.Column(c).Gap(space8).Children(func() {
		sectionLabel(c, model.T("Current runtime"))
		if current.ID == "" {
			mgo.Text(c, model.T("No runtime is selected.")).FontSize(textCaption).TextColor(tokens.Warning)
		} else {
			mgo.Text(c, model.T(current.Label)).FontSize(textBody).Bold()
			detail := model.T("Node") + " " + current.Node + " · DSH " + current.DSH
			if current.HostVersion != "" {
				detail += " · Host " + current.HostVersion
			}
			mgo.Text(c, detail).FontSize(textMicro).TextColor(tokens.TextMuted)
			if location := runtimeLocation(current); location != "" {
				detailValue(c, model, model.T("Location"), location, model.T("Copy runtime path"))
			}
		}
		if model.Runtime.Error != "" {
			mgo.Text(c, model.T(model.Runtime.Error)).FontSize(textMicro).TextColor(tokens.Danger)
		}
		if model.Runtime.Notice != "" {
			mgo.Text(c, model.T(model.Runtime.Notice)).FontSize(textMicro).TextColor(tokens.TextMuted)
		}

		mgo.Divider(c)
		sectionLabel(c, model.T("Installed runtimes"))
		if len(model.Runtime.Installed) == 0 {
			mgo.Text(c, model.T("No runtimes are installed.")).FontSize(textCaption).TextColor(tokens.TextMuted)
		}
		for _, choice := range model.Runtime.Installed {
			runtimeInstalledRow(c, model, choice, current)
		}

		mgo.Divider(c)
		sectionLabel(c, model.T("Add runtime"))
		mgo.Text(c, model.T("Installing a runtime adds it to the list. Use it when you are ready to switch.")).
			FontSize(textMicro).TextColor(tokens.TextMuted)
		mgo.Row(c.Key("runtime-add-actions")).Gap(space8).Wrap().Children(func() {
			if model.Runtime.Detect != nil {
				mgo.Button(c.Key("runtime-detect"), model.T("Check installed DSH")).
					Disabled(model.Runtime.Busy || model.Runtime.Checking || model.Runtime.Switching).
					OnClick(model.Runtime.Detect)
			}
			if model.Runtime.InstallFile != nil {
				mgo.Button(c.Key("runtime-install-file"), model.T("Install from file…")).
					Disabled(model.Runtime.Busy || model.Runtime.Checking || model.Runtime.Switching).
					OnClick(model.Runtime.InstallFile)
			}
			if model.Runtime.CheckUpdates != nil {
				mgo.Button(c.Key("runtime-check-updates"), model.T("Check for updates")).
					Disabled(model.Runtime.Busy || model.Runtime.Checking || model.Runtime.Switching).
					OnClick(model.Runtime.CheckUpdates)
			}
		})
		if model.Runtime.Checking {
			mgo.Row(c.Key("runtime-checking")).Gap(space6).AlignItems(mgo.Center).Children(func() {
				mgo.Spinner(c.Key("runtime-checking-spinner")).Size(12, 12)
				mgo.Text(c, model.T("Checking installed DSH…")).FontSize(textMicro).TextColor(tokens.TextMuted)
			})
		}
		for _, choice := range model.Runtime.Candidates {
			runtimeCandidateRow(c, model, choice)
		}
		for _, choice := range model.Runtime.Available {
			runtimeAvailableRow(c, model, choice)
		}
	})
}

func runtimeInstalledRow(c *mgo.Context, model *Model, choice RuntimeChoice, current RuntimeChoice) {
	tokens := tokensFor(c.Theme())
	isCurrent := choice.ID == current.ID
	row := mgo.Row(c.Key("runtime-installed-" + choice.ID)).FillWidth().Gap(space8).AlignItems(mgo.Center).
		Padding(space8).Radius(radiusSm).Background(tokens.Surface)
	row.Children(func() {
		mgo.Column(c).Grow(1).MinWidth(0).Gap(space2).Children(func() {
			mgo.Text(c, model.T(choice.Label)).FontSize(textCaption).Bold().SingleLine()
			detail := "DSH " + choice.DSH + " · Node " + choice.Node
			if choice.HostVersion != "" {
				detail += " · Host " + choice.HostVersion
			}
			mgo.Text(c, detail).FontSize(textMicro).TextColor(tokens.TextMuted).SingleLine()
		})
		if model.Runtime.RemoveConfirm == choice.ID {
			mgo.Text(c, model.T("Remove this runtime?")).FontSize(textMicro).TextColor(tokens.Warning)
			mgo.Button(c.Key("runtime-remove-confirm-"+choice.ID), model.T("Remove")).
				Disabled(model.Runtime.Busy || model.Runtime.Switching).
				OnClick(func() {
					model.Runtime.RemoveConfirm = ""
					if model.Runtime.Remove != nil {
						model.Runtime.Remove(choice.ID)
					}
				})
			mgo.Button(c.Key("runtime-remove-cancel-"+choice.ID), model.T("Cancel")).
				OnClick(func() { model.Runtime.RemoveConfirm = "" })
			return
		}
		if isCurrent {
			statusPill(c, "runtime-current-"+choice.ID, model.T("Current"), tokens.Success, tokens.SuccessSoft)
			return
		}
		activate := mgo.Button(c.Key("runtime-use-"+choice.ID), model.T("Use")).
			Disabled(!model.CanActivateRuntime() || current.Kind == "direct" || model.Runtime.Activate == nil)
		if activate.Clicked() && model.CanActivateRuntime() && current.Kind != "direct" && model.Runtime.Activate != nil {
			model.Runtime.Activate(choice.ID)
		}
		if choice.Kind != "direct" && model.Runtime.Remove != nil {
			remove := mgo.Button(c.Key("runtime-remove-"+choice.ID), model.T("Remove")).
				Disabled(model.Runtime.Busy || model.Runtime.Switching)
			if remove.Clicked() {
				model.Runtime.RemoveConfirm = choice.ID
			}
		}
	})
}

func runtimeLocation(choice RuntimeChoice) string {
	if choice.Path != "" {
		return choice.Path
	}
	return choice.Detail
}
