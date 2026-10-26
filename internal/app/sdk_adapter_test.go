package app

import (
	"testing"

	"dsh-mygo/internal/protocol"
)

func TestSDKAdapterOwnsLegacySpellingsAndCapabilityGate(t *testing.T) {
	options := Options{SDKProtocol: true, SessionID: "sdk-session", CWD: "C:/work", Provider: "test", Model: "echo"}
	adapter := newSDKAdapter(&options)
	if !adapter.enabled() || adapter.richUI() || !adapter.isSDKNotification(protocol.NotificationSDKSessionEvent) {
		t.Fatal("SDK adapter did not report its mode")
	}
	if got := adapter.notificationMethod(protocol.NotificationSessionEvent); got != protocol.NotificationSDKSessionEvent {
		t.Fatalf("event method = %q", got)
	}
	if got := adapter.notificationMethod(protocol.NotificationSessionStatus); got != protocol.NotificationSDKSessionStatus {
		t.Fatalf("status method = %q", got)
	}
	if got := adapter.notificationMethod(protocol.NotificationHostFatal); got != protocol.NotificationHostFatal {
		t.Fatalf("unrelated method changed: %q", got)
	}
	blocks := []protocol.ContentBlock{protocol.NewTextBlock("hello")}
	if request, ok := adapter.promptRequest("sdk-session", blocks).(protocol.SDKPromptParams); !ok || request.SessionID != "sdk-session" || len(request.ContentBlocks) != 1 {
		t.Fatalf("prompt request = %#v", adapter.promptRequest("sdk-session", blocks))
	}

	options.SDKProtocol = false
	if adapter.enabled() || !adapter.richUI() {
		t.Fatal("adapter did not follow the composition-root option")
	}
}
