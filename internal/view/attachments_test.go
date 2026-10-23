package view

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"dsh-mygo/internal/attachment"
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

func TestComposerPastesClipboardImages(t *testing.T) {
	var mediaType string
	var data []byte
	model := sessionModel()
	model.Attachments.Supported = true
	model.Attachments.AttachImage = func(_ string, mediaTypeValue string, bytes []byte) {
		mediaType, data = mediaTypeValue, bytes
	}
	tt := tester(model)
	png := []byte("\x89PNG\r\n\x1a\npasted")
	tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, png))))

	tt.Key(ui.Ctrl, ui.KeyV)

	if mediaType != attachment.MediaPNG || string(data) != string(png) {
		t.Fatalf("pasted = %q %q", mediaType, data)
	}
	if model.Draft != "" {
		t.Fatalf("draft = %q", model.Draft)
	}
}

func TestComposerDroppedFilesReachTheApp(t *testing.T) {
	var dropped []string
	model := sessionModel()
	model.Attachments.Supported = true
	model.Attachments.AttachFiles = func(paths []string) { dropped = paths }
	tt := tester(model)
	x, y := float32(450), float32(640)
	if rect, ok := tt.Find(model.T("Message")); ok {
		x, y = rect.X+rect.W/2, rect.Y+rect.H/2
	}

	if !tt.DropFiles(x, y, "C:/tmp/shot.png") {
		t.Fatalf("drop not handled, texts = %v", tt.Texts())
	}
	if len(dropped) != 1 || dropped[0] != "C:/tmp/shot.png" {
		t.Fatalf("dropped = %v", dropped)
	}
}

func TestComposerChipsShowAndRemoveStagedImages(t *testing.T) {
	model := sessionModel()
	model.Attachments.Supported = true
	model.Attachments.Pending = []attachment.Draft{{
		Name: "shot.png", MediaType: attachment.MediaPNG, Data: []byte("png"),
	}}
	tt := tester(model)
	if !tt.HasText("Image · image/png · 3 B · shot.png") {
		t.Fatalf("chip missing in %v", tt.Texts())
	}
	if err := tt.Click("Remove 1"); err != nil {
		t.Fatal(err)
	}
	if len(model.Attachments.Pending) != 0 {
		t.Fatalf("pending = %+v", model.Attachments.Pending)
	}
	if tt.HasText("Remove 1") {
		t.Fatalf("chip still shown in %v", tt.Texts())
	}
}

func TestComposerSendsImagesWithoutText(t *testing.T) {
	var sent []string
	model := sessionModel()
	model.Attachments.Supported = true
	model.Attachments.Pending = []attachment.Draft{{MediaType: attachment.MediaPNG, Data: []byte("png")}}
	model.Attachments.PickAttachment = func() {}
	model.Send = func(text string) { sent = append(sent, text) }
	tt := tester(model)
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != "" {
		t.Fatalf("sent = %v", sent)
	}
	if tt.HasText("Attach image…") == false {
		t.Fatalf("attach action missing in %v", tt.Texts())
	}
}

func TestComposerShowsAttachmentErrorAndPicker(t *testing.T) {
	picked := 0
	model := sessionModel()
	model.Attachments.Supported = true
	model.Attachments.PickAttachment = func() { picked++ }
	model.Attachments.Error = attachment.ErrTooMany.Error()
	tt := tester(model)
	if !tt.HasText(model.T(attachment.ErrTooMany.Error())) {
		t.Fatalf("error missing in %v", tt.Texts())
	}
	if err := tt.Click("Attach image…"); err != nil {
		t.Fatal(err)
	}
	if picked != 1 {
		t.Fatalf("picked = %d", picked)
	}
}
