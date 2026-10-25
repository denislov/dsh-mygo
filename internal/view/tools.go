package view

import (
	"encoding/json"
	"fmt"
	"strings"

	"dsh-mygo/internal/projection"
	mgo "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func toolAction(title string) string {
	name := strings.ToLower(strings.TrimPrefix(title, "Tool · "))
	switch name {
	case "exec_command", "bash", "pwsh", "shell", "shell_command", "run_command", "terminal":
		return "Run command"
	case "read", "read_file", "view_file":
		return "Read"
	case "edit", "edit_file", "write", "write_file", "apply_patch":
		return "Edit file"
	case "grep", "glob", "search", "find", "list_files":
		return "Search files"
	default:
		return strings.TrimPrefix(title, "Tool · ")
	}
}

// Only recognized, user-facing argument fields become a summary. Avoid dumping
// arbitrary JSON, file contents or a tool's entire result into the activity row.
func toolSummary(item projection.Item) string {
	var fields map[string]json.RawMessage
	source := item.Text
	if item.Tool != nil && item.Tool.Arguments != "" && !item.Tool.ArgumentsTruncated {
		source = item.Tool.Arguments
	}
	if len(source) > projection.MaxToolDetailBytes || json.Unmarshal([]byte(source), &fields) != nil {
		return ""
	}
	for _, key := range []string{"description", "file_path", "path", "command", "cmd", "query", "pattern"} {
		var value string
		if json.Unmarshal(fields[key], &value) == nil && strings.TrimSpace(value) != "" {
			return singleLineSummary(value, 180)
		}
	}
	return ""
}

func (model *Model) toolTitle(title string) string {
	if strings.HasPrefix(title, "Tool · ") {
		return model.T("Tool · ") + strings.TrimPrefix(title, "Tool · ")
	}
	return model.T(title)
}

func toolIcon(title string) *mgo.SVG {
	name := strings.ToLower(title)
	switch {
	case strings.Contains(name, "bash"), strings.Contains(name, "pwsh"), strings.Contains(name, "shell"), strings.Contains(name, "exec"), strings.Contains(name, "terminal"):
		return iconTerminal
	case strings.Contains(name, "edit"), strings.Contains(name, "write"), strings.Contains(name, "patch"), strings.Contains(name, "apply"):
		return iconEdit
	case strings.Contains(name, "read"), strings.Contains(name, "view"), strings.Contains(name, "glob"), strings.Contains(name, "grep"), strings.Contains(name, "search"), strings.Contains(name, "find"):
		return iconSearch
	case strings.Contains(name, "agent"), strings.Contains(name, "task"), strings.Contains(name, "subagent"):
		return iconAgents
	case strings.Contains(name, "image"):
		return iconImage
	case strings.Contains(name, "folder"), strings.Contains(name, "list"):
		return iconFolder
	default:
		return iconFile
	}
}

func toolStatusPill(c *mgo.Context, model *Model, item projection.Item) mgo.Element {
	tokens := tokensFor(c.Theme())
	foreground, background := tokens.TextMuted, tokens.Surface
	label := model.T(toolState(item.State))
	switch {
	case item.IsError || item.State == "error":
		foreground, background = tokens.Danger, tokens.DangerSoft
	case item.State == "running":
		foreground, background = tokens.Accent, tokens.AccentSoft
	case item.State == "done":
		foreground, background = tokens.Success, tokens.SuccessSoft
	}
	return statusPill(c, "tool-state-"+item.ID, label, foreground, background)
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
		tokens := tokensFor(c.Theme())
		mgo.Row(c).Gap(space4).Children(func() {
			for index, label := range []string{"Arguments", "Result", "Changes"} {
				background := mgo.Transparent
				foreground := tokens.TextMuted
				if inspector.Tab == index {
					background, foreground = tokens.AccentSoft, tokens.Accent
				}
				if mgo.Button(c.Key(label), model.T(label)).Padding(space4, space12).Radius(radiusSm).
					Background(background).TextColor(foreground).Clicked() {
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
