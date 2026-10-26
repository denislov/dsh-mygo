package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestClientCapabilitiesMatchWireFixtureAndOwnTheirSlice(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "protocol", "capabilities.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ ClientCapabilities []string }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	first := ClientCapabilities()
	if !slices.Equal(first, fixture.ClientCapabilities) {
		t.Fatalf("offered capabilities changed: %v", first)
	}
	first[0] = "modified"
	if !slices.Equal(ClientCapabilities(), fixture.ClientCapabilities) {
		t.Fatal("a caller can mutate future handshakes")
	}
}

func TestParseCapabilitiesEnablesOnlyExactKnownNames(t *testing.T) {
	for _, test := range []struct {
		name Capability
		want Capabilities
	}{
		{CapabilitySessionCancel, Capabilities{Cancel: true}},
		{CapabilityApproval, Capabilities{Approval: true}},
		{CapabilityQuestion, Capabilities{Question: true}},
		{CapabilityAssistantStream, Capabilities{Stream: true}},
		{CapabilitySessionHistory, Capabilities{History: true}},
		{CapabilityHistoryWindow, Capabilities{HistoryWindow: true}},
		{CapabilitySessionRelease, Capabilities{Release: true}},
		{CapabilitySessionResync, Capabilities{Resync: true}},
		{CapabilityStreamBaseline, Capabilities{StreamBaseline: true}},
		{CapabilityStreamBaselineReasoning, Capabilities{StreamBaselineReasoning: true}},
		{CapabilitySessionRename, Capabilities{Rename: true}},
		{CapabilitySessionArchive, Capabilities{Archive: true}},
		{CapabilityModelSelection, Capabilities{Models: true}},
		{CapabilityConfiguration, Capabilities{Configure: true}},
		{CapabilitySessionPermissions, Capabilities{Permissions: true}},
		{CapabilityAttachments, Capabilities{Attachments: true}},
		{CapabilitySubagentInspection, Capabilities{InspectSubagents: true}},
	} {
		t.Run(string(test.name), func(t *testing.T) {
			wire := []string{"future.test", string(test.name), string(test.name)}
			if got := ParseCapabilities(wire); got != test.want {
				t.Fatalf("parsed = %+v, want %+v", got, test.want)
			}
		})
	}
	if got := ParseCapabilities([]string{"future.test", "SESSION.CANCEL", "session.history.extra", ""}); got != (Capabilities{}) {
		t.Fatalf("unknown names enabled a capability: %+v", got)
	}
	if ParseCapabilities(nil) != (Capabilities{}) || ParseCapabilities([]string{}) != (Capabilities{}) {
		t.Fatal("empty handshake enabled capabilities")
	}
}
