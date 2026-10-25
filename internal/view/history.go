package view

import (
	"dsh-mygo/internal/projection"
	mgo "github.com/egoist/mygo/ui"
)

// Discard widget buffers that no longer belong to the bounded transcript.
func (model *Model) PruneTranscriptState(items []projection.Item) {
	visible := make(map[string]bool, len(items))
	for _, item := range items {
		visible[item.ID] = true
	}
	for id := range model.ToolCards {
		if !visible[id] {
			delete(model.ToolCards, id)
		}
	}
	groups := make(map[string]bool)
	for _, row := range buildTranscriptRows(items) {
		if row.activity != nil {
			groups[row.activity.key] = true
		}
	}
	for key := range model.Activity {
		if !groups[key] {
			delete(model.Activity, key)
		}
	}
	if model.ToolInspector != nil && !visible[model.ToolInspector.ID] {
		model.ToolInspector = nil
	}
	if !visible[model.CopiedMessageID] {
		model.CopiedMessageID = ""
		model.CopiedMessageText = ""
	}
}

type HistoryState struct {
	Available           bool
	Start               uint64
	Busy, Detached      bool
	Error               string
	LoadEarlier, Latest func()
	scroll              mgo.ScrollState
	autoStart           uint64
	observedStart       uint64
}

// FinishRead releases the per-boundary guard as well as the busy flag, so a
// canceled background read can be requested again when the reader returns.
func (h *HistoryState) FinishRead() {
	h.Busy = false
	h.autoStart = 0
}

func (model *Model) CanLoadHistory() bool {
	return model.History.Available && !model.History.Busy && model.CanChooseSession() && !model.SessionsOpen
}

// Called after building the virtual list. Each durable boundary schedules at
// most one guarded read; stable list keys keep the reader's anchor on prepend.
func autoLoadHistory(c *mgo.Context, model *Model, rows int) {
	h := &model.History
	_, last := model.List.Visible()
	// Let List commit the new keys and anchor before testing its top edge.
	if h.observedStart != h.Start {
		h.observedStart = h.Start
		c.After(0)
		return
	}
	if !model.CanLoadHistory() || h.Busy || h.Start == 0 || h.LoadEarlier == nil || h.Error != "" || h.autoStart == h.Start {
		return
	}
	if last < 0 && rows > 0 || h.scroll.Y > 160 {
		return
	}
	h.autoStart = h.Start
	h.LoadEarlier()
}

func historyControls(c *mgo.Context, model *Model) {
	h := &model.History
	if h.Error == "" {
		return
	}
	mgo.Row(c.Key("history-error")).FillWidth().MaxWidth(readingWidth).Gap(space8).AlignItems(mgo.Center).Children(func() {
		mgo.Text(c, model.T(h.Error)).FontSize(textMicro).TextColor(c.Theme().Danger).Grow(1)
		iconButton(c.Key("retry-history"), model.T("Retry loading earlier messages"), iconRefresh).
			Disabled(!model.CanLoadHistory() || h.Busy || h.LoadEarlier == nil).OnClick(func() {
			h.Error = ""
			h.LoadEarlier()
		})
	})
}

func historyJumpButton(c *mgo.Context, model *Model) {
	h := &model.History
	_, last := model.List.Visible()
	if model.SessionSwitching || model.RecoveryOpen || overlayActive(model) || !h.Detached && (last < 0 || model.List.AtEnd()) {
		return
	}
	mgo.Box(c.Key("history-jump-anchor")).FillWidth().MaxWidth(readingWidth).Height(0).Children(func() {
		button := iconButton(c.Key("jump-to-latest"), model.T("Return to latest"), iconDown).
			Attach(mgo.AnchorTop, mgo.AnchorBottom).Margin(0, 0, space12, 0).Size(36, 36).Padding(space8).Radius(radiusPill).
			Background(tokensFor(c.Theme()).SurfaceRaised).Border(1, tokensFor(c.Theme()).BorderStrong).
			Disabled(h.Detached && (!model.CanLoadHistory() || h.Latest == nil)).OnClick(func() {
			if h.Detached {
				h.Latest()
			} else {
				model.List.ScrollToEnd()
			}
		})
		shadowFloating.apply(button)
	})
}

func sessionLoading(c *mgo.Context, model *Model) {
	mgo.Column(c.Key("session-loading")).Grow(1).MinHeight(0).FillWidth().Justify(mgo.Center).AlignItems(mgo.Center).Gap(space12).Children(func() {
		mgo.Spinner(c.Key("session-loading-spinner")).Size(20, 20)
		mgo.Text(c, model.T("Loading session history…")).FontSize(textCaption).TextColor(c.Theme().TextMuted)
	})
}
