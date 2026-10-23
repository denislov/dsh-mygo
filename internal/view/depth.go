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
	theme := c.Theme()
	done, total := taskProgress(model.Tasks)
	const maxVisible = 8
	visible := model.Tasks
	if len(visible) > maxVisible {
		visible = visible[:maxVisible]
	}
	mgo.Column(c.Key("tasks-panel")).Margin(8, 20, 0, 20).Padding(8, 12).Gap(2).Radius(8).
		Background(theme.Surface).Border(1, theme.Border).Children(func() {
		mgo.Text(c, fmt.Sprintf(model.T("Tasks %d/%d"), done, total)).FontSize(11).Bold()
		for _, task := range visible {
			prefix, color := "○", theme.TextMuted
			switch task.Status {
			case "completed":
				prefix = "✓"
			case "in_progress":
				prefix, color = "▶", theme.Text
			}
			mgo.Text(c, prefix+" "+task.Content).FontSize(11).TextColor(color).SingleLine()
		}
		if len(model.Tasks) > maxVisible {
			mgo.Text(c, fmt.Sprintf(model.T("+%d more"), len(model.Tasks)-maxVisible)).FontSize(11).TextColor(theme.TextMuted)
		}
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
