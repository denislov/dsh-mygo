package app

import (
	"errors"
	"strings"
	"testing"

	"dsh-mygo/internal/view"
)

func TestPreflightFailureShowsHostError(t *testing.T) {
	application := New(Options{PreflightErr: errors.New("profile seed failed")})
	application.Start()

	model := application.Model()
	if model.Phase != view.PhaseFailed {
		t.Fatalf("phase = %v, want failed", model.Phase)
	}
	if !strings.Contains(model.Error, "profile") {
		t.Fatalf("error = %q", model.Error)
	}
	if !strings.Contains(model.ErrorDetail, "profile seed failed") {
		t.Fatalf("detail = %q", model.ErrorDetail)
	}
}
