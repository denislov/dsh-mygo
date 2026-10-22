package view

import (
	"fmt"

	mgo "github.com/egoist/mygo/ui"
)

func approvalModal(c *mgo.Context, model *Model) {
	if !model.CanApprove || len(model.Approvals) == 0 {
		return
	}
	request := model.Approvals[0]
	disabled := request.Responding || model.CancelPending || model.RespondApproval == nil
	modal := mgo.Modal(c.Key("approval-"+request.RequestID), &model.ApprovalOpen, func() {
		mgo.Text(c, "Tool approval").FontSize(18).Bold()
		mgo.Text(c, fmt.Sprintf("Approval 1 of %d", len(model.Approvals))).FontSize(11)
		mgo.Text(c, request.ToolName).FontSize(15).Bold()
		if request.External {
			mgo.Text(c, "Unlinked approval: verify this request before allowing.").TextColor(c.Theme().Warning)
		}
		if request.Reason != "" {
			mgo.Text(c, request.Reason).FontSize(13)
		}
		if request.Arguments != "" {
			mgo.Scroll(c.Key("approval-arguments")).MaxHeight(220).FillWidth().Selectable().Children(func() {
				mgo.Text(c, request.Arguments).FontSize(12)
			})
		}
		if request.Error != "" {
			mgo.Text(c, request.Error).TextColor(c.Theme().Danger)
		}
		if request.Responding {
			mgo.Text(c, "Sending decision…").FontSize(11)
		}
		mgo.Row(c).Gap(8).Children(func() {
			if model.CanCancel && model.Cancel != nil {
				if mgo.Button(c.Key("approval-stop"), "Stop turn").Disabled(model.CancelPending).Clicked() && !model.CancelPending {
					model.Cancel()
				}
			}
			mgo.Spacer(c)
			if mgo.Button(c.Key("approval-deny"), "Deny").Disabled(disabled).Clicked() && !disabled {
				model.RespondApproval(request.RequestID, "deny")
			}
			if mgo.PrimaryButton(c.Key("approval-allow"), "Allow once").Disabled(disabled).Clicked() && !disabled {
				model.RespondApproval(request.RequestID, "allow")
			}
		})
	})
	if modal.Dismissed() {
		model.ApprovalOpen = true
		if !disabled {
			model.RespondApproval(request.RequestID, "deny")
		}
	}
}
