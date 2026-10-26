// Package protocol defines UI/Host wire vocabulary without transport or UI state.
package protocol

type Capability string

const (
	CapabilitySessionCancel           Capability = "session.cancel"
	CapabilityApproval                Capability = "approval"
	CapabilityQuestion                Capability = "question"
	CapabilityAssistantStream         Capability = "session.assistant-stream"
	CapabilitySessionHistory          Capability = "session.history"
	CapabilityHistoryWindow           Capability = "session.history-window"
	CapabilitySessionRelease          Capability = "session.release"
	CapabilitySessionResync           Capability = "session.resync"
	CapabilityStreamBaseline          Capability = "session.assistant-stream-baseline"
	CapabilityStreamBaselineReasoning Capability = "session.assistant-stream-baseline-reasoning"
	CapabilitySessionRename           Capability = "session.rename"
	CapabilitySessionArchive          Capability = "session.archive"
	CapabilityModelSelection          Capability = "model.selection"
	CapabilityConfiguration           Capability = "settings.configuration"
	CapabilitySessionPermissions      Capability = "session.permissions"
	CapabilityAttachments             Capability = "attachments"
	CapabilitySubagentInspection      Capability = "subagent.inspection"
)

// ClientCapabilities returns an owned slice in the established handshake order.
// Mutating one request cannot alter a later host generation's offer.
func ClientCapabilities() []string {
	capabilities := [...]Capability{
		CapabilitySessionCancel, CapabilityApproval, CapabilityQuestion, CapabilityAssistantStream,
		CapabilitySessionHistory, CapabilityHistoryWindow, CapabilitySessionRelease, CapabilitySessionResync,
		CapabilityStreamBaseline, CapabilityStreamBaselineReasoning, CapabilitySessionRename, CapabilitySessionArchive,
		CapabilityModelSelection, CapabilityConfiguration, CapabilitySessionPermissions, CapabilityAttachments,
		CapabilitySubagentInspection,
	}
	result := make([]string, len(capabilities))
	for i, capability := range capabilities {
		result[i] = string(capability)
	}
	return result
}

// Capabilities is the known subset of one validated handshake, not a wire DTO.
// Its zero value enables nothing, including in explicit SDK regression mode.
type Capabilities struct {
	Cancel                  bool
	Approval                bool
	Question                bool
	Stream                  bool
	History                 bool
	HistoryWindow           bool
	Release                 bool
	Resync                  bool
	StreamBaseline          bool
	StreamBaselineReasoning bool
	Rename                  bool
	Archive                 bool
	Models                  bool
	Configure               bool
	Permissions             bool
	Attachments             bool
	InspectSubagents        bool
}

// ParseCapabilities ignores unknown names and treats duplicates idempotently.
// It does not validate the envelope or infer dependencies: callers first apply
// the existing handshake validation, including attachment policy consistency.
func ParseCapabilities(values []string) (result Capabilities) {
	for _, value := range values {
		switch Capability(value) {
		case CapabilitySessionCancel:
			result.Cancel = true
		case CapabilityApproval:
			result.Approval = true
		case CapabilityQuestion:
			result.Question = true
		case CapabilityAssistantStream:
			result.Stream = true
		case CapabilitySessionHistory:
			result.History = true
		case CapabilityHistoryWindow:
			result.HistoryWindow = true
		case CapabilitySessionRelease:
			result.Release = true
		case CapabilitySessionResync:
			result.Resync = true
		case CapabilityStreamBaseline:
			result.StreamBaseline = true
		case CapabilityStreamBaselineReasoning:
			result.StreamBaselineReasoning = true
		case CapabilitySessionRename:
			result.Rename = true
		case CapabilitySessionArchive:
			result.Archive = true
		case CapabilityModelSelection:
			result.Models = true
		case CapabilityConfiguration:
			result.Configure = true
		case CapabilitySessionPermissions:
			result.Permissions = true
		case CapabilityAttachments:
			result.Attachments = true
		case CapabilitySubagentInspection:
			result.InspectSubagents = true
		}
	}
	return result
}
