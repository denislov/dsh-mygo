package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/attachment"
	"dsh-mygo/internal/view"
)

func testLimits() *attachment.Limits {
	return &attachment.Limits{
		MediaTypes:           []string{attachment.MediaPNG, attachment.MediaJPEG},
		MaxBase64Bytes:       8 * 1024 * 1024,
		MaxImageBytes:        6 * 1024 * 1024,
		MaxImagesPerMessage:  8,
		MaxMessageImageBytes: 6 * 1024 * 1024,
		MaxImagePixels:       64000000,
		MaxImageDimension:    8192,
	}
}

func newMygoSendApp(client rpcClient) (*App, chan func()) {
	updates := make(chan func(), 64)
	application := New(Options{
		CWD:       "C:/work",
		Provider:  "test",
		Model:     "echo",
		SessionID: "s1",
		Post:      func(fn func()) { updates <- fn },
	})
	application.client = client
	application.model.Phase = view.PhaseIdle
	application.acceptAttachments(testLimits())
	return application, updates
}

func waitForCalls(t *testing.T, client *fakeClient, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(client.called()) < count && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls := client.called(); len(calls) < count {
		t.Fatalf("calls = %v", calls)
	}
}

func waitForPendingCleared(t *testing.T, application *App, updates <-chan func()) {
	t.Helper()
	waitFor(t, application, updates, 2*time.Second, func() bool {
		return len(application.model.Attachments.Pending) == 0
	})
}

func TestSendCarriesStagedImagesAsCanonicalBase64(t *testing.T) {
	client := &fakeClient{result: json.RawMessage(`{"messageId":"m1"}`)}
	application, updates := newMygoSendApp(client)
	data := []byte("\x89PNG\r\n\x1a\nfake-bytes")
	application.model.Attachments.Pending = []attachment.Draft{{Name: "shot.png", MediaType: attachment.MediaPNG, Data: data, Width: 1, Height: 1}}

	application.Send("look at this")

	waitForCalls(t, client, 1)
	params := client.params[0].(map[string]any)
	blocks := params["content"].([]map[string]any)
	if len(blocks) != 2 {
		t.Fatalf("blocks = %+v", blocks)
	}
	if blocks[0]["type"] != "text" || blocks[0]["text"] != "look at this" {
		t.Fatalf("text block = %+v", blocks[0])
	}
	if blocks[1]["type"] != "image" || blocks[1]["mimeType"] != attachment.MediaPNG || blocks[1]["name"] != "shot.png" {
		t.Fatalf("image block = %+v", blocks[1])
	}
	if blocks[1]["data"] != base64.StdEncoding.EncodeToString(data) {
		t.Fatal("image payload is not canonical base64")
	}
	if application.model.Draft != "" {
		t.Fatalf("draft = %q", application.model.Draft)
	}
	waitForPendingCleared(t, application, updates)
}

func TestSendAllowsImageOnlyMessages(t *testing.T) {
	client := &fakeClient{result: json.RawMessage(`{"messageId":"m1"}`)}
	application, _ := newMygoSendApp(client)
	application.model.Attachments.Pending = []attachment.Draft{{MediaType: attachment.MediaPNG, Data: []byte("png")}}
	application.model.Draft = ""

	application.Send("")

	waitForCalls(t, client, 1)
	blocks := client.params[0].(map[string]any)["content"].([]map[string]any)
	if len(blocks) != 1 || blocks[0]["type"] != "image" {
		t.Fatalf("blocks = %+v", blocks)
	}
}

