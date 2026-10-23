package app

import (
	"encoding/base64"

	"dsh-mygo/internal/attachment"
	"dsh-mygo/internal/channel"
)

// attachmentLimitsOf reads the image policy a host advertised at initialize.
// A host that did not negotiate the capability keeps uploads unavailable.
func attachmentLimitsOf(initialized channel.InitializeResult) *attachment.Limits {
	if initialized.Attachments == nil {
		return nil
	}
	limits := initialized.Attachments.Image
	return &limits
}

// acceptAttachments installs the negotiated image policy and drops staged
// uploads: they never survive a session switch or a host restart.
func (a *App) acceptAttachments(limit *attachment.Limits) {
	a.attachmentLimits = limit
	a.model.Attachments.Supported = limit != nil
	if limit != nil {
		a.model.Attachments.Limits = *limit
	} else {
		a.model.Attachments.Limits = attachment.Limits{}
	}
	a.model.Attachments.Clear()
}

// promptBlocks builds the ordered content blocks for one send: the visible
// text first, then staged images as canonical base64. Validation runs before
// any request, so a refused batch never clears the draft.
func (a *App) promptBlocks(text string) ([]map[string]any, error) {
	pending := a.model.Attachments.Pending
	if len(pending) == 0 {
		return []map[string]any{{"type": "text", "text": text}}, nil
	}
	if a.options.SDKProtocol {
		return nil, attachment.ErrUnsupported
	}
	if a.attachmentLimits == nil {
		return nil, attachment.ErrUnsupported
	}
	if err := a.attachmentLimits.Validate(pending); err != nil {
		return nil, err
	}
	blocks := make([]map[string]any, 0, len(pending)+1)
	if text != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
	}
	for _, draft := range pending {
		block := map[string]any{"type": "image", "mimeType": draft.MediaType, "data": base64.StdEncoding.EncodeToString(draft.Data)}
		if name := attachment.DisplayName(draft.Name); name != "" {
			block["name"] = name
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}
