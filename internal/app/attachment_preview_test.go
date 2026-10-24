package app

import (
	"bytes"
	"image"
	"image/png"
	"testing"
	"time"

	"dsh-mygo/internal/attachment"
)

func nextPreviewUpdate(t *testing.T, updates <-chan func()) func() {
	t.Helper()
	select {
	case update := <-updates:
		return update
	case <-time.After(3 * time.Second):
		t.Fatal("preview worker did not publish")
		return nil
	}
}

func TestAttachmentPreviewRejectsLateResultsAfterRemoveAndSwitch(t *testing.T) {
	a, updates := newMygoSendApp(&fakeClient{})
	a.attachImage("one.png", attachment.MediaPNG, pngBytes(t))
	a.attachImage("two.png", attachment.MediaPNG, pngBytes(t))
	first, second := nextPreviewUpdate(t, updates), nextPreviewUpdate(t, updates)
	remainingID := a.model.Attachments.Previews[1].ID
	a.model.Attachments.Remove(0)
	first()
	second()
	if len(a.model.Attachments.Previews) != 1 || a.model.Attachments.Previews[0].ID != remainingID || a.model.Attachments.Preview(0) == nil {
		t.Fatal("late preview attached to a removed image or lost the surviving image")
	}
	a.model.Attachments.Clear()
	a.attachImage("three.png", attachment.MediaPNG, pngBytes(t))
	third := nextPreviewUpdate(t, updates)
	first()
	second()
	if a.model.Attachments.Preview(0) != nil {
		t.Fatal("old generation repopulated a cleared preview")
	}
	a.model.SessionID = "different-session"
	third()
	if a.model.Attachments.Preview(0) != nil {
		t.Fatal("preview crossed session identity")
	}
}

func TestAttachmentPreviewUsesNativeDecoderAndBoundsPixels(t *testing.T) {
	bitmap := attachmentThumbnail(pngBytes(t))
	if bitmap == nil {
		t.Fatal("valid image has no preview")
	}
	w, h := bitmap.Size()
	if w != 2 || h != 2 {
		t.Fatalf("preview dimensions %d x %d", w, h)
	}
	if attachmentThumbnail([]byte("not an image")) != nil {
		t.Fatal("invalid image got a preview")
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2001, 2000))); err != nil {
		t.Fatal(err)
	}
	if attachmentThumbnail(encoded.Bytes()) != nil {
		t.Fatal("preview exceeded its independent pixel budget")
	}
}
