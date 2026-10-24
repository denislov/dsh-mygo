package view

import (
	"dsh-mygo/internal/projection"

	mgo "github.com/egoist/mygo/ui"
)

type SubagentSummary struct {
	ID        string
	Label     string
	Mode      string
	Status    string
	CreatedAt int64
}

type SubagentPanelState struct {
	Available, Open, Busy             bool
	ParentID, Selected, Error         string
	Children                          []SubagentSummary
	Content                           *Model
	List                              mgo.ListState
	Start, Inherited                  uint64
	HasMore, Earlier                  bool
	EndReason                         string
	Show, Close, Refresh, EarlierPage func()
	Select                            func(string)
}

func (p *SubagentPanelState) HasLocalHistory() bool {
	if p.Selected == "" {
		return true
	}
	for _, child := range p.Children {
		if child.ID == p.Selected {
			return child.Mode == "one-shot" || child.Mode == "continuable"
		}
	}
	return false
}

func (model *Model) CanInspectSubagents() bool {
	return !model.Runtime.SetupRequired && model.Inspection.Available && !model.Inspection.Open && !model.Setup.Picking && model.SessionID != "" && (model.Phase == PhaseIdle || model.Phase == PhaseRunning) && !model.SessionBusy && !model.LocalState.Closing && !model.RecoveryOpen && !model.SettingsOpen && !model.SettingsBusy && !model.SessionsOpen && model.ToolInspector == nil && len(model.Approvals) == 0 && len(model.Questions) == 0
}

func childLabel(child SubagentSummary) string {
	if child.Label != "" {
		return child.Label
	}
	return child.ID
}

func childStatus(model *Model, child SubagentSummary) string {
	for _, live := range model.Subagents {
		if live.ID == child.ID {
			if live.Running {
				return model.T("Working")
			}
			return model.T(subagentOutcome(live.Reason))
		}
	}
	if child.Status == "running" {
		return model.T("Working")
	}
	if child.Status == "idle" {
		return model.T("Ready")
	}
	if child.Status == "external" {
		return model.T("External task")
	}
	return model.T("Saved task")
}

func subagentOutcome(reason string) string {
	switch reason {
	case "completed":
		return "Completed"
	case "aborted":
		return "Cancelled"
	case "error":
		return "Failed"
	case "max-tokens":
		return "Token limit reached"
	case "refusal":
		return "Refused"
	default:
		return "Saved task"
	}
}

func subagentModal(c *mgo.Context, model *Model) {
	p := &model.Inspection
	if !p.Open || !p.Available || model.RecoveryOpen || model.Phase == PhaseResyncing || len(model.Approvals) > 0 || len(model.Questions) > 0 {
		return
	}
	_, height := c.Size()
	modal := mgo.Modal(c.Key("subagents-"+p.ParentID), &p.Open, func() {
		mgo.Text(c, model.T("Subagents")).FontSize(18).Bold()
		mgo.Text(c, model.T("Read-only tasks and output")).FontSize(11).TextColor(c.Theme().TextMuted)
		mgo.Text(c, model.T("Parent conversation · ")+model.ActiveSessionLabel()).FontSize(11).SingleLine().TextColor(c.Theme().TextMuted)
		if p.Error != "" {
			mgo.Text(c, model.T(p.Error)).FontSize(12).TextColor(c.Theme().Danger)
		}
		if p.Selected == "" {
			if p.Busy {
				mgo.Text(c, model.T("Loading subagents…")).FontSize(12)
			}
			if !p.Busy && len(p.Children) == 0 && p.Error == "" {
				mgo.Text(c, model.T("No subagents in this conversation.")).FontSize(13)
			}
			mgo.List(c.Key("child-list"), &p.List, len(p.Children)).Height(max(80, min(300, height-230))).FillWidth().
				ItemKey(func(index int) any { return p.Children[index].ID }).Rows(func(row mgo.ListRow) {
				child := p.Children[row.Index]
				mgo.Button(c.Key(child.ID), "").Label(model.T("Inspect · ")+childLabel(child)).FillWidth().Padding(10, 12).Disabled(p.Busy || p.Select == nil).
					OnClick(func() { p.Select(child.ID) }).Children(func() {
					mgo.Column(c).FillWidth().Gap(4).Children(func() {
						mgo.Text(c, childLabel(child)).FontSize(13).Bold().SingleLine()
						mgo.Text(c, childStatus(model, child)).FontSize(11).TextColor(c.Theme().TextMuted)
					})
				})
			})
		} else {
			subagentDetail(c, model, max(70, min(360, height-270)))
		}
		mgo.Row(c).Gap(8).Children(func() {
			mgo.Button(c.Key("close"), model.T("Close subagents")).OnClick(p.Close)
			mgo.Spacer(c)
			if model.Phase == PhaseRunning && model.CanCancel && model.Cancel != nil {
				mgo.Button(c.Key("parent-stop"), model.T("Stop turn")).Disabled(model.CancelPending).OnClick(model.Cancel)
			}
			mgo.Button(c.Key("refresh"), model.T("Refresh")).Disabled(p.Busy || !p.HasLocalHistory() || p.Refresh == nil).OnClick(p.Refresh)
		})
	})
	if modal.Dismissed() && p.Close != nil {
		p.Close()
	}
}

