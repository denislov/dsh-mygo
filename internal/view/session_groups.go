package view

import (
	"fmt"
	"path"
	"runtime"
	"strings"
	"time"
)

// sessionListEntry is one row of the grouped session list: either a sticky
// project header or a Host-ordered session summary.
type sessionListEntry struct {
	Header  bool
	Session SessionSummary
	Key     string
	Project string
}

func projectKey(cwd string) string {
	if cwd == "" {
		return ""
	}
	key := path.Clean(strings.ReplaceAll(cwd, `\`, "/"))
	if runtime.GOOS == "windows" || len(key) > 1 && key[1] == ':' || strings.HasPrefix(key, "//") {
		key = strings.ToLower(key)
	}
	return key
}

func projectName(cwd string) string {
	clean := strings.TrimRight(strings.ReplaceAll(cwd, `\`, "/"), "/")
	if clean == "" {
		return cwd
	}
	return path.Base(clean)
}

// Project order follows first occurrence, and each project's sessions retain
// their Host order. Full paths are the identity even when names are equal.
func (model *Model) projectSessionEntries() []sessionListEntry {
	groups := make(map[string][]SessionSummary)
	var order []string
	for _, session := range model.FilteredSessions() {
		key := projectKey(session.CWD)
		if _, found := groups[key]; !found {
			order = append(order, key)
		}
		groups[key] = append(groups[key], session)
	}
	var entries []sessionListEntry
	for _, key := range order {
		entries = append(entries, sessionListEntry{Header: true, Key: "project:" + key, Project: groups[key][0].CWD})
		for _, session := range groups[key] {
			entries = append(entries, sessionListEntry{Key: "session:" + session.ID, Session: session})
		}
	}
	return entries
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
