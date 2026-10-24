package view

import (
	"fmt"
	"strings"

	"dsh-mygo/internal/attachment"
	"github.com/egoist/mygo/transfer"
	mgo "github.com/egoist/mygo/ui"
)

func composer(c *mgo.Context, model *Model) {
	theme := c.Theme()
	services := c.Services()
	stopped := model.Phase == PhaseStopped || model.Phase == PhaseFailed || model.Phase == PhaseResyncing || model.SessionSwitching || model.RecoveryOpen || model.LocalState.Closing || model.Prefs.Open || model.Inspection.Open
	canSend := model.Phase == PhaseIdle && !model.SetupBlocked() && model.Send != nil && len(model.Questions) == 0 && !model.SessionBusy && !model.History.Detached && !model.SessionPanel.BlocksInput() && !model.SettingsOpen && !model.Prefs.Open && !model.Inspection.Open && !model.SettingsBusy && !model.ModelUncertain && !model.ConfigurationUncertain && !model.Permission.Uncertain && !model.LocalState.Closing
	attachments := &model.Attachments
	hasContent := func() bool { return strings.TrimSpace(model.Draft) != "" || len(attachments.Pending) > 0 }
	zone := mgo.Column(c.Key("composer-zone")).Padding(10, 24, 14, 24).Gap(7).AlignItems(mgo.Center)
	if paths := zone.DroppedFiles(); len(paths) > 0 && attachments.AttachFiles != nil {
		attachments.AttachFiles(paths)
	}
	if zone.FileDragOver() {
		zone.Border(1, theme.Accent)
	}
	zone.Children(func() {
		surface := mgo.Column(c.Key("composer-surface")).FillWidth().MaxWidth(readingWidth).Padding(8, 10).Gap(4).Radius(18).
			Background(theme.Surface).Border(1, theme.Border).Transition(quietTransition)
		if surface.FocusWithin() {
			surface.Border(1, theme.Accent)
		}
		surface.Children(func() {
			if len(attachments.Pending) > 0 || attachments.Error != "" {
				_, windowHeight := c.Size()
				mgo.Scroll(c.Key("staged-attachments")).MaxHeight(max(44, min(96, windowHeight-500))).FillWidth().Children(func() {
					mgo.Column(c).FillWidth().Gap(5).Children(func() {
						for index, draft := range attachments.Pending {
							mgo.Row(c.Key(fmt.Sprintf("attachment-%d", index))).Gap(8).AlignItems(mgo.Center).Children(func() {
								if preview := attachments.Preview(index); preview != nil {
									mgo.Image(c, preview).Size(48, 40).Fit(mgo.Cover).Radius(6)
								}
								mgo.Text(c, draftLabel(model, draft)).FontSize(11).TextColor(theme.TextMuted).SingleLine()
								remove := fmt.Sprintf("%s %d", model.T("Remove"), index+1)
								mgo.Spacer(c)
								mgo.Toolbar(c.Key("attachment-actions"), func() {
									iconButton(c.Key("remove"), remove, iconClose).Disabled(stopped).OnClick(func() { attachments.Remove(index) })
								}).Padding(0)
							})
						}
						if attachments.Error != "" {
							mgo.Text(c, model.T(attachments.Error)).FontSize(11).TextColor(theme.Danger)
						}
					})
				})
			}
			area := mgo.TextArea(c.Key("composer-"+model.SessionID), &model.Draft).Lines(2, composerLines(c)).MinHeight(0).
				FontSize(15).Padding(6, 8).Border(0, mgo.Transparent).FocusRing(false).Background(mgo.Transparent).
				Label(model.T("Message")).Placeholder(model.T("Message DeepSeek…")).AutoFocus().Disabled(stopped).
				OnChange(func() {
					if model.DraftChanged != nil {
						model.DraftChanged()
					}
				}).
				OnPaste(func(text string) bool {
					if text != "" || attachments.AttachImage == nil {
						return false
					}
					png, err := services.ReadClipboardData(transfer.PNG).Read(transfer.PNG)
					if err != nil || len(png) == 0 {
						return false
					}
					attachments.AttachImage("pasted.png", attachment.MediaPNG, png)
					return true
				}).
				OnTextKey(func(key mgo.TextKey) bool {
					if mentionKey(model, key) {
						return true
					}
					mods := mgo.Modifiers(0)
					if model.Prefs.Value.Send == "Ctrl+Enter" {
						mods = mgo.Ctrl
					}
					if key.Key != mgo.KeyEnter || key.Mods != mods || model.Prefs.Open {
						return false
					}
					text := strings.TrimSpace(key.Text())
					if !canSend || text == "" && len(attachments.Pending) == 0 {
						return false
					}
					model.Send(key.Text())
					key.SetText("")
					return true
				})
			if model.Mention.CaretTarget >= 0 {
				area.SetTextSelection(model.Mention.CaretTarget, model.Mention.CaretTarget)
				model.Mention.CaretTarget = -1
			}
			composerMentions(c, area, model)
			mgo.Row(c.Key("composer-controls")).FillWidth().Gap(4).AlignItems(mgo.Center).Children(func() {
				mgo.Toolbar(c.Key("input-options"), func() {
					if attachments.Supported && attachments.PickAttachment != nil && model.Phase == PhaseIdle {
						iconButton(c.Key("attach-image"), model.T("Attach image…"), iconAttach).Disabled(stopped).OnClick(attachments.PickAttachment)
					}
					if (model.CanModels || model.Permission.Available) && model.OpenSettings != nil {
						label := model.ActiveRoute.Model
						if label == "" {
							label = model.ModelLabel
						}
						if label == "" {
							label = model.T("Settings")
						}
						mgo.Button(c.Key("settings"), "").Label(model.T("Settings")).Tooltip(model.T("Settings")).MaxWidth(180).
							Disabled(!model.CanEditSettings()).OnClick(model.OpenSettings).Children(func() {
							mgo.Text(c, label).FontSize(12).SingleLine()
							mgo.Icon(c, iconChevron).Size(12, 12).TextColor(theme.TextMuted)
						})
					}
				}).Padding(0).MinWidth(0)
				mgo.Spacer(c)
				if model.Phase == PhaseRunning && model.CanCancel && model.Cancel != nil {
					label := "Stop"
					if model.CancelPending {
						label = "Stopping…"
					}
					if iconButton(c.Key("stop"), model.T(label), iconStop).Size(34, 34).Disabled(model.CancelPending).Clicked() && !model.CancelPending {
						model.Cancel()
					}
				} else {
					send := mgo.PrimaryButton(c.Key("send"), "").Label(model.T("Send")).Tooltip(model.T("Send")).Size(34, 34).Padding(7).Radius(11).
						Children(func() { mgo.Icon(c, iconSend).Size(18, 18) }).Transition(quietTransition).
						Disabled(!canSend || !hasContent())
					if send.Clicked() {
						if canSend && hasContent() {
							model.Send(model.Draft)
						}
					}
				}
			})
		})
		mgo.Text(c.Key("composer-hint"), model.T(composerHint(model))).FontSize(10).TextColor(theme.TextMuted).SingleLine()
	})
}

func composerLines(c *mgo.Context) int {
	_, height := c.Size()
	if height < 560 {
		return 3
	}
	return 8
}
