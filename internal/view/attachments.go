package view

import (
	"fmt"

	"dsh-mygo/internal/attachment"
	"dsh-mygo/internal/projection"
)

// AttachmentState is the composer's staged uploads and the host policy that
// bounds them. Uploads are session-local and never persisted with drafts.
type AttachmentState struct {
	Supported bool
	Limits    attachment.Limits
	Pending   []attachment.Draft
	Error     string
}

// Clear drops the staged uploads and their error, keeping the policy.
func (state *AttachmentState) Clear() {
	state.Pending = nil
	state.Error = ""
}

// attachmentLabel renders the display-safe metadata of one durable upload.
// Bytes and dimensions are optional: older logs may omit them.
func attachmentLabel(model *Model, attachment projection.Attachment) string {
	label := model.T("Image") + " · " + attachment.MediaType
	if attachment.Width > 0 && attachment.Height > 0 {
		label += fmt.Sprintf(" · %d×%d", attachment.Width, attachment.Height)
	}
	if attachment.Bytes > 0 {
		label += " · " + byteLabel(attachment.Bytes)
	}
	if attachment.Name != "" {
		label += " · " + attachment.Name
	}
	return label
}

// byteLabel formats an encoded size for a one-line attachment chip.
func byteLabel(bytes int64) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d B", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1f MiB", float64(bytes)/(1024*1024))
	}
}
