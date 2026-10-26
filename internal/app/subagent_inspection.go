package app

import (
	"encoding/json"
	"errors"
	"github.com/egoist/mygo/ui"
	"strings"
	"unicode/utf8"

	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

const maxSubagentPageBytes = 256 * 1024

type subagentHistoryPage struct {
	Start, End, Next, Inherited uint64
	HasMore                     bool
	EndReason                   string
	Items                       []projection.Item
}

func decodeSubagentCatalog(raw json.RawMessage, parent string) ([]view.SubagentSummary, error) {
	invalid := errors.New("Invalid or oversized subagent catalog")
	var result protocol.SubagentListResult
	if len(raw) > maxSubagentPageBytes || !utf8.Valid(raw) || json.Unmarshal(raw, &result) != nil || result.ParentSessionID != parent || result.Children == nil || len(result.Children) > 128 {
		return nil, invalid
	}
	children := make([]view.SubagentSummary, 0, len(result.Children))
	seen := make(map[string]bool)
	for _, child := range result.Children {
		if !safeSubagentID(child.ID) || seen[child.ID] || child.Label == nil || len(*child.Label) > 1024 || strings.IndexFunc(*child.Label, func(r rune) bool { return r < 32 || r == 127 }) >= 0 || child.CreatedAt == nil || *child.CreatedAt < 0 || *child.CreatedAt > maxSafeInteger {
			return nil, invalid
		}
		if child.Mode != protocol.SubagentModeOneShot && child.Mode != protocol.SubagentModeContinuable && child.Mode != protocol.SubagentModeExternal && child.Mode != protocol.SubagentModeUnknown {
			return nil, invalid
		}
		if child.Status != protocol.SubagentStatusStored && child.Status != protocol.SubagentStatusIdle && child.Status != protocol.SubagentStatusRunning && child.Status != protocol.SubagentStatusExternal || (child.Mode == protocol.SubagentModeExternal) != (child.Status == protocol.SubagentStatusExternal) {
			return nil, invalid
		}
		seen[child.ID] = true
		children = append(children, view.SubagentSummary{ID: child.ID, Label: *child.Label, Mode: child.Mode, Status: child.Status, CreatedAt: *child.CreatedAt})
	}
	return children, nil
}

func decodeSubagentHistory(raw json.RawMessage, parent, child string, before *uint64) (subagentHistoryPage, error) {
	invalid := errors.New("Invalid or oversized subagent history")
	var page protocol.SubagentHistoryResult
	if len(raw) > maxSubagentPageBytes || !utf8.Valid(raw) || json.Unmarshal(raw, &page) != nil || page.ParentSessionID != parent || page.ChildSessionID != child || page.StartSeq == nil || page.EndSeq == nil || page.NextSeq == nil || page.InheritedEventCount == nil || page.HasMore == nil || page.Events == nil || len(page.Events) > 64 {
		return subagentHistoryPage{}, invalid
	}
	if *page.NextSeq > maxSafeInteger || *page.InheritedEventCount > *page.StartSeq || *page.StartSeq > *page.EndSeq || *page.EndSeq > *page.NextSeq || *page.EndSeq-*page.StartSeq != uint64(len(page.Events)) || *page.HasMore != (*page.StartSeq > *page.InheritedEventCount) || *page.EndSeq > *page.InheritedEventCount && len(page.Events) == 0 {
		return subagentHistoryPage{}, invalid
	}
	if before == nil && *page.EndSeq != *page.NextSeq || before != nil && *page.EndSeq != *before {
		return subagentHistoryPage{}, invalid
	}
	result := subagentHistoryPage{Start: *page.StartSeq, End: *page.EndSeq, Next: *page.NextSeq, Inherited: *page.InheritedEventCount, HasMore: *page.HasMore}
	var transcript projection.Transcript
	for index, data := range page.Events {
		var event projection.Event
		var required struct {
			Seq  *uint64 `json:"seq"`
			Time *int64  `json:"time"`
		}
		if json.Unmarshal(data, &event) != nil || json.Unmarshal(data, &required) != nil || required.Seq == nil || required.Time == nil || event.Seq != result.Start+uint64(index) || event.Time < 0 || event.Time > maxSafeInteger || len(event.Data) == 0 || event.Data[0] != '{' {
			return subagentHistoryPage{}, invalid
		}
		switch event.Type {
		case "assistant/message", "user/message", "tool/call", "tool/result", "mygo/hidden":
		case "turn/start":
			result.EndReason = ""
		case "turn/end":
			var end struct {
				Reason struct {
					Kind string `json:"kind"`
				} `json:"reason"`
			}
			if json.Unmarshal(event.Data, &end) != nil || !subagentReasons[end.Reason.Kind] {
				return subagentHistoryPage{}, invalid
			}
			result.EndReason = end.Reason.Kind
		default:
			return subagentHistoryPage{}, invalid
		}
		transcript.Apply(event)
	}
	result.Items = transcript.Items()
	return result, nil
}

func (a *App) cancelSubagentRead() {
	a.subagents.finishRead()
	a.model.Inspection.Busy = false
}

func (a *App) OpenSubagents() {
	if a.options.SDKProtocol || a.shuttingDown.Load() || a.currentClient() == nil || !a.model.CanInspectSubagents() {
		return
	}
	a.CloseSubagents()
	a.subagents.openFor(a.model.SessionID)
	a.model.Inspection.Open = true
	a.model.Inspection.ParentID = a.model.SessionID
	a.RefreshSubagents()
}

func (a *App) CloseSubagents() {
	a.cancelSubagentRead()
	a.subagents.close()
	p := &a.model.Inspection
	p.Open, p.Selected, p.Content, p.Children, p.Error = false, "", nil, nil, ""
	p.List = ui.ListState{}
	p.EndReason, p.Earlier, p.HasMore = "", false, false
}

func (a *App) SelectSubagent(id string) {
	p := &a.model.Inspection
	if !p.Open || p.Busy {
		return
	}
	if id == "" {
		a.subagents.selectChild("")
		p.Selected, p.Content, p.EndReason, p.Earlier = "", nil, "", false
		a.RefreshSubagents()
		return
	}
	for _, entry := range p.Children {
		if entry.ID == id {
			a.subagents.selectChild(id)
			p.Selected, p.Content, p.EndReason, p.Error, p.Earlier = id, nil, "", "", false
			if entry.Mode != "external" && entry.Mode != "unknown" {
				a.RefreshSubagents()
			}
			return
		}
	}
}

func (a *App) RefreshSubagents() { a.readSubagents(nil) }

func (a *App) EarlierSubagentHistory() {
	p := &a.model.Inspection
	if !p.HasMore || p.Content == nil {
		return
	}
	before, ok := a.subagents.before()
	if !ok {
		return
	}
	a.readSubagents(&before)
}

func (a *App) readSubagents(before *uint64) {
	p := &a.model.Inspection
	if a.shuttingDown.Load() || !p.Available || !p.Open || !p.HasLocalHistory() || p.Busy || p.ParentID != a.model.SessionID || a.model.Phase != view.PhaseIdle && a.model.Phase != view.PhaseRunning {
		return
	}
	client := a.currentClient()
	if client == nil {
		return
	}
	a.cancelSubagentRead()
	generation := a.hostGeneration.Load()
	ctx, read, cancel, ok := a.subagents.beginRead(generation, before != nil)
	if !ok {
		return
	}
	p.Busy, p.Error = true, ""
	go func() {
		defer cancel()
		var children []view.SubagentSummary
		var history subagentHistoryPage
		var err error
		if read.child == "" {
			var raw json.RawMessage
			raw, err = client.Call(ctx, protocol.MethodSubagentList, protocol.SubagentListParams{ParentSessionID: read.parent})
			if err == nil {
				children, err = decodeSubagentCatalog(raw, read.parent)
			}
		} else {
			params := protocol.SubagentHistoryParams{ParentSessionID: read.parent, ChildSessionID: read.child, BeforeSeq: before}
			var raw json.RawMessage
			raw, err = client.Call(ctx, protocol.MethodSubagentHistory, params)
			if err == nil {
				history, err = decodeSubagentHistory(raw, read.parent, read.child, before)
			}
		}
		a.post(func() {
			p := &a.model.Inspection
			if a.shuttingDown.Load() || !a.subagents.acceptRead(read, a.hostGeneration.Load()) {
				return
			}
			a.cancelSubagentRead()
			if err != nil {
				p.Error = "Could not load subagents. Refresh to retry"
				return
			}
			if read.child == "" {
				p.Children = children
				return
			}
			page, ok := a.subagents.commitPage(read, history)
			if !ok {
				p.Error = "Could not load subagents. Refresh to retry"
				return
			}
			content := view.NewModel()
			content.SessionID = read.child
			content.ResetTranscript(history.Items)
			p.Content, p.Start, p.Inherited, p.HasMore, p.Earlier, p.EndReason = content, page.start, page.inherited, page.hasMore, page.earlier, page.endReason
		})
	}()
}
