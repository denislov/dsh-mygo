package view

import (
	"fmt"
	"strings"

	mgo "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"dsh-mygo/internal/projection"
)

// transcriptRow is one row of the transcript list: either a plain item or an
// activity group that folds a turn's process rows together.
type transcriptRow struct {
	key      string
	item     *projection.Item
	activity *activityGroup
}

type activityGroup struct {
	key   string
	items []projection.Item
}

// ActivityState is device-local disclosure state for one activity group.
type ActivityState struct{ Expanded bool }

func (model *Model) activityState(key string) *ActivityState {
	if model.Activity == nil {
		model.Activity = make(map[string]*ActivityState)
	}
	if model.Activity[key] == nil {
		model.Activity[key] = &ActivityState{}
	}
	return model.Activity[key]
}

// buildTranscriptRows folds a turn's process rows together. A turn's process is
// its reasoning, tool calls, and any assistant text before the final answer;
// only the final answer stays a standalone, copyable message. A turn that ends
// in tool activity has no final answer, so all of its text joins the process.
// Rows without a usable turn position fall back to folding adjacent reasoning
// and tool rows, which keeps previews and older history readable.
func buildTranscriptRows(items []projection.Item) []transcriptRow {
	rows := make([]transcriptRow, 0, len(items))
	used := make(map[string]int)
	for index := 0; index < len(items); {
		if group, end := turnProcessGroup(items, index); group != nil {
			rows = append(rows, groupRow(group, used))
			index = end
			continue
		}
		item := &items[index]
		if !isActivityItem(*item) {
			rows = append(rows, transcriptRow{key: item.ID, item: item})
			index++
			continue
		}
		end := index + 1
		for end < len(items) && isActivityItem(items[end]) && sameTurn(*item, items[end]) {
			end++
		}
		rows = append(rows, groupRow(&activityGroup{items: items[index:end]}, used))
		index = end
	}
	return rows
}

// turnProcessGroup folds the contiguous process rows of one turn. When the
// turn ends in an assistant row, that row is the final answer and stays
// outside the group; otherwise every process row is folded. The second return
// value is where the caller resumes after the group.
func turnProcessGroup(items []projection.Item, index int) (*activityGroup, int) {
	first := items[index]
	if first.Turn == 0 || !isProcessItem(first) {
		return nil, index
	}
	end := index + 1
	for end < len(items) && items[end].Turn == first.Turn && isProcessItem(items[end]) {
		end++
	}
	finalAnswer := items[end-1].Kind == projection.KindAssistant
	processEnd := end
	if finalAnswer {
		processEnd = end - 1
	}
	if processEnd <= index {
		return nil, index
	}
	return &activityGroup{items: items[index:processEnd]}, processEnd
}

func isProcessItem(item projection.Item) bool {
	return item.Kind == projection.KindAssistant || isActivityItem(item)
}

func isActivityItem(item projection.Item) bool {
	return item.Kind == projection.KindReasoning || item.Kind == projection.KindTool
}

func groupRow(group *activityGroup, used map[string]int) transcriptRow {
	group.key = activityGroupKey(group)
	if used[group.key] > 0 {
		group.key += ":" + group.items[0].ID
	}
	used[group.key]++
	return transcriptRow{key: group.key, activity: group}
}

func sameTurn(a, b projection.Item) bool {
	return a.Turn == 0 || b.Turn == 0 || a.Turn == b.Turn
}

// activityGroupKey prefers the first reasoning row's durable run key so the
// group keeps its expansion state when a preview is replaced by its commit.
func activityGroupKey(group *activityGroup) string {
	for _, item := range group.items {
		if item.Kind == projection.KindReasoning && item.ActivityKey != "" {
			return item.ActivityKey
		}
	}
	if len(group.items) > 0 {
		return "activity:" + group.items[0].ID
	}
	return "activity"
}

