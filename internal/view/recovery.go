package view

import mgo "github.com/egoist/mygo/ui"

// recoveryModal is the required action surface after a host failure or an
// unrecoverable event-sequence gap. Escape keeps it open on purpose.
func recoveryModal(c *mgo.Context, model *Model) {
	if !model.RecoveryOpen || (model.Phase != PhaseFailed && model.Phase != PhaseResyncing) {
		return
	}
	disabled := model.Restarting
	modal := mgo.Modal(c.Key("host-recovery"), &model.RecoveryOpen, func() {
		title := "Host stopped"
		if model.Phase == PhaseResyncing {
			title = "Session needs a restart"
		}
		mgo.Text(c, title).FontSize(18).Bold()
		mgo.Text(c, model.Error).FontSize(13)
		if model.ErrorDetail != "" {
			mgo.Scroll(c.Key("recovery-detail")).MaxHeight(260).FillWidth().Children(func() {
				mgo.Text(c, model.ErrorDetail).FontSize(11).TextColor(c.Theme().TextMuted).Selectable()
			})
		}
		if model.RecoveryError != "" {
			mgo.Text(c, model.RecoveryError).FontSize(12).TextColor(c.Theme().Danger)
		}
		if model.Restarting {
			mgo.Text(c, "Restarting the host…").FontSize(11).TextColor(c.Theme().TextMuted)
		}
		mgo.Row(c).Gap(8).Children(func() {
			if model.CanOpenLogs && model.OpenLogs != nil {
				if mgo.Button(c.Key("recovery-logs"), "View logs").Disabled(disabled).Clicked() && !disabled {
					model.OpenLogs()
				}
			}
			mgo.Spacer(c)
			if model.Quit != nil {
				if mgo.Button(c.Key("recovery-quit"), "Quit").Disabled(disabled).Clicked() && !disabled {
					model.Quit()
				}
			}
			if model.Recoverable && model.Restart != nil {
				if mgo.PrimaryButton(c.Key("recovery-restart"), "Restart host").Disabled(disabled).Clicked() && !disabled {
					model.Restart()
				}
			}
		})
	})
	if modal.Dismissed() {
		model.RecoveryOpen = true
	}
}
