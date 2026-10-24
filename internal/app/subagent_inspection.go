package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/egoist/mygo/ui"
	"strings"
	"time"
	"unicode/utf8"

	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/view"
)

const maxSubagentPageBytes = 256 * 1024

type subagentReadState struct {
	operation uint64
	cancel    context.CancelFunc
}

type subagentHistoryPage struct {
	Start, End, Next, Inherited uint64
	HasMore                     bool
	EndReason                   string
	Items                       []projection.Item
}

func decodeSubagentCatalog(raw json.RawMessage, parent string) ([]view.SubagentSummary, error) {
	invalid := errors.New("Invalid or oversized subagent catalog")
	var result struct {
		Parent   string            `json:"parentSessionId"`
		Children []json.RawMessage `json:"children"`
	}
	if len(raw) > maxSubagentPageBytes || !utf8.Valid(raw) || json.Unmarshal(raw, &result) != nil || result.Parent != parent || result.Children == nil || len(result.Children) > 128 {
		return nil, invalid
	}
	children := make([]view.SubagentSummary, 0, len(result.Children))
	seen := make(map[string]bool)
	for _, data := range result.Children {
		var child struct {
			ID        string  `json:"id"`
			Label     *string `json:"label"`
			Mode      string  `json:"mode"`
			Status    string  `json:"status"`
			CreatedAt *int64  `json:"createdAt"`
		}
		if json.Unmarshal(data, &child) != nil || !safeSubagentID(child.ID) || seen[child.ID] || child.Label == nil || len(*child.Label) > 1024 || strings.IndexFunc(*child.Label, func(r rune) bool { return r < 32 || r == 127 }) >= 0 || child.CreatedAt == nil || *child.CreatedAt < 0 || *child.CreatedAt > maxSafeInteger {
			return nil, invalid
		}
		if child.Mode != "one-shot" && child.Mode != "continuable" && child.Mode != "external" && child.Mode != "unknown" {
			return nil, invalid
		}
		if child.Status != "stored" && child.Status != "idle" && child.Status != "running" && child.Status != "external" || (child.Mode == "external") != (child.Status == "external") {
			return nil, invalid
		}
		seen[child.ID] = true
		children = append(children, view.SubagentSummary{ID: child.ID, Label: *child.Label, Mode: child.Mode, Status: child.Status, CreatedAt: *child.CreatedAt})
	}
	return children, nil
}

func decodeSubagentHistory(raw json.RawMessage, parent, child string, before *uint64) (subagentHistoryPage, error) {
	invalid := errors.New("Invalid or oversized subagent history")
	var page struct {
		Parent    string            `json:"parentSessionId"`
		Child     string            `json:"childSessionId"`
		Start     *uint64           `json:"startSeq"`
		End       *uint64           `json:"endSeq"`
		Next      *uint64           `json:"nextSeq"`
		Inherited *uint64           `json:"inheritedEventCount"`
		More      *bool             `json:"hasMore"`
		Events    []json.RawMessage `json:"events"`
	}
	if len(raw) > maxSubagentPageBytes || !utf8.Valid(raw) || json.Unmarshal(raw, &page) != nil || page.Parent != parent || page.Child != child || page.Start == nil || page.End == nil || page.Next == nil || page.Inherited == nil || page.More == nil || page.Events == nil || len(page.Events) > 64 {
		return subagentHistoryPage{}, invalid
	}
	if *page.Next > maxSafeInteger || *page.Inherited > *page.Start || *page.Start > *page.End || *page.End > *page.Next || *page.End-*page.Start != uint64(len(page.Events)) || *page.More != (*page.Start > *page.Inherited) || *page.End > *page.Inherited && len(page.Events) == 0 {
		return subagentHistoryPage{}, invalid
	}
	if before == nil && *page.End != *page.Next || before != nil && *page.End != *before {
		return subagentHistoryPage{}, invalid
	}
	result := subagentHistoryPage{Start: *page.Start, End: *page.End, Next: *page.Next, Inherited: *page.Inherited, HasMore: *page.More}
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
	a.inspectionRead.operation++
	if a.inspectionRead.cancel != nil {
		a.inspectionRead.cancel()
		a.inspectionRead.cancel = nil
	}
	a.model.Inspection.Busy = false
}

func (a *App) OpenSubagents() {
	if a.options.SDKProtocol || a.shuttingDown.Load() || a.currentClient() == nil || !a.model.CanInspectSubagents() {
		return
	}
	a.CloseSubagents()
	a.model.Inspection.Open = true
	a.model.Inspection.ParentID = a.model.SessionID
	a.RefreshSubagents()
}

func (a *App) CloseSubagents() {
	a.cancelSubagentRead()
	p := &a.model.Inspection
	p.Open, p.Selected, p.Content, p.Children, p.Error = false, "", nil, nil, ""
	p.List = ui.ListState{}
	p.EndReason, p.Earlier = "", false
}

func (a *App) SelectSubagent(id string) {
	p := &a.model.Inspection
	if !p.Open || p.Busy {
		return
	}
	if id == "" {
		p.Selected, p.Content, p.EndReason = "", nil, ""
		a.RefreshSubagents()
		return
	}
	for _, entry := range p.Children {
		if entry.ID == id {
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
	before := p.Start
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	a.inspectionRead.cancel = cancel
	operation, generation, parent, child := a.inspectionRead.operation, a.hostGeneration.Load(), p.ParentID, p.Selected
	p.Busy, p.Error = true, ""
	go func() {
		defer cancel()
		var children []view.SubagentSummary
		var history subagentHistoryPage
		var err error
		if child == "" {
			var raw json.RawMessage
			raw, err = client.Call(ctx, "subagent/list", map[string]any{"parentSessionId": parent})
			if err == nil {
				children, err = decodeSubagentCatalog(raw, parent)
			}
		} else {
			params := map[string]any{"parentSessionId": parent, "childSessionId": child}
			if before != nil {
				params["beforeSeq"] = *before
			}
			var raw json.RawMessage
			raw, err = client.Call(ctx, "subagent/history", params)
			if err == nil {
				history, err = decodeSubagentHistory(raw, parent, child, before)
			}
		}
		a.post(func() {
			p := &a.model.Inspection
			if a.shuttingDown.Load() || operation != a.inspectionRead.operation || generation != a.hostGeneration.Load() || !p.Open || parent != a.model.SessionID || child != p.Selected {
				return
			}
			a.cancelSubagentRead()
			if err != nil {
				p.Error = "Could not load subagents. Refresh to retry"
				return
			}
			if child == "" {
				p.Children = children
				return
			}
			if before != nil && p.Inherited != history.Inherited {
				p.Error = "Could not load subagents. Refresh to retry"
				return
			}
			content := view.NewModel()
			content.SessionID = child
			content.ResetTranscript(history.Items)
			p.Content, p.Start, p.Inherited, p.HasMore, p.Earlier = content, history.Start, history.Inherited, history.HasMore, before != nil
			if before == nil {
				p.EndReason = history.EndReason
			}
		})
	}()
}
