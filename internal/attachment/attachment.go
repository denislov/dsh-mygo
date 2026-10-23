// Package attachment holds the prompt-attachment contract shared by the host
// handshake, the app's send gate and the native composer: the image policy a
// host advertises, the pending uploads a user staged, and the validation that
// runs before any bytes reach the channel.
package attachment

import (
	"errors"
	"fmt"
	"strings"
)

// Image media types the version-one attachment path accepts.
const (
	MediaPNG  = "image/png"
	MediaJPEG = "image/jpeg"
	MediaWebP = "image/webp"
	MediaGIF  = "image/gif"
)

// Limits is the image policy advertised in the initialize result. Every field
// is a positive bound; zero means the host sent no usable policy.
type Limits struct {
	MediaTypes           []string `json:"mediaTypes"`
	MaxBase64Bytes       int      `json:"maxBase64Bytes"`
	MaxImageBytes        int      `json:"maxImageBytes"`
	MaxImagesPerMessage  int      `json:"maxImagesPerMessage"`
	MaxMessageImageBytes int      `json:"maxMessageImageBytes"`
	MaxImagePixels       int      `json:"maxImagePixels"`
	MaxImageDimension    int      `json:"maxImageDimension"`
}

// Draft is one image the user staged for the next prompt. Data is the encoded
// image exactly as the host will store it; it never enters the session log or
// the local editing file.
type Draft struct {
	Name      string
	MediaType string
	Data      []byte
	Width     int
	Height    int
}

// User-facing failures. They are translation keys: the view localizes them and
// the app never wraps them with payload details.
var (
	ErrUnsupported     = errors.New("Image attachments are not supported by this host")
	ErrUnsupportedType = errors.New("Choose a PNG, JPEG, WebP or GIF image")
	ErrEmpty           = errors.New("The image attachment is empty")
	ErrTooMany         = errors.New("Too many images for one message")
	ErrTooLarge        = errors.New("The image attachment exceeds the supported size")
)

// knownTypes are the raster formats the attachment seam recognizes; a host may
// advertise a subset.
var knownTypes = map[string]bool{MediaPNG: true, MediaJPEG: true, MediaWebP: true, MediaGIF: true}

// ValidatePolicy checks the bounds a host advertised before the app trusts
// them. It rejects duplicate or unknown media types and implausible limits.
func (l Limits) ValidatePolicy() error {
	if len(l.MediaTypes) == 0 || len(l.MediaTypes) > len(knownTypes) {
		return fmt.Errorf("invalid attachment media types")
	}
	seen := make(map[string]bool, len(l.MediaTypes))
	for _, mediaType := range l.MediaTypes {
		if !knownTypes[mediaType] || seen[mediaType] {
			return fmt.Errorf("invalid attachment media type")
		}
		seen[mediaType] = true
	}
	bounds := []struct {
		value int
		limit int
	}{
		{l.MaxBase64Bytes, 64 * 1024 * 1024},
		{l.MaxImageBytes, 20 * 1024 * 1024},
		{l.MaxImagesPerMessage, 32},
		{l.MaxMessageImageBytes, 256 * 1024 * 1024},
		{l.MaxImagePixels, 1 << 30},
		{l.MaxImageDimension, 1 << 15},
	}
	for _, bound := range bounds {
		if bound.value <= 0 || bound.value > bound.limit {
			return fmt.Errorf("invalid attachment limits")
		}
	}
	return nil
}

// Accepts reports whether the host accepts this media type.
func (l Limits) Accepts(mediaType string) bool {
	for _, accepted := range l.MediaTypes {
		if accepted == mediaType {
			return true
		}
	}
	return false
}

// EncodeLength is the canonical base64 length of size bytes.
func EncodeLength(size int) int { return 4 * ((size + 2) / 3) }

// Validate checks one staged batch against the advertised policy. It runs
// before the app clears the draft, so a refused batch keeps the user's text and
// images intact.
func (l Limits) Validate(drafts []Draft) error {
	if len(drafts) == 0 {
		return nil
	}
	if len(drafts) > l.MaxImagesPerMessage {
		return ErrTooMany
	}
	total := 0
	for _, draft := range drafts {
		if draft.MediaType == "" || !l.Accepts(draft.MediaType) {
			return ErrUnsupportedType
		}
		if len(draft.Data) == 0 {
			return ErrEmpty
		}
		if len(draft.Data) > l.MaxImageBytes {
			return ErrTooLarge
		}
		if draft.Width > 0 && draft.Height > 0 {
			if draft.Width > l.MaxImageDimension || draft.Height > l.MaxImageDimension || draft.Width*draft.Height > l.MaxImagePixels {
				return ErrTooLarge
			}
		}
		total += len(draft.Data)
		if total > l.MaxMessageImageBytes {
			return ErrTooLarge
		}
	}
	if EncodeLength(total) > l.MaxBase64Bytes {
		return ErrTooLarge
	}
	return nil
}

// DisplayName strips a staged file name down to a safe display label: no path
// separators, no control characters, bounded length. An unusable name is
// dropped rather than mutated.
func DisplayName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 128 || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return ""
	}
	if strings.ContainsAny(name, `/\:`) {
		return ""
	}
	return name
}
