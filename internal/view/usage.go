package view

import (
	"fmt"
	"strings"
)

// usageContextLabel summarizes the newest step accounting and route capacity.
// It returns empty when the session has no metrics yet, so the header stays
// quiet for hosts and routes that report none.
func usageContextLabel(model *Model) string {
	usage, capacity := model.Usage, model.Context
	if !usage.HasUsage && capacity.Window <= 0 {
		return ""
	}
	parts := make([]string, 0, 4)
	if capacity.Window > 0 {
		if usage.HasUsage {
			used := usage.InputTokens + usage.OutputTokens
			if used > maxReportedTokens {
				used = maxReportedTokens
			}
			percent := 0
			if used > 0 {
				percent = used * 100 / capacity.Window
			}
			parts = append(parts, fmt.Sprintf("%s %s / %s · %d%%", model.T("Context"), formatTokens(used), formatTokens(capacity.Window), percent))
		} else {
			parts = append(parts, fmt.Sprintf("%s %s", model.T("Context"), formatTokens(capacity.Window)))
		}
	}
	if usage.HasUsage {
		parts = append(parts, fmt.Sprintf("%s %s", model.T("In"), formatTokens(usage.InputTokens)), fmt.Sprintf("%s %s", model.T("Out"), formatTokens(usage.OutputTokens)))
		if usage.CacheReadTokens > 0 {
			parts = append(parts, fmt.Sprintf("%s %s", model.T("cache"), formatTokens(usage.CacheReadTokens)))
		}
	}
	return strings.Join(parts, " · ")
}

// maxReportedTokens bounds the displayed context sum; larger sums are clamped
// rather than overflowing or rendering a misleading percentage.
const maxReportedTokens = 1 << 40

// formatTokens shortens a count for a one-line metrics row.
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
