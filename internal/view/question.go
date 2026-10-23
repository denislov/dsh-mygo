package view

import (
	"fmt"

	mgo "github.com/egoist/mygo/ui"
)

func questionModal(c *mgo.Context, model *Model) {
	if !model.CanQuestion || len(model.Questions) == 0 || len(model.Approvals) > 0 || model.Phase == PhaseResyncing {
		return
	}
	request := model.Questions[0]
	disabled := request.Responding || model.CancelPending || model.RespondQuestion == nil
	modal := mgo.Modal(c.Key("question-"+request.RequestID), &model.QuestionOpen, func() {
		mgo.Text(c, model.T("User questions")).FontSize(18).Bold()
		mgo.Text(c, fmt.Sprintf(model.T("Request 1 of %d"), len(model.Questions))).FontSize(11)
		if request.SessionID == "" {
			mgo.Text(c, model.T("Host question (not linked to a session)")).FontSize(11)
		}
		mgo.Scroll(c.Key("question-form")).MaxHeight(360).FillWidth().Children(func() {
			mgo.Column(c).Gap(12).Children(func() {
				for index, question := range request.Questions {
					draft := &request.Drafts[index]
					mgo.Column(c.Key(question.ID)).Gap(6).Children(func() {
						if question.Header != "" {
							mgo.Text(c, question.Header).FontSize(11).Bold()
						}
						mgo.Text(c, question.Question).FontSize(14).Bold()
						if question.Detail != "" {
							mgo.Text(c, question.Detail).FontSize(12)
						}
						if question.MultiSelect {
							mgo.Text(c, model.T("Select any that apply; custom text may be added.")).FontSize(11)
						}
						for optionIndex, option := range question.Options {
							mgo.Checkbox(c.Key(fmt.Sprintf("option-%d", optionIndex)), &draft.Selected[optionIndex], option.Label).Disabled(disabled).OnChange(func() {
								if !question.MultiSelect && draft.Selected[optionIndex] {
									for otherIndex := range draft.Selected {
										draft.Selected[otherIndex] = otherIndex == optionIndex
									}
									draft.Custom = ""
								}
							})
							if option.Description != "" {
								mgo.Text(c, option.Description).FontSize(11).TextColor(c.Theme().TextMuted)
							}
						}
						mgo.TextInput(c.Key("custom"), &draft.Custom).Label(model.T("Answer: ") + question.ID).Placeholder(model.T("Your answer / Other…")).Disabled(disabled).OnChange(func() {
							if !question.MultiSelect && draft.Custom != "" {
								for optionIndex := range draft.Selected {
									draft.Selected[optionIndex] = false
								}
							}
						})
					})
				}
			})
		})
		if request.Error != "" {
			mgo.Text(c, model.T(request.Error)).TextColor(c.Theme().Danger)
		}
		if request.Responding {
			mgo.Text(c, model.T("Sending answer…")).FontSize(11)
		}
		mgo.Row(c).Gap(8).Children(func() {
			if request.SessionID != "" && model.CanCancel && model.Cancel != nil {
				if mgo.Button(c.Key("question-stop"), model.T("Stop turn")).Disabled(model.CancelPending).Clicked() && !model.CancelPending {
					model.Cancel()
				}
			}
			mgo.Spacer(c)
			if mgo.Button(c.Key("question-cancel"), model.T("Cancel question")).Disabled(disabled).Clicked() && !disabled {
				model.RespondQuestion(request.RequestID, true)
			}
			if mgo.PrimaryButton(c.Key("question-submit"), model.T("Submit answers")).Disabled(disabled).Clicked() && !disabled {
				model.RespondQuestion(request.RequestID, false)
			}
		})
	})
	if modal.Dismissed() {
		model.QuestionOpen = true
		if !disabled {
			model.RespondQuestion(request.RequestID, true)
		}
	}
}
