package app

import (
	"errors"
	"strings"
	"testing"

	"dsh-mygo/internal/view"
)

func TestPreflightFailureShowsHostError(t *testing.T) {
	application := New(Options{
		PreflightErr:     errors.New("profile seed failed"),
		PreflightSummary: "Could not prepare the profile",
	})
	application.Start()

	model := application.Model()
	if model.Phase != view.PhaseFailed {
		t.Fatalf("phase = %v, want failed", model.Phase)
	}
	if model.Error != "Could not prepare the profile" {
		t.Fatalf("error = %q", model.Error)
	}
	if !strings.Contains(model.ErrorDetail, "profile seed failed") {
		t.Fatalf("detail = %q", model.ErrorDetail)
	}
}

func TestPreflightFailureUsesDefaultSummary(t *testing.T) {
	application := New(Options{PreflightErr: errors.New("host entry missing")})
	application.Start()

	model := application.Model()
	if model.Phase != view.PhaseFailed || !strings.Contains(model.Error, "host") {
		t.Fatalf("phase = %v, error = %q", model.Phase, model.Error)
	}
}
