package projection

import (
	"encoding/json"
	"testing"
)

func TestUserImageBlocksBecomeAttachmentRows(t *testing.T) {
	var transcript Transcript
	event := Event{Seq: 1, Type: "user/message", Data: json.RawMessage(`{
		"source": {"kind": "user"},
		"content": [
			{"type": "text", "text": "look"},
			{"type": "image", "attachment": {"attachmentId": "sha256:abc", "mediaType": "image/png", "bytes": 67, "width": 1, "height": 1, "name": "shot.png"}}
		]
	}`)}
	if !transcript.Apply(event) {
		t.Fatal("image attachment was not projected")
	}
	items := transcript.Items()
	if len(items) != 1 || items[0].Text != "look" || len(items[0].Attachments) != 1 {
		t.Fatalf("items = %+v", items)
	}
	attachment := items[0].Attachments[0]
	if attachment.ID != "sha256:abc" || attachment.MediaType != "image/png" || attachment.Bytes != 67 || attachment.Width != 1 || attachment.Name != "shot.png" {
		t.Fatalf("attachment = %+v", attachment)
	}
}

func TestUserImageOnlyMessagesStayVisible(t *testing.T) {
	var transcript Transcript
	if !transcript.Apply(Event{Seq: 1, Type: "user/message", Data: json.RawMessage(`{
		"source": {"kind": "user"},
		"content": [{"type": "image", "attachment": {"attachmentId": "sha256:abc", "mediaType": "image/webp", "bytes": 9, "width": 2, "height": 3}}]
	}`)}) {
		t.Fatal("image-only message was dropped")
	}
	items := transcript.Items()
	if len(items) != 1 || items[0].Text != "" || len(items[0].Attachments) != 1 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Attachments[0].Name != "" {
		t.Fatalf("attachment = %+v", items[0].Attachments[0])
	}
}

func TestMalformedAttachmentReferencesNeverReachTheUi(t *testing.T) {
	var transcript Transcript
	transcript.Apply(Event{Seq: 1, Type: "user/message", Data: json.RawMessage(`{
		"source": {"kind": "user"},
		"content": [
			{"type": "text", "text": "keep"},
			{"type": "image", "attachment": {"attachmentId": "", "mediaType": "image/png"}},
			{"type": "image", "attachment": {"attachmentId": "sha256:bad media", "mediaType": "image/png"}},
			{"type": "image", "attachment": {"attachmentId": "sha256:ok", "mediaType": "image/svg+xml"}},
			{"type": "image", "attachment": {"attachmentId": "sha256:wide", "mediaType": "image/png", "width": 999999, "height": 1}},
			{"type": "image", "attachment": {"attachmentId": "sha256:neg", "mediaType": "image/png", "bytes": -1}},
			{"type": "image", "attachment": {"attachmentId": "sha256:good", "mediaType": "image/gif", "bytes": 3, "width": 1, "height": 1, "name": "ok.gif"}}
		]
	}`)})
	items := transcript.Items()
	if len(items) != 1 || items[0].Text != "keep" {
		t.Fatalf("items = %+v", items)
	}
	if len(items[0].Attachments) != 1 || items[0].Attachments[0].ID != "sha256:good" {
		t.Fatalf("attachments = %+v", items[0].Attachments)
	}
}

func TestSyntheticUserContextWithImagesStaysHidden(t *testing.T) {
	var transcript Transcript
	transcript.Apply(Event{Seq: 1, Type: "user/message", Data: json.RawMessage(`{
		"source": {"kind": "runtime-context"},
		"content": [{"type": "image", "attachment": {"attachmentId": "sha256:abc", "mediaType": "image/png"}}]
	}`)})
	if len(transcript.Items()) != 0 {
		t.Fatalf("items = %+v", transcript.Items())
	}
}
