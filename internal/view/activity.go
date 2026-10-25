package view

import (
	"fmt"
	"strings"

	mgo "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"dsh-mygo/internal/projection"
)

// transcriptRow is one row of the transcript list: either a plain item or an
// activity group that folds consecutive reasoning and tool rows together.
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

// buildTranscriptRows folds adjacent reasoning and tool rows into groups. A
// group never crosses a turn boundary or a visible assistant/user row.
func buildTranscriptRows(items []projection.Item) []transcriptRow {
	rows := make([]transcriptRow, 0, len(items))
	used := make(map[string]int)
	for index := 0; index < len(items); {
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
		group := &activityGroup{items: items[index:end]}
		group.key = activityGroupKey(group)
		if used[group.key] > 0 {
			group.key += ":" + group.items[0].ID
		}
		used[group.key]++
		rows = append(rows, transcriptRow{key: group.key, activity: group})
		index = end
	}
	return rows
}

func isActivityItem(item projection.Item) bool {
	return item.Kind == projection.KindReasoning || item.Kind == projection.KindTool
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
	card := surfaceCard(c, "activity-card-"+group.key).FillWidth().MaxWidth(readingWidth).
		Padding(space6, space12).Gap(space2).Selectable()
	card.Children(func() {
		disclosure := mgo.CollapsibleBase(c, &state.Expanded)
		disclosure.Trigger.FillWidth().Gap(space8).AlignItems(mgo.Center).Padding(space2, 0).Label(summary)
		disclosure.Trigger.Children(func() {
			mgo.Icon(c, iconActivity).Size(14, 14).TextColor(tokens.TextMuted)
			mgo.Text(c, summary).FontSize(textCaption).TextColor(tokens.TextMuted).SingleLine().Grow(1)
			if running {
				mgo.Spinner(c.Key("activity-spinner-"+group.key)).Size(10, 10)
			}
			if failed {
				statusPill(c, "activity-state-"+group.key, model.T("Failed"), tokens.Danger, tokens.DangerSoft)
			}
			mgo.Icon(c, iconChevronRight).Size(12, 12).TextColor(tokens.TextMuted).
				Rotate(90 * disclosure.Progress())
		})
		disclosure.Panel(func() {
			mgo.Column(c.Key("activity-body-"+group.key)).Gap(space8).Padding(space8, 0, space2, 0).Children(func() {
				for _, item := range group.items {
					switch item.Kind {
					case projection.KindReasoning:
						reasoningBlock(c, model, item)
					case projection.KindTool:
						toolCard(c, model, item)
					}
				}
			})
		})
	})
}

func reasoningBlock(c *mgo.Context, model *Model, item projection.Item) {
	tokens := tokensFor(c.Theme())
	mgo.Column(c.Key("reasoning-"+item.ID)).FillWidth().Gap(space4).Children(func() {
		if item.State == "streaming" {
			markdown.Stream(c.Key("reasoning-markdown-"+item.ID), item.Text, &model.ReasoningMarkdown).FillWidth()
		} else {
			markdown.View(c.Key("reasoning-markdown-"+item.ID), item.Text, &model.ReasoningMarkdown).FillWidth()
		}
		if item.Truncated {
			mgo.Text(c, model.T("Truncated message: only the visible prefix is available.")).
				FontSize(textMicro).TextColor(tokens.Warning)
		}
	})
}

// activitySummary describes the group in one line: live status first, then a
// duration and tool count. It also reports whether work is still running and
// whether any tool failed.
func (model *Model) activitySummary(group *activityGroup) (string, bool, bool) {
	thoughts, tools := 0, 0
	running, failed, interrupted := false, false, false
	var first, last int64
	for _, item := range group.items {
		switch item.Kind {
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