func subagentDetail(c *mgo.Context, model *Model, bodyHeight float32) {
	p := &model.Inspection
	mgo.Row(c).Gap(8).Children(func() {
		mgo.Button(c.Key("back-to-children"), model.T("All subagents")).Disabled(p.Busy || p.Select == nil).OnClick(func() { p.Select("") })
		if p.Content != nil {
			mgo.Button(c.Key("earlier-child"), model.T("Earlier activity")).Disabled(p.Busy || !p.HasMore || p.EarlierPage == nil).OnClick(p.EarlierPage)
			if p.Earlier {
				mgo.Button(c.Key("latest-child"), model.T("Latest activity")).Disabled(p.Busy).OnClick(p.Refresh)
			}
		}
	})
	for _, child := range p.Children {
		if child.ID != p.Selected {
			continue
		}
		mgo.Text(c, childLabel(child)).FontSize(14).Bold().SingleLine().Tooltip(child.ID)
		status := childStatus(model, child)
		if p.EndReason != "" && status != model.T("Working") {
			status = model.T(subagentOutcome(p.EndReason))
		}
		mgo.Text(c, status).FontSize(11).TextColor(c.Theme().TextMuted)
		if child.Mode == "external" || child.Mode == "unknown" {
			mgo.Text(c, model.T("No local history is available for this subagent.")).FontSize(12)
			return
		}
		break
	}
	if p.Busy {
		mgo.Text(c, model.T("Loading task output…")).FontSize(12)
	}
	if p.Content == nil {
		mgo.Box(c).Height(bodyHeight)
		return
	}
	content := p.Content
	content.Prefs.Value = model.Prefs.Value
	content.Markdown.OnLink = model.Markdown.OnLink
	content.InterruptedMarkdown.OnLink = model.InterruptedMarkdown.OnLink
	mgo.List(c.Key("child-history-"+p.Selected), &content.List, len(content.Items)).Height(bodyHeight).FillWidth().Gap(18).Padding(8, 0).
		ItemKey(func(index int) any { return content.Items[index].ID }).Rows(func(row mgo.ListRow) {
		item := content.Items[row.Index]
		if item.Kind != projection.KindTool {
			message(c, content, item)
			return
		}
		mgo.Column(c.Key(item.ID)).FillWidth().Gap(6).Children(func() {
			mgo.Collapsible(c.Key("child-tool"), content.toolTitle(item.Title), &content.toolCardState(item.ID).Expanded, func() {
				mgo.Text(c, item.Text).FontSize(12).Selectable()
				mgo.Text(c, item.Detail).FontSize(12).Selectable()
			}).FillWidth()
			if item.State != "" {
				mgo.Text(c, model.T(toolState(item.State))).FontSize(11).TextColor(c.Theme().TextMuted)
			}
		})
	}).Children(func() {
		if len(content.Items) == 0 {
			mgo.Text(c, model.T("No visible messages in this activity window.")).FontSize(12)
		}
	})
}
