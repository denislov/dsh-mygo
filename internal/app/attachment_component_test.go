package app

import (
	"testing"

	"dsh-mygo/internal/attachment"
)

func TestAttachmentComponentOwnsStagingPreviewAndSessionBoundary(t *testing.T) {
	var component attachmentComponent
	session := "s1"
	component.sessionOf = func() string { return session }
	limits := testLimits()
	limits.MaxImagesPerMessage = 1
	component.setLimits(limits)

	draft := attachment.Draft{Name: "shot.png", MediaType: attachment.MediaPNG, Data: []byte("png")}
	staged, err := component.stage(nil, draft)
	if err != nil || len(staged) != 1 {
		t.Fatalf("staged = %+v, %v", staged, err)
	}
	if _, err := component.stage(staged, draft); err != attachment.ErrTooMany {
		t.Fatalf("second upload error = %v", err)
	}
	component.setLimits(nil)
	if _, err := component.stage(nil, draft); err != attachment.ErrUnsupported {
		t.Fatalf("unsupported upload error = %v", err)
	}

	first, second := component.newPreview(), component.newPreview()
	if first == 0 || second != first+1 {
		t.Fatalf("preview ids = %d, %d", first, second)
	}
	if !component.acceptPreview("s1") {
		t.Fatal("current session preview was rejected")
	}
	session = "s2"
	if component.acceptPreview("s1") {
		t.Fatal("preview from another session was accepted")
	}

	sequence := component.nextMention()
	if component.acceptMention(sequence, "s1") {
		t.Fatal("mention result from another session was accepted")
	}
	session = "s1"
	if !component.acceptMention(sequence, "s1") {
		t.Fatal("current mention result was rejected")
	}
	newer := component.nextMention()
	if component.acceptMention(sequence, "s1") || !component.acceptMention(newer, "s1") {
		t.Fatal("mention sequence did not supersede the previous result")
	}
}