func TestSendRefusesImagesWithoutNegotiatedPolicy(t *testing.T) {
	client := &fakeClient{}
	application, _ := newMygoSendApp(client)
	application.acceptAttachments(nil)
	application.model.Attachments.Pending = []attachment.Draft{{MediaType: attachment.MediaPNG, Data: []byte("png")}}
	application.model.Draft = "keep me"

	application.Send("keep me")

	if calls := client.called(); len(calls) != 0 {
		t.Fatalf("calls = %v", calls)
	}
	if application.model.Error != attachment.ErrUnsupported.Error() {
		t.Fatalf("error = %q", application.model.Error)
	}
	if application.model.Draft != "keep me" || len(application.model.Attachments.Pending) != 1 {
		t.Fatalf("draft = %q, pending = %+v", application.model.Draft, application.model.Attachments.Pending)
	}
}

func TestSendRefusesImagesOutsideTheAdvertisedLimits(t *testing.T) {
	client := &fakeClient{}
	application, _ := newMygoSendApp(client)
	limits := testLimits()
	limits.MaxImageBytes = 4
	application.acceptAttachments(limits)
	application.model.Attachments.Pending = []attachment.Draft{{MediaType: attachment.MediaPNG, Data: []byte("too-large")}}
	application.model.Draft = "keep me"

	application.Send("keep me")

	if calls := client.called(); len(calls) != 0 {
		t.Fatalf("calls = %v", calls)
	}
	if application.model.Error != attachment.ErrTooLarge.Error() {
		t.Fatalf("error = %q", application.model.Error)
	}
	if application.model.Draft != "keep me" || len(application.model.Attachments.Pending) != 1 {
		t.Fatalf("draft = %q, pending = %+v", application.model.Draft, application.model.Attachments.Pending)
	}
}

func TestAcceptAttachmentsInstallsPolicyAndDropsStagedUploads(t *testing.T) {
	application := newTestApp(&fakeClient{})
	application.model.Attachments.Pending = []attachment.Draft{{MediaType: attachment.MediaPNG, Data: []byte("png")}}

	application.acceptAttachments(testLimits())
	if !application.model.Attachments.Supported || application.model.Attachments.Limits.MaxImagesPerMessage != 8 {
		t.Fatalf("state = %+v", application.model.Attachments)
	}
	if len(application.model.Attachments.Pending) != 0 {
		t.Fatal("staged uploads survived a policy change")
	}

	application.acceptAttachments(nil)
	if application.model.Attachments.Supported || len(application.model.Attachments.Limits.MediaTypes) != 0 {
		t.Fatalf("state = %+v", application.model.Attachments)
	}
}

func TestMygoInitializeParsesAttachmentPolicy(t *testing.T) {
	handshake := `{
		"protocolVersion": 1,
		"server": {"name": "dsh-mygo-host", "version": "0.1.0", "dshVersion": "0.2.1-rc.2"},
		"capabilities": ["attachments"],
		"attachments": {"image": {
			"mediaTypes": ["image/png", "image/jpeg"],
			"maxBase64Bytes": 8388608,
			"maxImageBytes": 6291456,
			"maxImagesPerMessage": 8,
			"maxMessageImageBytes": 6291456,
			"maxImagePixels": 64000000,
			"maxImageDimension": 8192
		}}
	}`
	client := &fakeClient{results: map[string]json.RawMessage{
		"initialize":     json.RawMessage(handshake),
		"session/create": json.RawMessage(`{"session":{"id":"s1","status":"idle","nextSeq":0}}`),
	}}
	application := newTestApp(client)
	application.options.SDKProtocol = false

	initialized, err := application.initialize(context.Background(), client, "")
	if err != nil {
		t.Fatal(err)
	}
	if initialized.attachments == nil {
		t.Fatal("attachment policy was dropped")
	}
	if strings.Join(initialized.attachments.MediaTypes, ",") != "image/png,image/jpeg" || initialized.attachments.MaxImageBytes != 6291456 {
		t.Fatalf("limits = %+v", *initialized.attachments)
	}
	capabilities := client.params[0].(map[string]any)["capabilities"].([]string)
	if !strings.Contains(strings.Join(capabilities, ","), "attachments") {
		t.Fatalf("capabilities = %v", capabilities)
	}
}
