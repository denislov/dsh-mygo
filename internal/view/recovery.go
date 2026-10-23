package view

import mgo "github.com/egoist/mygo/ui"

// recoveryModal is the required action surface after a host failure or an
// unrecoverable event-sequence gap. Escape keeps it open on purpose.
func recoveryModal(c *mgo.Context, model *Model) {
	if !model.RecoveryOpen || (model.Phase != PhaseFailed && model.Phase != PhaseResyncing) {
		return
	}
	disabled := model.Restarting || model.LocalState.Closing
	modal := mgo.Modal(c.Key("host-recovery"), &model.RecoveryOpen, func() {
		title := "Host stopped"
		if model.Phase == PhaseResyncing {
			title = "Session needs a restart"
		}
		mgo.Text(c, model.T(title)).FontSize(18).Bold()
		mgo.Text(c, model.T(model.Error)).FontSize(13)
		if model.ErrorDetail != "" {
			detailHeight := float32(260)
			if model.LocalState.Error != "" {
				detailHeight = 160
			}
			mgo.Scroll(c.Key("recovery-detail")).MaxHeight(detailHeight).FillWidth().Children(func() {
				mgo.Text(c, model.ErrorDetail).FontSize(11).TextColor(c.Theme().TextMuted).Selectable()
			})
		}
		if model.RecoveryError != "" {
			mgo.Text(c, model.T(model.RecoveryError)).FontSize(12).TextColor(c.Theme().Danger)
		}
		if model.Restarting {
			mgo.Text(c, model.T("Restarting the host…")).FontSize(11).TextColor(c.Theme().TextMuted)
		}
		if model.LocalState.Error != "" {
			mgo.Text(c, model.T(model.LocalState.Error)).FontSize(11).TextColor(c.Theme().Warning)
		}
		mgo.Row(c).Gap(8).Children(func() {
			if model.CanOpenLogs && model.OpenLogs != nil {
				if mgo.Button(c.Key("recovery-logs"), model.T("View logs")).Disabled(disabled).Clicked() && !disabled {
					model.OpenLogs()
				}
			}
			mgo.Spacer(c)
			if model.Quit != nil {
				label, quit := "Quit", model.Quit
				if model.LocalState.Error != "" && model.LocalState.QuitWithoutSaving != nil {
					label, quit = "Quit without saving drafts", model.LocalState.QuitWithoutSaving
				}
				if mgo.Button(c.Key("recovery-quit"), model.T(label)).Disabled(disabled).Clicked() && !disabled {
					quit()
				}
			}
			if model.Recoverable && model.Restart != nil {
				if mgo.PrimaryButton(c.Key("recovery-restart"), model.T("Restart host")).Disabled(disabled).Clicked() && !disabled {
					model.Restart()
				}
			}
		})
	})
	if modal.Dismissed() {
		model.RecoveryOpen = true
	}
}
