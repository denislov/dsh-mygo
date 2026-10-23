package app

import (
	"bytes"
	"image"
	"image/png"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"dsh-mygo/internal/attachment"
	"dsh-mygo/internal/host"
	"dsh-mygo/internal/projection"
	"dsh-mygo/internal/view"
)

// onePixelPNG is a valid 1×1 PNG, small enough for every advertised budget.
const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFAAH/q842iQAAAABJRU5ErkJggg=="

func pngBytes(t *testing.T) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func hasUserAttachment(items []projection.Item, mediaType string) bool {
	for _, item := range items {
		for _, entry := range item.Attachments {
			if item.Kind == projection.KindUser && entry.MediaType == mediaType {
				return true
			}
		}
	}
	return false
}

// TestAgainstMygoFakeHostAttachments drives paste-to-transcript through the
// fake custom host: capability negotiation, base64 prompt block, durable
// reference in the user event, and a metadata row in the projection.
func TestAgainstMygoFakeHostAttachments(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	fakeHost, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 128)
	application := New(Options{
		Launch: host.Config{Command: node, Args: []string{fakeHost, "--models"}},
		CWD:    t.TempDir(), Provider: "test", Model: "echo",
		Post: func(fn func()) { updates <- fn },
	})
	defer application.Shutdown(2 * time.Second)
	go application.Start()
	waitFor(t, application, updates, 20*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })
	if !application.Model().Attachments.Supported {
		t.Fatal("host did not advertise attachment support")
	}
	if !application.Model().Attachments.Limits.Accepts(attachment.MediaPNG) {
		t.Fatalf("limits = %+v", application.Model().Attachments.Limits)
	}

	application.Model().Attachments.Pending = []attachment.Draft{{
		Name: "shot.png", MediaType: attachment.MediaPNG, Data: pngBytes(t), Width: 1, Height: 1,
	}}
	application.Send("look at this")
	waitFor(t, application, updates, 20*time.Second, func() bool {
		return application.Model().Phase == view.PhaseIdle && hasUserAttachment(application.Model().Items, attachment.MediaPNG)
	})
	items := application.Model().Items
	if len(items) == 0 || items[0].Kind != projection.KindUser || items[0].Text != "look at this" {
		t.Fatalf("items = %+v", items)
	}
	if len(items[0].Attachments) != 1 || items[0].Attachments[0].Bytes != int64(len(pngBytes(t))) {
		t.Fatalf("attachments = %+v", items[0].Attachments)
	}
	if pending := application.Model().Attachments.Pending; len(pending) != 0 {
		t.Fatalf("staged uploads survived the send: %+v", pending)
	}
}

// TestAgainstMygoRealAttachments sends one image through the real DSH
// composition with the mock adapter: the base bundle's attachment store must
// persist it before the user event, and history must carry only metadata.
func TestAgainstMygoRealAttachments(t *testing.T) {
	launch, root := realMygoLaunch(t, true)
	updates := make(chan func(), 512)
	application := New(Options{
		Launch: launch, CWD: root, Provider: "mygo-mock", Model: "echo",
		Post: func(fn func()) { updates <- fn },
	})
	defer application.Shutdown(10 * time.Second)
	go application.Start()
	waitFor(t, application, updates, 90*time.Second, func() bool { return application.Model().Phase == view.PhaseIdle })
	if !application.Model().Attachments.Supported {
		t.Fatalf("real host did not advertise attachments: %+v", application.Model().Attachments)
	}

	application.Model().Attachments.Pending = []attachment.Draft{{
		Name: "shot.png", MediaType: attachment.MediaPNG, Data: pngBytes(t), Width: 1, Height: 1,
	}}
	application.Send("look at this")
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case fn := <-updates:
			fn()
		case <-time.After(10 * time.Millisecond):
		}
		if hasUserAttachment(application.Model().Items, attachment.MediaPNG) || application.Model().Phase == view.PhaseFailed {
			break
		}
	}
	if !hasUserAttachment(application.Model().Items, attachment.MediaPNG) {
		t.Fatalf("phase=%v error=%q detail=%q items=%+v", application.Model().Phase, application.Model().Error, application.Model().ErrorDetail, application.Model().Items)
	}
	found := false
	for _, item := range application.Model().Items {
		if item.Kind != projection.KindUser || len(item.Attachments) != 1 {
			continue
		}
		found = true
		entry := item.Attachments[0]
		// The real store normalizes the raster, so sizes are its own facts.
		if entry.ID == "" || entry.Bytes <= 0 || entry.Width != 2 || entry.Height != 2 {
			t.Fatalf("attachment metadata = %+v", entry)
		}
		if entry.ID == "shot.png" {
			t.Fatal("attachment reference leaked a path or name")
		}
	}
	if !found {
		t.Fatalf("no user attachment row: %+v", application.Model().Items)
	}
	application.Shutdown(10 * time.Second)
}
