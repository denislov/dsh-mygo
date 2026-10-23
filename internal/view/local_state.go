package view

import mgo "github.com/egoist/mygo/ui"

type LocalStateStatus struct {
	Error             string
	Notice            string
	Closing           bool
	Retry             func()
	QuitWithoutSaving func()
	RecoverDraft      func()
}

func localStateStatus(c *mgo.Context, model *Model) {
	state := &model.LocalState
	if state.Error == "" && state.Notice == "" && !state.Closing {
		return
	}
	mgo.Column(c.Key("local-state-status")).Padding(8, 20).Gap(4).Children(func() {
		if state.Closing {
			mgo.Text(c, model.T("Saving drafts and closing…")).FontSize(11)
			return
		}
		if state.Notice != "" {
			mgo.Text(c, model.T(state.Notice)).FontSize(11).TextColor(c.Theme().Warning)
			if state.RecoverDraft != nil {
				mgo.Button(c, model.T("Copy previous draft here")).OnClick(state.RecoverDraft)
			}
		}
		if state.Error != "" {
			mgo.Text(c, model.T(state.Error)).FontSize(11).TextColor(c.Theme().Warning)
			mgo.Row(c).Gap(8).Children(func() {
				if state.Retry != nil {
					mgo.Button(c, model.T("Retry saving drafts")).OnClick(state.Retry)
				}
				if state.QuitWithoutSaving != nil {
					mgo.Button(c, model.T("Quit without saving drafts")).OnClick(state.QuitWithoutSaving)
				}
			})
		}
	})
}
