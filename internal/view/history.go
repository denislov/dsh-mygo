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
}

func (model *Model) CanLoadHistory() bool {
	return model.History.Available && model.CanChooseSession() && !model.SessionsOpen
}

func historyControls(c *mgo.Context, model *Model) {
	h := &model.History
	if !h.Available || h.Start == 0 && !h.Detached && h.Error == "" {
		return
	}
	mgo.Column(c.Key("history-controls")).Padding(6, 20).Gap(4).Children(func() {
		mgo.Row(c).Gap(8).Children(func() {
			if h.Start > 0 {
				mgo.Button(c.Key("earlier"), model.T("Load earlier messages")).Disabled(!model.CanLoadHistory() || h.LoadEarlier == nil).OnClick(h.LoadEarlier)
			}
			mgo.Button(c.Key("latest"), model.T("Return to latest")).Disabled(!model.CanLoadHistory() || h.Latest == nil).OnClick(h.Latest)
			if h.Busy {
				mgo.Text(c, model.T("Loading history…")).FontSize(11)
			}
		})
		if h.Detached {
			mgo.Text(c, model.T("Viewing earlier history. Return to latest before sending.")).FontSize(11).TextColor(c.Theme().TextMuted)
		}
		if h.Error != "" {
			mgo.Text(c, model.T(h.Error)).FontSize(11).TextColor(c.Theme().Danger)
		}
	})
}
