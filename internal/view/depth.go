package view

import (
	"fmt"
	"strings"

	mgo "github.com/egoist/mygo/ui"

	"dsh-mygo/internal/projection"
)

// depthLabel summarizes plan mode and the current goal for the header. It stays
// empty when neither is present, so routes without those plugins stay quiet.
func depthLabel(model *Model) string {
	parts := make([]string, 0, 2)
	if model.PlanActive {
		parts = append(parts, model.T("Plan mode"))
	}
	if model.Goal != nil {
		goal := fmt.Sprintf("%s: %s · %s", model.T("Goal"), model.Goal.Objective, model.T(goalPhaseLabel(model.Goal.Phase)))
		if model.Goal.MaxRounds > 0 {
			goal += fmt.Sprintf(" · %d/%d", model.Goal.RoundsStarted, model.Goal.MaxRounds)
		}
		parts = append(parts, goal)
	}
	return strings.Join(parts, " · ")
}

func goalPhaseLabel(phase string) string {
	switch phase {
	case "active", "paused", "blocked", "complete":
		return phase
	default:
		return "active"
	}
}

// taskPanel shows the standing checklist above the composer. It is bounded to
// a handful of rows so a hostile snapshot cannot push the composer off-screen.
func taskPanel(c *mgo.Context, model *Model) {
	if len(model.Tasks) == 0 {
		return
	}
	tokens := tokensFor(c.Theme())
	done, total := taskProgress(model.Tasks)
	const maxVisible = 8
	visible := model.Tasks
	if len(visible) > maxVisible {
		visible = visible[:maxVisible]
	}
	label := fmt.Sprintf(model.T("Tasks %d/%d"), done, total)
	mgo.Row(c.Key("tasks-zone")).FillWidth().Justify(mgo.Center).Padding(0, space24, space4, space24).Children(func() {
		card := mgo.Column(c.Key("tasks-panel")).FillWidth().MaxWidth(readingWidth).
			Padding(space6, space12).Radius(radiusLg).Background(tokens.SurfaceRaised).Border(1, tokens.Border)
		card = shadowCard.apply(card)
		card.Children(func() {
			disclosure := mgo.CollapsibleBase(c, &model.Shell.TasksExpanded)
			disclosure.Trigger.FillWidth().Gap(space8).AlignItems(mgo.Center).Label(label)
			disclosure.Trigger.Children(func() {
				mgo.Text(c, label).FontSize(textCaption).Bold().SingleLine()
				ratio := 0.0
				if total > 0 {
					ratio = float64(done) / float64(total)
				}
				mgo.Progress(c.Key("task-progress"), ratio).Grow(1).Label(label)
				mgo.Icon(c, iconChevronRight).Size(iconSmall, iconSmall).TextColor(tokens.TextMuted).Rotate(90 * disclosure.Progress())
			})
			disclosure.Panel(func() {
				_, height := c.Size()
				mgo.Scroll(c.Key("task-scroll")).FillWidth().
					MaxHeight(max(54, min(150, height-titleBarHeight(c)-330))).Children(func() {
					mgo.Column(c).FillWidth().Gap(space6).Padding(space4, 0).Children(func() {
						for _, task := range visible {
							prefix, color := "○", tokens.TextMuted
							switch task.Status {
							case "completed":
								prefix = "✓"
							case "in_progress":
								prefix, color = "▶", tokens.Accent
							}
							mgo.Text(c, prefix+" "+task.Content).FontSize(textCaption).TextColor(color).SingleLine()
						}
						if len(model.Tasks) > maxVisible {
							mgo.Text(c, fmt.Sprintf(model.T("+%d more"), len(model.Tasks)-maxVisible)).FontSize(textMicro).TextColor(tokens.TextMuted)
						}
					})
				})
			})
		})
	})
}

// taskProgress counts completed rows for the checklist header.
func taskProgress(tasks []projection.Task) (done, total int) {
	for _, task := range tasks {
		if task.Status == "completed" {
			done++
		}
	}
	return done, len(tasks)
}
