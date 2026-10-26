package app

import (
	"context"
	"time"
)

// operationSlot is the shared version-token and cancellation primitive for
// main-thread operations. Domains still own their own state, identity checks
// and persistence semantics; invalidating a slot only rejects older results and
// cancels their context.
type operationSlot struct {
	id     uint64
	cancel context.CancelFunc
}

// begin invalidates the previous operation, then creates a bounded context and
// returns its identity plus the cancel function captured by the worker.
func (s *operationSlot) begin(timeout time.Duration) (context.Context, uint64, context.CancelFunc) {
	s.invalidate()
	s.id++
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	s.cancel = cancel
	return ctx, s.id, cancel
}

// invalidate rejects in-flight results and cancels their context.
func (s *operationSlot) invalidate() {
	s.id++
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// valid reports whether id still names the active operation.
func (s *operationSlot) valid(id uint64) bool { return id == s.id }
