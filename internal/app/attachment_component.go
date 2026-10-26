package app

import (
	"sync/atomic"

	"dsh-mygo/internal/attachment"
)

// attachmentComponent owns the negotiated image policy, staging checks, preview
// identities and mention completion generations. Attachment bytes stay in the
// model's unsent draft only; they are never written to draft files or logs.
type attachmentComponent struct {
	limits        *attachment.Limits
	sessionOf     func() string
	nextPreviewID uint64
	mentionSeq    atomic.Uint64
}

func (c *attachmentComponent) setLimits(limit *attachment.Limits) { c.limits = limit }

func (c *attachmentComponent) limitsValue() (attachment.Limits, bool) {
	if c.limits == nil {
		return attachment.Limits{}, false
	}
	return *c.limits, true
}

func (c *attachmentComponent) session() string {
	if c.sessionOf != nil {
		return c.sessionOf()
	}
	return ""
}

// currentSession reports whether a read started for session is still allowed to
// publish into the visible conversation.
func (c *attachmentComponent) currentSession(session string) bool {
	return session == c.session()
}

// stage validates one new upload against the advertised per-message budget and
// the uploads already staged.
func (c *attachmentComponent) stage(pending []attachment.Draft, draft attachment.Draft) ([]attachment.Draft, error) {
	limits, ok := c.limitsValue()
	if !ok {
		return pending, attachment.ErrUnsupported
	}
	if len(pending) >= limits.MaxImagesPerMessage {
		return pending, attachment.ErrTooMany
	}
	batch := append(append([]attachment.Draft{}, pending...), draft)
	if err := limits.Validate(batch); err != nil {
		return pending, err
	}
	return batch, nil
}

// newPreview reserves an identity that survives reordering and removal.
func (c *attachmentComponent) newPreview() uint64 {
	c.nextPreviewID++
	return c.nextPreviewID
}

// acceptPreview rejects a thumbnail decoded for another session.
func (c *attachmentComponent) acceptPreview(session string) bool {
	return c.currentSession(session)
}

func (c *attachmentComponent) nextMention() uint64 { return c.mentionSeq.Add(1) }

// acceptMention rejects completion results for a superseded prefix or session.
func (c *attachmentComponent) acceptMention(sequence uint64, session string) bool {
	return session == c.session() && sequence == c.mentionSeq.Load()
}
