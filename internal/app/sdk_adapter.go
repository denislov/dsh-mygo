package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"dsh-mygo/internal/protocol"
)

// sdkAdapter is the explicit legacy SDK boundary. It owns the dotted
// notification spelling, contentBlocks prompt shape and SDK initialize shape.
// Rich UI capabilities (stream, approvals, questions, history, settings,
// sessions, attachments and subagents) stay unavailable in this mode.
type sdkAdapter struct {
	options *Options
}

func newSDKAdapter(options *Options) sdkAdapter { return sdkAdapter{options: options} }

func (s sdkAdapter) enabled() bool { return s.options != nil && s.options.SDKProtocol }

// richUI reports whether the custom channel's interactive features apply.
func (s sdkAdapter) richUI() bool { return !s.enabled() }

// hostEnvelopes reports whether durable event/status envelopes must be
// sequence/identity checked. SDK notifications predate those envelopes.
func (s sdkAdapter) hostEnvelopes() bool { return !s.enabled() }

func (s sdkAdapter) initialize(ctx context.Context, client rpcClient) (initializedSession, error) {
	params := protocol.SDKInitializeParams{
		CWD: s.options.CWD, Provider: s.options.Provider, Model: s.options.Model,
		ReasoningEffort: s.options.ReasoningEffort,
	}
	result, err := client.Call(ctx, protocol.MethodInitialize, params)
	if err != nil {
		return initializedSession{}, err
	}
	var initialized protocol.SDKInitializeResult
	if json.Unmarshal(result, &initialized) != nil || initialized.ServerInfo.Name == "" {
		return initializedSession{}, errors.New("invalid SDK host handshake")
	}
	return initializedSession{
		sessionID: s.options.SessionID, cwd: s.options.CWD,
		detail: strings.TrimSpace(initialized.ServerInfo.Name + " " + initialized.ServerInfo.Version),
	}, nil
}

func (s sdkAdapter) promptRequest(sessionID string, blocks []protocol.ContentBlock) any {
	return protocol.SDKPromptParams{SessionID: sessionID, ContentBlocks: blocks}
}

// notificationMethod maps custom-channel names to the only two SDK events the
// legacy adapter consumes. Other custom notifications stay rejected.
func (s sdkAdapter) notificationMethod(method string) string {
	switch method {
	case protocol.NotificationSessionEvent:
		return protocol.NotificationSDKSessionEvent
	case protocol.NotificationSessionStatus:
		return protocol.NotificationSDKSessionStatus
	default:
		return method
	}
}

func (s sdkAdapter) isSDKNotification(method string) bool {
	return method == protocol.NotificationSDKSessionEvent || method == protocol.NotificationSDKSessionStatus
}
