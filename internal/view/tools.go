package view

import (
	"fmt"
	"strings"

	"dsh-mygo/internal/projection"
	mgo "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func (model *Model) toolTitle(title string) string {
	if strings.HasPrefix(title, "Tool · ") {
		return model.T("Tool · ") + strings.TrimPrefix(title, "Tool · ")
	}
	return model.T(title)
}

type ToolCardState struct{ Expanded bool }

type ToolInspectorState struct {
	ID        string
	Open      bool
	Tab       int
	Change    int
	Diff      *editor.DiffState
	source    *projection.ToolDetails
	diffIndex int
}

func (model *Model) toolCardState(identity string) *ToolCardState {
	if model.ToolCards == nil {
		model.ToolCards = make(map[string]*ToolCardState)
	}
	if model.ToolCards[identity] == nil {
		model.ToolCards[identity] = &ToolCardState{}
	}
	return model.ToolCards[identity]
}

func (model *Model) OpenToolDetails(identity string) {
	model.ToolInspector = &ToolInspectorState{ID: identity, Open: true}
}

func (inspector *ToolInspectorState) prepareDiff(details *projection.ToolDetails) {
	if inspector.source != details || inspector.diffIndex != inspector.Change {
		inspector.source = details
		inspector.diffIndex = inspector.Change
		inspector.Diff = nil
	}
	if details == nil || inspector.Change < 0 || inspector.Change >= len(details.Diffs) || inspector.Diff != nil {
		return
	}
	change := details.Diffs[inspector.Change]
	if len(change.OldText)+len(change.NewText) > 16*1024 || strings.Count(change.OldText, "\n")+strings.Count(change.NewText, "\n") > 400 {
		return
	}
	inspector.Diff = &editor.DiffState{Old: editor.NewBuffer(change.OldText), New: editor.NewBuffer(change.NewText), Layout: editor.DiffUnified, Inline: editor.DiffInlineNone, Indicators: editor.DiffClassic, Wrap: true}
}

func toolInspectorModal(c *mgo.Context, model *Model) {
	inspector := model.ToolInspector
	if inspector == nil {
		return
	}
	var item *projection.Item
	for index := range model.Items {
		if model.Items[index].ID == inspector.ID {
			item = &model.Items[index]
			break
		}
	}
	if item == nil {
		model.ToolInspector = nil
		return
	}
	if len(model.Approvals) > 0 || len(model.Questions) > 0 || model.SessionsOpen || model.SettingsOpen {
		return
	}
	if inspector.source != item.Tool {
		inspector.source = item.Tool
		inspector.Diff = nil
	}
	modal := mgo.Modal(c.Key("tool-inspector-"+inspector.ID), &inspector.Open, func() {
		mgo.Text(c, model.T("Tool details")).FontSize(18).Bold()
		mgo.Text(c, item.Title).FontSize(13)
		mgo.Row(c).Gap(8).Children(func() {
			for index, label := range []string{"Arguments", "Result", "Changes"} {
				if mgo.Button(c.Key(label), model.T(label)).Clicked() {
					inspector.Tab = index
				}
			}
		})
		if item.ToolEvicted {
			mgo.Text(c, model.T("Full details evicted from the bounded cache; only previews remain.")).TextColor(c.Theme().Warning)
		}
		if item.Tool != nil && item.Tool.Notice != "" {
			mgo.Text(c, item.Tool.Notice).TextColor(c.Theme().Warning)
		}
		switch inspector.Tab {
		case 0:
			text, truncated := item.Text, false
			if item.Tool != nil {
				text, truncated = item.Tool.Arguments, item.Tool.ArgumentsTruncated
			}
			toolDetailText(c, model, "arguments", text, truncated)
		case 1:
			if item.IsError {
				mgo.Text(c, "Tool error: "+item.Detail).TextColor(c.Theme().Danger)
			}
			text, truncated := item.Detail, false
			if item.Tool != nil {
				text, truncated = item.Tool.Result, item.Tool.ResultTruncated
			}
			toolDetailText(c, model, "result", text, truncated)
		case 2:
			toolChanges(c, model, inspector, item.Tool)
		}
		if mgo.Button(c.Key("close-tool-details"), model.T("Close details")).Clicked() {
			inspector.Open = false
		}
	})
	if modal.Dismissed() || !inspector.Open {
		model.ToolInspector = nil
	}
}

func toolDetailText(c *mgo.Context, model *Model, key, text string, truncated bool) {
	if truncated {
		mgo.Text(c, model.T("Truncated: only the first 64 KiB is retained.")).TextColor(c.Theme().Warning)
	}
	if text == "" {
		mgo.Text(c, model.T("No recorded text.")).FontSize(12)
		return
	}
	mgo.Scroll(c.Key(key)).MaxHeight(300).FillWidth().Selectable().Children(func() { mgo.Text(c, text).FontSize(12) })
}

func toolChanges(c *mgo.Context, model *Model, inspector *ToolInspectorState, details *projection.ToolDetails) {
	if details == nil || len(details.Diffs) == 0 {
		message := "No structured diff metadata was recorded; see Result."
		if details != nil && details.DiffNotice != "" {
			message = details.DiffNotice
		}
		mgo.Text(c, model.T(message)).FontSize(12)
		return
	}
	if inspector.Change >= len(details.Diffs) {
		inspector.Change = 0
	}
	mgo.Row(c).Gap(8).Children(func() {
		if mgo.Button(c.Key("previous-change"), model.T("Previous change")).Disabled(inspector.Change == 0).Clicked() && inspector.Change > 0 {
			inspector.Change--
		}
		if mgo.Button(c.Key("next-change"), model.T("Next change")).Disabled(inspector.Change+1 >= len(details.Diffs)).Clicked() && inspector.Change+1 < len(details.Diffs) {
			inspector.Change++
		}
		mgo.Text(c, fmt.Sprintf(model.T("Recorded hunk %d of %d"), inspector.Change+1, len(details.Diffs))).FontSize(11)
	})
	change := details.Diffs[inspector.Change]
	mgo.Text(c, change.Path).FontSize(12).Bold().SingleLine()
	mgo.Text(c, model.T("Recorded context, not a complete file snapshot. Read-only.")).FontSize(11)
	if change.OldTextMissing {
		mgo.Text(c, model.T("Insertion: no old text was recorded for this hunk.")).FontSize(11)
	}
	inspector.prepareDiff(details)
	if inspector.Diff != nil {
		editor.DiffView(c.Key(fmt.Sprintf("diff-%d", inspector.Change)), inspector.Diff).Height(260).FillWidth().Label(model.T("Recorded change"))
		return
	}
	mgo.Text(c, model.T("Large hunk: read-only before/after text (not a computed diff).")).FontSize(11)
	mgo.Scroll(c.Key("large-diff")).MaxHeight(260).FillWidth().Selectable().Children(func() {
		mgo.Column(c).Gap(6).Children(func() {
			mgo.Text(c, model.T("Before")).Bold()
			mgo.Text(c, change.OldText).FontSize(12)
			mgo.Text(c, model.T("After")).Bold()
			mgo.Text(c, change.NewText).FontSize(12)
		})
	})
}