func activityCard(c *mgo.Context, model *Model, group *activityGroup) {
	tokens := tokensFor(c.Theme())
	state := model.activityState(group.key)
	summary, running, failed := model.activitySummary(group)
	card := mgo.Column(c.Key("activity-card-"+group.key)).FillWidth().MaxWidth(readingWidth).
		Padding(space8, space12).Gap(space2).Radius(radiusLg).
		Background(tokens.ActivitySurface).Transition(quietTransition).Selectable()
	if state.Expanded {
		card.Background(mgo.Transparent)
	} else if card.Hovered() {
		card.Background(tokens.ActivityHover)
	}
	card.Children(func() {
		disclosure := mgo.CollapsibleBase(c, &state.Expanded)
		disclosure.Trigger.FillWidth().Gap(space8).AlignItems(mgo.Center).Padding(space2, 0).Label(summary)
		disclosure.Trigger.Children(func() {
			mgo.Icon(c, iconActivity).Size(iconDefault, iconDefault).TextColor(tokens.TextMuted)
			mgo.Text(c, summary).FontSize(textCaption).TextColor(tokens.TextMuted).SingleLine().Grow(1)
			if running {
				mgo.Spinner(c.Key("activity-spinner-"+group.key)).Size(10, 10)
			}
			if failed {
				statusPill(c, "activity-state-"+group.key, model.T("Failed"), tokens.Danger, tokens.DangerSoft)
			}
			mgo.Icon(c, iconChevronRight).Size(iconSmall, iconSmall).TextColor(tokens.TextMuted).
				Rotate(90 * disclosure.Progress())
		})
		disclosure.Panel(func() {
			mgo.Column(c.Key("activity-body-"+group.key)).Gap(space8).Padding(space8, 0, space2, 0).Children(func() {
				mgo.Divider(c)
				for _, item := range group.items {
					switch item.Kind {
					case projection.KindAssistant:
						processMessage(c, model, item)
					case projection.KindReasoning:
						reasoningBlock(c, model, item)
					case projection.KindTool:
						toolCard(c, model, item, true)
					}
				}
			})
		})
	})
}

// processMessage shows assistant text that belongs to a turn's process. It has
// no message copy action: the turn's final answer remains the one copyable row.
func processMessage(c *mgo.Context, model *Model, item projection.Item) {
	tokens := tokensFor(c.Theme())
	mgo.Column(c.Key("process-message-"+item.ID)).FillWidth().Gap(space4).Padding(space2, 0).Children(func() {
		if item.Detail != "" {
			color := tokens.TextMuted
			if item.State == "interrupted" {
				color = tokens.Warning
			}
			mgo.Text(c, model.T(item.Detail)).FontSize(textMicro).TextColor(color)
		}
		switch item.State {
		case "streaming":
			markdown.Stream(c.Key("process-markdown-"+item.ID), item.Text, &model.Markdown).FillWidth()
		case "interrupted":
			markdown.View(c.Key("process-markdown-"+item.ID), item.Text, &model.InterruptedMarkdown).FillWidth()
		default:
			markdown.View(c.Key("process-markdown-"+item.ID), item.Text, &model.Markdown).FillWidth()
		}
		if item.Truncated {
			mgo.Text(c, model.T("Truncated message: only the visible prefix is available.")).
				FontSize(textMicro).TextColor(tokens.Warning)
		}
	})
}

