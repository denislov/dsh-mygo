package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/attachment"
	"dsh-mygo/internal/protocol"
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

// promptContentBlocks decodes the typed prompt request back into its wire
// objects so tests assert the order and fields the Host receives.
func promptContentBlocks(t *testing.T, params any) []map[string]any {
	t.Helper()
	raw, ok := requestFields(t, params)["content"].([]any)
	if !ok {
		t.Fatalf("content = %v", requestFields(t, params)["content"])
	}
	blocks := make([]map[string]any, len(raw))
	for index, value := range raw {
		blocks[index], ok = value.(map[string]any)
		if !ok {
			t.Fatalf("block %d = %v", index, value)
		}
	}
	return blocks
}

func TestSendCarriesStagedImagesAsCanonicalBase64(t *testing.T) {
	client := &fakeClient{result: json.RawMessage(`{"messageId":"m1"}`)}
	application, updates := newMygoSendApp(client)
	data := []byte("\x89PNG\r\n\x1a\nfake-bytes")
	application.model.Attachments.Pending = []attachment.Draft{{Name: "shot.png", MediaType: attachment.MediaPNG, Data: data, Width: 1, Height: 1}}

	application.Send("look at this")

	waitForCalls(t, client, 1)
	blocks := promptContentBlocks(t, client.params[0])
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
	blocks := promptContentBlocks(t, client.params[0])
	if len(blocks) != 1 || blocks[0]["type"] != "image" {
		t.Fatalf("blocks = %+v", blocks)
	}
}

func TestSendPreservesTextAndImageOrder(t *testing.T) {
	client := &fakeClient{result: json.RawMessage(`{"messageId":"m1"}`)}
	application, _ := newMygoSendApp(client)
	application.model.Attachments.Pending = []attachment.Draft{
		{Name: "first.png", MediaType: attachment.MediaPNG, Data: []byte("one")},
		{Name: "second.png", MediaType: attachment.MediaPNG, Data: []byte("two")},
	}

	application.Send("look")

	waitForCalls(t, client, 1)
	blocks := promptContentBlocks(t, client.params[0])
	if len(blocks) != 3 || blocks[0]["type"] != "text" || blocks[1]["name"] != "first.png" || blocks[2]["name"] != "second.png" {
		t.Fatalf("blocks = %+v", blocks)
	}
	if blocks[1]["data"] != base64.StdEncoding.EncodeToString([]byte("one")) || blocks[2]["data"] != base64.StdEncoding.EncodeToString([]byte("two")) {
		t.Fatal("image order or encoding changed")
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
		"server": {"name": "dsh-mygo-host", "version": "0.1.1", "dshVersion": "0.2.1-rc.2"},
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
		"session/create": json.RawMessage(`{"session":{"id":"s1","title":"","cwd":"C:/work","provider":"","model":"","effort":"","createdAt":1,"updatedAt":1,"status":"idle","archived":false,"nextSeq":0}}`),
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
	capabilities := client.params[0].(protocol.InitializeParams).Capabilities
	if !strings.Contains(strings.Join(capabilities, ","), "attachments") {
		t.Fatalf("capabilities = %v", capabilities)
	}
}

func TestAttachImageStagesValidDraft(t *testing.T) {
	application, _ := newMygoSendApp(&fakeClient{})
	application.attachImage("shot.png", attachment.MediaPNG, pngBytes(t))
	pending := application.model.Attachments.Pending
	if len(pending) != 1 || pending[0].Width != 2 || pending[0].Height != 2 || pending[0].Name != "shot.png" {
		t.Fatalf("pending = %+v", pending)
	}
	if application.model.Attachments.Error != "" {
		t.Fatalf("error = %q", application.model.Attachments.Error)
	}
}

func TestAttachImageRefusesUnsupportedHost(t *testing.T) {
	application, _ := newMygoSendApp(&fakeClient{})
	application.acceptAttachments(nil)
	application.attachImage("shot.png", attachment.MediaPNG, pngBytes(t))
	if len(application.model.Attachments.Pending) != 0 {
		t.Fatalf("pending = %+v", application.model.Attachments.Pending)
	}
	if application.model.Attachments.Error != attachment.ErrUnsupported.Error() {
		t.Fatalf("error = %q", application.model.Attachments.Error)
	}
}

func TestAttachImageRefusesOversizedImages(t *testing.T) {
	application, _ := newMygoSendApp(&fakeClient{})
	limits := testLimits()
	limits.MaxImageBytes = 4
	application.acceptAttachments(limits)
	application.attachImage("shot.png", attachment.MediaPNG, pngBytes(t))
	if len(application.model.Attachments.Pending) != 0 {
		t.Fatalf("pending = %+v", application.model.Attachments.Pending)
	}
	if application.model.Attachments.Error != attachment.ErrTooLarge.Error() {
		t.Fatalf("error = %q", application.model.Attachments.Error)
	}
}

func TestAttachFilesStagesImagesAndReferences(t *testing.T) {
	directory := t.TempDir()
	imagePath := filepath.Join(directory, "shot.png")
	if err := os.WriteFile(imagePath, pngBytes(t), 0o600); err != nil {
		t.Fatal(err)
	}
	textPath := filepath.Join(directory, "notes.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	application, updates := newMygoSendApp(&fakeClient{})
	application.AttachFiles([]string{imagePath, textPath})
	waitFor(t, application, updates, 2*time.Second, func() bool {
		return len(application.model.Attachments.Pending) == 1 && strings.Contains(application.model.Draft, "@"+filepath.Clean(textPath))
	})
	pending := application.model.Attachments.Pending
	if pending[0].MediaType != attachment.MediaPNG || pending[0].Width != 2 || pending[0].Height != 2 {
		t.Fatalf("pending = %+v", pending)
	}
	if application.model.Attachments.Error != "" {
		t.Fatalf("error = %q", application.model.Attachments.Error)
	}
}

func TestAttachFilesRefusesUnsupportedHostWithoutReads(t *testing.T) {
	application, _ := newMygoSendApp(&fakeClient{})
	application.acceptAttachments(nil)
	application.AttachFiles([]string{"C:/missing/shot.png"})
	if application.model.Attachments.Error != attachment.ErrUnsupported.Error() {
		t.Fatalf("error = %q", application.model.Attachments.Error)
	}
}
