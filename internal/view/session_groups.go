package view

import (
	"fmt"
	"time"
)

type sessionGroup int

const (
	groupToday sessionGroup = iota
	groupYesterday
	groupPrevious7
	groupOlder
)

func (group sessionGroup) labelKey() string {
	switch group {
	case groupYesterday:
		return "Yesterday"
	case groupPrevious7:
		return "Previous 7 days"
	case groupOlder:
		return "Older"
	default:
		return "Today"
	}
}

func (group sessionGroup) headerKey() string {
	return fmt.Sprintf("header:%d", int(group))
}

// sessionListEntry is one row of the grouped session list: either a sticky
// date header or a Host-ordered session summary.
type sessionListEntry struct {
	Header  bool
	Group   sessionGroup
	Session SessionSummary
	Key     string
}

func sessionGroupFor(now, then time.Time) sessionGroup {
	if then.IsZero() {
		return groupOlder
	}
	year, month, day := now.Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	year, month, day = then.Date()
	that := time.Date(year, month, day, 0, 0, 0, 0, then.Location())
	days := int(today.Sub(that).Hours() / 24)
	switch {
	case days <= 0:
		return groupToday
	case days == 1:
		return groupYesterday
	case days <= 7:
		return groupPrevious7
	default:
		return groupOlder
	}
}

func relativeTimeLabel(model *Model, now time.Time, millis int64) string {
	if millis <= 0 {
		return ""
	}
	then := time.UnixMilli(millis)
	diff := now.Sub(then)
	switch {
	case diff < time.Minute:
		return model.T("Just now")
	case diff < time.Hour:
		return fmt.Sprintf("%dm", int(diff/time.Minute))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh", int(diff/time.Hour))
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(diff/(24*time.Hour)))
	default:
		return then.Format("01-02")
	}
}

// sessionListEntries groups the filtered sessions by local calendar day while
// preserving the Host's ordering inside each group.
func (model *Model) sessionListEntries(now time.Time) []sessionListEntry {
	sessions := model.FilteredSessions()
	entries := make([]sessionListEntry, 0, len(sessions)+4)
	previous := sessionGroup(-1)
	for _, session := range sessions {
		updated := session.UpdatedAt
		if updated == 0 {
			updated = session.CreatedAt
		}
		group := groupOlder
		if updated > 0 {
			group = sessionGroupFor(now, time.UnixMilli(updated))
		}
		if group != previous {
			entries = append(entries, sessionListEntry{Header: true, Group: group, Key: group.headerKey()})
			previous = group
		}
		entries = append(entries, sessionListEntry{Group: group, Session: session, Key: "session:" + session.ID})
	}
	return entries
}
