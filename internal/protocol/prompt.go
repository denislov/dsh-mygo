package protocol

// ContentBlock is one prompt part. Concrete blocks own their exact wire fields;
// tool or extension blocks added later may carry bounded raw JSON.
type ContentBlock interface {
	contentBlock()
}

// TextBlock is one user-visible text part.
type TextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (TextBlock) contentBlock() {}

func NewTextBlock(text string) TextBlock {
	return TextBlock{Type: "text", Text: text}
}

// ImageBlock is one canonical-base64 raster part. An empty name is omitted.
type ImageBlock struct {
	Type     string `json:"type"`
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
	Name     string `json:"name,omitempty"`
}

func (ImageBlock) contentBlock() {}

func NewImageBlock(mimeType, data, name string) ImageBlock {
	return ImageBlock{Type: "image", MimeType: mimeType, Data: data, Name: name}
}

type SessionPromptParams struct {
	SessionID string         `json:"sessionId"`
	Content   []ContentBlock `json:"content"`
}

// SDKPromptParams is the explicit legacy SDK spelling of the same prompt.
type SDKPromptParams struct {
	SessionID     string         `json:"sessionId"`
	ContentBlocks []ContentBlock `json:"contentBlocks"`
}

type PromptResult struct {
	MessageID string `json:"messageId"`
}

type SessionCancelParams struct {
	SessionID string `json:"sessionId"`
}

// SessionCancelResult keeps cancelled as a pointer: a missing or null field must
// not read as a confirmed idle cancellation.
type SessionCancelResult struct {
	Cancelled *bool  `json:"cancelled"`
	Status    string `json:"status"`
}
