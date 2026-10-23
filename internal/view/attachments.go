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

	// AttachImage stages clipboard or file bytes; the app owns policy checks
	// and I/O for large payloads. AttachFiles classifies file paths, and
	// PickAttachment opens the native file picker.
	AttachImage    func(name, mediaType string, data []byte)
	AttachFiles    func(paths []string)
	PickAttachment func()
}

// Clear drops the staged uploads and their error, keeping the policy.
func (state *AttachmentState) Clear() {
	state.Pending = nil
	state.Error = ""
}

// Remove drops one staged upload without touching the draft.
func (state *AttachmentState) Remove(index int) {
	if index < 0 || index >= len(state.Pending) {
		return
	}
	state.Pending = append(state.Pending[:index], state.Pending[index+1:]...)
	state.Error = ""
}

// attachmentLabel renders the display-safe metadata of one durable upload.
// Bytes and dimensions are optional: older logs may omit them.
func attachmentLabel(model *Model, attachment projection.Attachment) string {
	return imageLabel(model, attachment.MediaType, attachment.Bytes, attachment.Width, attachment.Height, attachment.Name)
}

// draftLabel renders the metadata of one staged, unsent image.
func draftLabel(model *Model, draft attachment.Draft) string {
	return imageLabel(model, draft.MediaType, int64(len(draft.Data)), draft.Width, draft.Height, draft.Name)
}

func imageLabel(model *Model, mediaType string, bytes int64, width, height int, name string) string {
	label := model.T("Image") + " · " + mediaType
	if width > 0 && height > 0 {
		label += fmt.Sprintf(" · %d×%d", width, height)
	}
	if bytes > 0 {
		label += " · " + byteLabel(bytes)
	}
	if name != "" {
		label += " · " + name
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