func reasoningBlock(c *mgo.Context, model *Model, item projection.Item) {
	tokens := tokensFor(c.Theme())
	key := reasoningIdentity(item)
	if model.Reasoning == nil {
		model.Reasoning = make(map[string]*ActivityState)
	}
	if model.Reasoning[key] == nil {
		model.Reasoning[key] = &ActivityState{}
	}
	summary := singleLineSummary(item.Text, 180)
	mgo.Column(c.Key("reasoning-"+key)).FillWidth().Gap(space4).Padding(space4, space8).Children(func() {
		disclosure := mgo.CollapsibleBase(c, &model.Reasoning[key].Expanded)
		disclosure.Trigger.FillWidth().Gap(space8).AlignItems(mgo.Center).Padding(space2, 0).
			Label(model.T("Thought · ") + summary)
		disclosure.Trigger.Children(func() {
			mgo.Icon(c, iconActivity).Size(iconDefault, iconDefault).TextColor(tokens.TextMuted)
			mgo.Text(c, model.T("Thought")).FontSize(textCaption).TextColor(tokens.TextMuted).SingleLine()
			mgo.Text(c, "·").FontSize(textCaption).TextColor(tokens.TextFaint)
			mgo.Text(c, summary).FontSize(textCaption).TextColor(tokens.TextMuted).SingleLine().Grow(1).MinWidth(0)
			if item.State == "streaming" {
				mgo.Spinner(c.Key("thinking")).Size(10, 10)
			}
			mgo.Icon(c, iconChevronRight).Size(iconSmall, iconSmall).TextColor(tokens.TextFaint).Rotate(90 * disclosure.Progress())
		})
		disclosure.Panel(func() {
			mgo.Column(c.Key("reasoning-body")).FillWidth().Padding(space6, space8).Children(func() {
				if item.State == "streaming" {
					markdown.Stream(c.Key("reasoning-markdown-"+key), item.Text, &model.ReasoningMarkdown).FillWidth()
				} else {
					markdown.View(c.Key("reasoning-markdown-"+key), item.Text, &model.ReasoningMarkdown).FillWidth()
				}
				if item.Truncated {
					mgo.Text(c, model.T("Truncated message: only the visible prefix is available.")).
						FontSize(textMicro).TextColor(tokens.Warning)
				}
			})
		})
	})
}

func reasoningIdentity(item projection.Item) string {
	if item.ActivityKey != "" {
		return item.ActivityKey
	}
	return item.ID
}

// A bounded plain-text preview, never Markdown or a replacement for the source.
func singleLineSummary(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return text
}

// activitySummary describes the group in one line: live status first, then a
// duration, tool count, or earlier-message label. It also reports whether work
// is still running and whether any tool failed.
func (model *Model) activitySummary(group *activityGroup) (string, bool, bool) {
	thoughts, tools, messages := 0, 0, 0
	running, failed, interrupted := false, false, false
	var first, last int64
	for _, item := range group.items {
		switch item.Kind {
		case projection.KindAssistant:
			messages++
		case projection.KindReasoning:
			thoughts++
		case projection.KindTool:
			tools++
		}
		if item.State == "streaming" || item.State == "running" {
			running = true
		}
		if item.IsError || item.State == "error" {
			failed = true
		}
		if item.State == "interrupted" {
			interrupted = true
		}
		if item.Time != 0 && (first == 0 || item.Time < first) {
			first = item.Time
		}
		end := item.EndTime
		if end == 0 {
			end = item.Time
		}
		if end > last {
			last = end
		}
	}
	if interrupted && !running {
		return model.T("Activity interrupted"), false, failed
	}
	label := ""
	switch {
	case running && thoughts > 0 && tools == 0:
		label = model.T("Thinking…")
	case running && tools > 0:
		label = model.T("Working…")
	case thoughts > 0 && last > first:
		label = fmt.Sprintf(model.T("Thought for %s"), formatDuration(last-first))
	case tools > 0 && last > first:
		label = fmt.Sprintf(model.T("Worked for %s"), formatDuration(last-first))
	case tools > 0:
		label = model.activityToolNames(group)
	case messages > 0:
		label = model.T("Earlier messages")
	default:
		label = model.T("Activity")
	}
	if tools > 0 {
		count := model.T("%d tools")
		if tools == 1 {
			count = model.T("%d tool")
		}
		label += " · " + fmt.Sprintf(count, tools)
	}
	return label, running, failed
}

// activityToolNames names the first tools of a tool-only group so a collapsed
// row still says what ran.
func (model *Model) activityToolNames(group *activityGroup) string {
	names := make([]string, 0, 2)
	for _, item := range group.items {
		if item.Kind != projection.KindTool {
			continue
		}
		name := strings.TrimPrefix(model.toolTitle(item.Title), model.T("Tool · "))
		names = append(names, name)
		if len(names) == 2 {
			break
		}
	}
	if len(names) == 0 {
		return model.T("Activity")
	}
	return strings.Join(names, ", ")
}

func formatDuration(milliseconds int64) string {
	seconds := (milliseconds + 500) / 1000
	if seconds < 1 {
		seconds = 1
	}
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes, rest := seconds/60, seconds%60
	if rest == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dm %ds", minutes, rest)
}
