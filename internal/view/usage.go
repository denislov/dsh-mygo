package view

import (
	"fmt"
	"strings"

	mgo "github.com/egoist/mygo/ui"
)

// maxReportedTokens bounds the displayed context sum; larger sums are clamped
// rather than overflowing or rendering a misleading percentage.
const maxReportedTokens = 1 << 40

const (
	contextWarningRatio = 0.75
	contextDangerRatio  = 0.90
)

// contextUsage is the newest step's token accounting against the route
// capacity. UsageView describes one model call, so the panel labels the sum
// as the latest turn instead of implying a whole-session total.
type contextUsage struct {
	used     int
	window   int
	ratio    float64
	hasUsage bool
}

func readContextUsage(model *Model) contextUsage {
	usage := contextUsage{window: model.Context.Window, hasUsage: model.Usage.HasUsage}
	if !usage.hasUsage {
		return usage
	}
	usage.used = model.Usage.InputTokens + model.Usage.OutputTokens
	if usage.used < 0 {
		usage.used = 0
	}
	if usage.used > maxReportedTokens {
		usage.used = maxReportedTokens
	}
	if usage.window > 0 {
		usage.ratio = float64(usage.used) / float64(usage.window)
		if usage.ratio > 1 {
			usage.ratio = 1
		}
	}
	return usage
}

func contextUsageAvailable(model *Model) bool {
	return model.Context.Window > 0 || model.Usage.HasUsage
}

func (u contextUsage) percent() int {
	if u.window <= 0 || !u.hasUsage {
		return 0
	}
	return min(100, int(float64(u.used)*100/float64(u.window)))
}

// severity colors the chip ring and percentage before the window fills.
func (u contextUsage) severity(tokens appTokens) mgo.Color {
	if !u.hasUsage {
		return tokens.TextFaint
	}
	switch {
	case u.ratio >= contextDangerRatio:
		return tokens.Danger
	case u.ratio >= contextWarningRatio:
		return tokens.Warning
	default:
		return tokens.Accent
	}
}

func contextUsageChipLabel(usage contextUsage) string {
	if usage.window > 0 {
		if !usage.hasUsage {
			return formatTokens(usage.window)
		}
		return fmt.Sprintf("%d%% · %s", usage.percent(), formatTokens(usage.window))
	}
	return formatTokens(usage.used)
}

// contextUsageChip is the composer's compact entry to the usage panel. The
// ring carries the warning color as the window fills; the text stays quiet.
func contextUsageChip(c *mgo.Context, model *Model) mgo.Element {
	tokens := tokensFor(c.Theme())
	usage := readContextUsage(model)
	label := contextUsageChipLabel(usage)
	button := mgo.Button(c.Key("composer-context"), "").Label(model.T("Context usage")+" · "+label).
		Padding(space4, space8).Gap(space6).Radius(radiusPill).
		Background(tokens.Surface).Border(1, tokens.Border).Transition(quietTransition)
	if button.Hovered() || model.ContextUsageOpen {
		button.Background(tokens.SurfaceHover).Border(1, tokens.BorderStrong)
	}
	button.OnClick(func() { model.ContextUsageOpen = !model.ContextUsageOpen })
	button.Children(func() {
		mgo.Box(c).Size(8, 8).Radius(4).Border(2, usage.severity(tokens))
		mgo.Text(c, label).FontSize(textMicro).TextColor(tokens.TextMuted).SingleLine()
	})
	return button
}

// contextUsagePopover opens above the composer's chip when it has no room
// below. Modal and queue surfaces close it so the panel never floats over a
// decision the user must answer.
func contextUsagePopover(c *mgo.Context, model *Model, anchor mgo.Element) {
	if model.SettingsOpen || model.RecoveryOpen || model.SessionsOpen || model.SessionSwitching ||
		len(model.Approvals) > 0 || len(model.Questions) > 0 {
		model.ContextUsageOpen = false
		return
	}
	if !model.ContextUsageOpen {
		return
	}
	width, _ := c.Size()
	panelWidth := min(320, width-48)
	panel := mgo.Popover(c.Key("context-usage-popover"), anchor, &model.ContextUsageOpen, func() {
		mgo.Column(c).Width(panelWidth).Gap(space10).Padding(space12).Children(func() {
			contextUsagePanel(c, model, "popover")
		})
	})
	if panel.Dismissed() {
		model.ContextUsageOpen = false
	}
}

