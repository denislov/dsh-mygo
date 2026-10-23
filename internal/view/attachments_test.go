package view

import (
	"strings"
	"testing"

	"dsh-mygo/internal/projection"
)

func TestUserAttachmentRowsShowMetadataWithoutText(t *testing.T) {
	model := sessionModel()
	model.Items = []projection.Item{{
		ID: "u", Kind: projection.KindUser, Title: "You",
		Attachments: []projection.Attachment{{ID: "sha256:abc", MediaType: "image/png", Bytes: 2048, Width: 1200, Height: 800, Name: "shot.png"}},
	}}
	tt := tester(model)
	want := "Image · image/png · 1200×800 · 2.0 KiB · shot.png"
	if !tt.HasText(want) {
		t.Fatalf("missing %q in %v", want, tt.Texts())
	}
	if err := tt.Click("Copy message"); err == nil {
		t.Fatal("copy action offered for a message without text")
	}
}

func TestUserAttachmentRowsFollowTextAndLocalize(t *testing.T) {
	model := sessionModel()
	model.Items = []projection.Item{{
		ID: "u", Kind: projection.KindUser, Title: "You", Text: "look",
		Attachments: []projection.Attachment{{ID: "sha256:abc", MediaType: "image/jpeg", Bytes: 512}},
	}}
	tt := tester(model)
	if !tt.HasText("look") || !tt.HasText("Image · image/jpeg · 512 B") {
		t.Fatalf("texts = %v", tt.Texts())
	}
	if err := tt.Click("Copy message"); err != nil {
		t.Fatal(err)
	}
	model.Prefs.Value.Locale = "zh-CN"
	tt.Frame()
	if !tt.HasText("图片 · image/jpeg · 512 B") {
		t.Fatalf("Chinese chip missing in %v", tt.Texts())
	}
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "look") {
		t.Fatalf("conversation payload was translated: %v", tt.Texts())
	}
}
