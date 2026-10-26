package app

import (
	"context"
	"encoding/json"
	"errors"

	"dsh-mygo/internal/channel"
	"dsh-mygo/internal/interaction"
	"dsh-mygo/internal/protocol"
	"dsh-mygo/internal/view"
)

func (a *App) publishApprovals() {
	a.model.Approvals = a.approvals.Items()
	a.model.ApprovalOpen = len(a.model.Approvals) > 0
}

func (a *App) clearApprovals() {
	a.approvals.Clear()
	a.publishApprovals()
}

func (a *App) handleApprovalNotification(method string, params json.RawMessage) {
	if method == protocol.NotificationApprovalClosed {
		requestID, sessionID, err := interaction.DecodeApprovalClosed(params)
		if err != nil {
			a.setFailure("Invalid approval notification", err)
			go a.stopHost()
			return
		}
		a.approvals.Remove(requestID, sessionID)
		a.publishApprovals()
		return
	}
	request, err := interaction.DecodeApproval(params)
	if err != nil {
		a.setFailure("Invalid approval request", err)
		go a.stopHost()
		return
	}
	if request.SessionID != a.sessionID {
		return
	}
	if !a.model.CanApprove {
		a.setFailure("Unexpected approval request", errors.New("Approval capability was not negotiated"))
		go a.stopHost()
		return
	}
	if err := a.approvals.Add(request); err != nil {
		a.setFailure("Invalid approval queue", err)
		go a.stopHost()
		return
	}
	a.publishApprovals()
}

func (a *App) RespondApproval(requestID, decision string) {
	if a.shuttingDown.Load() || a.options.SDKProtocol || !a.model.CanApprove || a.model.CancelPending || a.model.Phase != view.PhaseRunning || decision != protocol.ApprovalDecisionAllow && decision != protocol.ApprovalDecisionDeny {
		return
	}
	client := a.currentClient()
	if client == nil || !a.approvals.Begin(requestID) {
		return
	}
	a.publishApprovals()
	sessionID := a.sessionID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cancelTimeout)
		defer cancel()
		result, err := client.Call(ctx, protocol.MethodApprovalRespond, protocol.ApprovalRespondParams{RequestID: requestID, Decision: decision})
		a.post(func() {
			if a.shuttingDown.Load() || a.model.Phase == view.PhaseFailed {
				return
			}
			var rpcError *channel.RPCError
			stale := errors.As(err, &rpcError) && protocol.ErrorKind(rpcError.Data) == protocol.ErrorKindApprovalStaleRequest
			var acknowledged map[string]json.RawMessage
			if err == nil && (json.Unmarshal(result, &acknowledged) != nil || acknowledged == nil) {
				err = errors.New("invalid approval response")
			}
			if err == nil || stale {
				a.approvals.Remove(requestID, sessionID)
			} else {
				a.approvals.Fail(requestID, "Could not send the decision. Retry or stop the turn.")
			}
			a.publishApprovals()
		})
	}()
}
