package app

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"

	"dsh-mygo/internal/attachment"
	"dsh-mygo/internal/channel"
)

// maxDroppedPaths bounds one drop or picker result.
const maxDroppedPaths = 16

// attachmentLimitsOf reads the image policy a host advertised at initialize.
// A host that did not negotiate the capability keeps uploads unavailable.
func attachmentLimitsOf(initialized channel.InitializeResult) *attachment.Limits {
	if initialized.Attachments == nil {
		return nil
	}
	limits := initialized.Attachments.Image
	return &limits
}

// acceptAttachments installs the negotiated image policy and drops staged
// uploads: they never survive a session switch or a host restart.
func (a *App) acceptAttachments(limit *attachment.Limits) {
	a.attachmentLimits = limit
	a.model.Attachments.Supported = limit != nil
	if limit != nil {
		a.model.Attachments.Limits = *limit
	} else {
		a.model.Attachments.Limits = attachment.Limits{}
	}
	a.model.Attachments.Clear()
}

// attachImage stages one image on the main thread after checking the advertised
// policy. Bytes are bounded by the clipboard or the file size the caller read.
func (a *App) attachImage(name, mediaType string, data []byte) {
	state := &a.model.Attachments
	if !state.Supported || a.attachmentLimits == nil {
		state.Error = attachment.ErrUnsupported.Error()
		return
	}
	if len(state.Pending) >= a.attachmentLimits.MaxImagesPerMessage {
		state.Error = attachment.ErrTooMany.Error()
		return
	}
	draft := attachment.Draft{Name: attachment.DisplayName(name), MediaType: mediaType, Data: data}
	if width, height, ok := imageDimensions(data); ok {
		draft.Width, draft.Height = width, height
	}
	batch := append(append([]attachment.Draft{}, state.Pending...), draft)
	if err := a.attachmentLimits.Validate(batch); err != nil {
		state.Error = err.Error()
		return
	}
	state.Pending = append(state.Pending, draft)
	state.Error = ""
}

// AttachFiles classifies dropped or picked paths: accepted images become staged
// uploads, other files become @path references in the draft. Reads run on a
// goroutine and publish through Post; staged uploads belong to the session they
// were read for.
func (a *App) AttachFiles(paths []string) {
	if len(paths) == 0 || a.options.SDKProtocol {
		return
	}
	if !a.model.Attachments.Supported {
		a.model.Attachments.Error = attachment.ErrUnsupported.Error()
		return
	}
	if len(paths) > maxDroppedPaths {
		paths = paths[:maxDroppedPaths]
	}
	limits := attachment.Limits{}
	if a.attachmentLimits != nil {
		limits = *a.attachmentLimits
	}
	sessionID := a.model.SessionID
	go func() {
		drafts, references, firstErr := readAttachments(paths, limits)
		a.post(func() {
			if a.shuttingDown.Load() || a.model.SessionID != sessionID {
				return
			}
			for _, draft := range drafts {
				a.attachImage(draft.Name, draft.MediaType, draft.Data)
			}
			if len(references) > 0 {
				insertReferences(a, references)
			}
			if firstErr != nil {
				a.model.Attachments.Error = firstErr.Error()
			}
		})
	}()
}

// pickAttachment opens the native image picker on a goroutine, as MyGo's dialog
// blocks until the user decides.
func (a *App) pickAttachment() {
	if a.options.SDKProtocol {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:    "Attach image",
			Multiple: true,
			Filters:  []mygo.FileFilter{{Name: "Images", Extensions: []string{"png", "jpg", "jpeg", "webp", "gif"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		a.post(func() { a.AttachFiles(paths) })
	}()
}

// readAttachments reads bounded paths off the main thread. It returns image
// drafts and text references for everything else; the first failure is reported
// once without dropping the files it could read.
func readAttachments(paths []string, limits attachment.Limits) ([]attachment.Draft, []string, error) {
	var drafts []attachment.Draft
	var references []string
	var firstErr error
	for _, path := range paths {
		draft, isImage, err := readAttachmentPath(path, limits)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if isImage {
			drafts = append(drafts, draft)
			continue
		}
		if path != "" {
			references = append(references, "@"+filepath.Clean(path))
		}
	}
	return drafts, references, firstErr
}

// readAttachmentPath sniffs one regular file: a supported raster becomes an
// image draft within the advertised per-image budget, anything else a path
// reference. Directories and unreadable paths are skipped.
func readAttachmentPath(path string, limits attachment.Limits) (attachment.Draft, bool, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return attachment.Draft{}, false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return attachment.Draft{}, false, nil
	}
	header := make([]byte, 512)
	n, readErr := io.ReadFull(file, header)
	file.Close()
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		return attachment.Draft{}, false, nil
	}
	mediaType := imageMediaType(header[:n])
	if mediaType == "" {
		return attachment.Draft{}, false, nil
	}
	if info.Size() > int64(limits.MaxImageBytes) {
		return attachment.Draft{}, true, attachment.ErrTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return attachment.Draft{}, false, nil
	}
	draft := attachment.Draft{Name: attachment.DisplayName(filepath.Base(path)), MediaType: mediaType, Data: data}
	if width, height, ok := imageDimensions(data); ok {
		draft.Width, draft.Height = width, height
	}
	return draft, true, nil
}

// imageMediaType maps sniffed bytes to the raster types the host accepts.
func imageMediaType(header []byte) string {
	switch http.DetectContentType(header) {
	case attachment.MediaPNG:
		return attachment.MediaPNG
	case attachment.MediaJPEG:
		return attachment.MediaJPEG
	case attachment.MediaGIF:
		return attachment.MediaGIF
	case attachment.MediaWebP:
		return attachment.MediaWebP
	default:
		return ""
	}
}

// imageDimensions reads only the raster header; unknown formats omit dimensions
// and let the host validate them.
func imageDimensions(data []byte) (int, int, bool) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

// insertReferences appends @path references to the draft without disturbing
// what the user already typed.
func insertReferences(a *App, references []string) {
	addition := strings.Join(references, " ")
	if a.model.Draft != "" && !strings.HasSuffix(a.model.Draft, " ") && !strings.HasSuffix(a.model.Draft, "\n") {
		addition = " " + addition
	}
	a.model.Draft += addition
	if a.model.DraftChanged != nil {
		a.model.DraftChanged()
	}
}

// promptBlocks builds the ordered content blocks for one send: the visible
// text first, then staged images as canonical base64. Validation runs before
// any request, so a refused batch never clears the draft.
func (a *App) promptBlocks(text string) ([]map[string]any, error) {
	pending := a.model.Attachments.Pending
	if len(pending) == 0 {
		return []map[string]any{{"type": "text", "text": text}}, nil
	}
	if a.options.SDKProtocol {
		return nil, attachment.ErrUnsupported
	}
	if a.attachmentLimits == nil {
		return nil, attachment.ErrUnsupported
	}
	if err := a.attachmentLimits.Validate(pending); err != nil {
		return nil, err
	}
	blocks := make([]map[string]any, 0, len(pending)+1)
	if text != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
	}
	for _, draft := range pending {
		block := map[string]any{"type": "image", "mimeType": draft.MediaType, "data": base64.StdEncoding.EncodeToString(draft.Data)}
		if name := attachment.DisplayName(draft.Name); name != "" {
			block["name"] = name
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}
