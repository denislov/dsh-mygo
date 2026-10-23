package attachment

import (
	"errors"
	"strings"
	"testing"
)

func validLimits() Limits {
	return Limits{
		MediaTypes:           []string{MediaPNG, MediaJPEG, MediaWebP, MediaGIF},
		MaxBase64Bytes:       8 * 1024 * 1024,
		MaxImageBytes:        6 * 1024 * 1024,
		MaxImagesPerMessage:  8,
		MaxMessageImageBytes: 6 * 1024 * 1024,
		MaxImagePixels:       64000000,
		MaxImageDimension:    8192,
	}
}

func TestValidatePolicyRejectsImplausibleLimits(t *testing.T) {
	if err := validLimits().ValidatePolicy(); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	for name, mutate := range map[string]func(*Limits){
		"no media types":   func(l *Limits) { l.MediaTypes = nil },
		"unknown media":    func(l *Limits) { l.MediaTypes = []string{"image/bmp"} },
		"duplicate media":  func(l *Limits) { l.MediaTypes = []string{MediaPNG, MediaPNG} },
		"zero image size":  func(l *Limits) { l.MaxImageBytes = 0 },
		"image over cap":   func(l *Limits) { l.MaxImageBytes = 21 * 1024 * 1024 },
		"base64 over cap":  func(l *Limits) { l.MaxBase64Bytes = 65 * 1024 * 1024 },
		"too many images":  func(l *Limits) { l.MaxImagesPerMessage = 33 },
		"total over cap":   func(l *Limits) { l.MaxMessageImageBytes = 257 * 1024 * 1024 },
		"pixels over cap":  func(l *Limits) { l.MaxImagePixels = 1 << 31 },
		"dimension over c": func(l *Limits) { l.MaxImageDimension = 1 << 16 },
	} {
		limits := validLimits()
		mutate(&limits)
		if err := limits.ValidatePolicy(); err == nil {
			t.Fatalf("%s: invalid policy accepted", name)
		}
	}
}

func TestValidateEnforcesTheBatchBudgets(t *testing.T) {
	limits := validLimits()
	if err := limits.Validate(nil); err != nil {
		t.Fatalf("empty batch rejected: %v", err)
	}
	png := Draft{MediaType: MediaPNG, Data: []byte("png")}
	if err := limits.Validate([]Draft{png, png}); err != nil {
		t.Fatalf("valid batch rejected: %v", err)
	}
	cases := []struct {
		name   string
		drafts []Draft
		want   error
	}{
		{"too many", make([]Draft, 9), ErrTooMany},
		{"unknown type", []Draft{{MediaType: "image/bmp", Data: []byte("x")}}, ErrUnsupportedType},
		{"empty", []Draft{{MediaType: MediaPNG}}, ErrEmpty},
		{"per image", []Draft{{MediaType: MediaPNG, Data: make([]byte, 7*1024*1024)}}, ErrTooLarge},
		{"dimension", []Draft{{MediaType: MediaPNG, Data: []byte("x"), Width: 9000, Height: 1}}, ErrTooLarge},
		{"pixels", []Draft{{MediaType: MediaPNG, Data: []byte("x"), Width: 8001, Height: 8000}}, ErrTooLarge},
		{"aggregate", []Draft{{MediaType: MediaPNG, Data: make([]byte, 4*1024*1024)}, {MediaType: MediaPNG, Data: make([]byte, 3*1024*1024)}}, ErrTooLarge},
	}
	for _, test := range cases {
		if err := limits.Validate(test.drafts); !errors.Is(err, test.want) {
			t.Fatalf("%s: error = %v, want %v", test.name, err, test.want)
		}
	}
}

func TestValidateUsesTheBase64BudgetForSmallLimits(t *testing.T) {
	limits := validLimits()
	limits.MaxBase64Bytes = 4
	limits.MaxImageBytes = 10
	if err := limits.Validate([]Draft{{MediaType: MediaPNG, Data: make([]byte, 4)}}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v", err)
	}
}

func TestAcceptsMatchesTheAdvertisedTypes(t *testing.T) {
	limits := validLimits()
	limits.MediaTypes = []string{MediaGIF}
	if !limits.Accepts(MediaGIF) || limits.Accepts(MediaPNG) {
		t.Fatal("media type matching is wrong")
	}
}

func TestDisplayNameDropsUnsafeLabels(t *testing.T) {
	for _, valid := range []string{"shot.png", "截图 1.png", "a.b.c"} {
		if DisplayName(valid) != valid {
			t.Fatalf("%q was rejected", valid)
		}
	}
	for _, invalid := range []string{"", "   ", "../secret.png", `C:\shots\a.png`, "a/b.png", strings.Repeat("x", 129), "bad\x00name", "tab\tname"} {
		if DisplayName(invalid) != "" {
			t.Fatalf("%q was accepted", invalid)
		}
	}
}