// contextUsagePanel renders the title, usage bar, breakdown rows and the
// note that scope the numbers. The scope keeps element keys unique when the
// details rail and the composer popover show the panel together.
func contextUsagePanel(c *mgo.Context, model *Model, scope string) {
	tokens := tokensFor(c.Theme())
	usage := readContextUsage(model)
	if !usage.hasUsage && usage.window <= 0 {
		return
	}
	title := model.T("Context window")
	if usage.hasUsage {
		title = model.T("Context used")
	}
	mgo.Row(c.Key("context-usage-header-" + scope)).FillWidth().Gap(space6).AlignItems(mgo.Center).Children(func() {
		mgo.Text(c, title).FontSize(textMicro).TextColor(tokens.TextMuted).SingleLine()
		if usage.hasUsage {
			mgo.Text(c, fmt.Sprintf("%d%%", usage.percent())).FontSize(textLabel).Bold().TextColor(usage.severity(tokens))
		}
		mgo.Spacer(c)
		mgo.Text(c, contextUsageAmount(usage)).FontSize(textMicro).TextColor(tokens.TextMuted).SingleLine()
	})
	if usage.window > 0 {
		contextUsageBar(c, model, usage, tokens, scope)
	}
	if usage.hasUsage {
		contextUsageRows(c, model, usage, tokens, scope)
	}
	switch {
	case !usage.hasUsage:
		mgo.Text(c, model.T("Waiting for the first turn's usage data")).FontSize(textMicro).TextColor(tokens.TextFaint)
	case usage.window <= 0:
		mgo.Text(c, model.T("The Host did not report a context window")).FontSize(textMicro).TextColor(tokens.TextFaint)
	default:
		mgo.Text(c, model.T("Latest turn only")).FontSize(textMicro).TextColor(tokens.TextFaint)
	}
}

func contextUsageAmount(usage contextUsage) string {
	switch {
	case usage.hasUsage && usage.window > 0:
		return fmt.Sprintf("%s / %s", formatTokens(usage.used), formatTokens(usage.window))
	case usage.hasUsage:
		return formatTokens(usage.used)
	default:
		return formatTokens(usage.window)
	}
}

func contextUsageBar(c *mgo.Context, model *Model, usage contextUsage, tokens appTokens, scope string) {
	inputShare, outputShare := float32(0), float32(0)
	if usage.hasUsage {
		inputShare = float32(model.Usage.InputTokens) / float32(usage.window)
		outputShare = float32(model.Usage.OutputTokens) / float32(usage.window)
		if total := inputShare + outputShare; total > 1 {
			inputShare, outputShare = inputShare/total, outputShare/total
		}
	}
	freeShare := max(float32(0), 1-inputShare-outputShare)
	mgo.Row(c.Key("context-usage-bar-" + scope)).FillWidth().Height(6).Radius(3).Clip().
		Background(tokens.Border).Children(func() {
		if inputShare > 0 {
			mgo.Box(c).Grow(inputShare).Height(6).Background(tokens.Accent)
		}
		if outputShare > 0 {
			mgo.Box(c).Grow(outputShare).Height(6).Background(tokens.Success)
		}
		if freeShare > 0 {
			mgo.Box(c).Grow(freeShare).Height(6).Background(tokens.Border)
		}
	})
}

func contextUsageRows(c *mgo.Context, model *Model, usage contextUsage, tokens appTokens, scope string) {
	mgo.Column(c.Key("context-usage-rows-" + scope)).FillWidth().Gap(space6).Children(func() {
		contextUsageRow(c, model.T("In"), formatTokens(model.Usage.InputTokens), tokens.Accent)
		contextUsageRow(c, model.T("Out"), formatTokens(model.Usage.OutputTokens), tokens.Success)
		if model.Usage.CacheReadTokens > 0 {
			contextUsageRow(c, model.T("Cache read"), formatTokens(model.Usage.CacheReadTokens), tokens.TextFaint)
		}
		if model.Usage.ReasoningTokens > 0 {
			contextUsageRow(c, model.T("Reasoning"), formatTokens(model.Usage.ReasoningTokens), tokens.TextMuted)
		}
		if usage.window > usage.used {
			contextUsageRow(c, model.T("Free"), formatTokens(usage.window-usage.used), tokens.BorderStrong)
		}
	})
}

func contextUsageRow(c *mgo.Context, label, value string, dot mgo.Color) {
	tokens := tokensFor(c.Theme())
	mgo.Row(c).FillWidth().Gap(space8).AlignItems(mgo.Center).Children(func() {
		mgo.Box(c).Size(6, 6).Radius(3).Background(dot)
		mgo.Text(c, label).FontSize(textMicro).TextColor(tokens.TextMuted).Grow(1)
		mgo.Text(c, value).FontSize(textMicro).TextColor(tokens.Text)
	})
}

// formatTokens shortens a token count for chips, bars and detail rows.
func formatTokens(count int) string {
	switch {
	case count < 1000:
		return fmt.Sprintf("%d", count)
	case count < 1_000_000:
		return trimDecimal(fmt.Sprintf("%.1f", float64(count)/1000)) + "k"
	default:
		return trimDecimal(fmt.Sprintf("%.1f", float64(count)/1_000_000)) + "M"
	}
}

func trimDecimal(value string) string {
	return strings.TrimSuffix(value, ".0")
}
