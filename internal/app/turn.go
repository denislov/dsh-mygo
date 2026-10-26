package app

// turnTracker owns the prompt/cancel transition state. It performs no UI or
// I/O work and must only be called on the main thread. App reads the result and
// executes the corresponding view, stream and queue side effects.
type turnTracker struct {
	awaiting   bool
	activity   bool
	cancelIdle bool
	prompt     uint64
	delivery   <-chan error
}

// submit starts a new prompt and returns its identity and delivery barrier.
func (t *turnTracker) submit(delivery <-chan error) uint64 {
	t.prompt++
	t.awaiting = true
	t.activity = false
	t.cancelIdle = false
	t.delivery = delivery
	return t.prompt
}

// noteActivity records that the Host claimed the current prompt. A persistent
// event is the only signal that a queued prompt may end on idle.
func (t *turnTracker) noteActivity() { t.activity = true }

// acknowledgeIdle reports whether an idle status may end the current turn. An
// idle before any activity is the claim race and is ignored.
func (t *turnTracker) acknowledgeIdle() bool {
	if t.awaiting && !t.activity {
		return false
	}
	if t.awaiting {
		t.awaiting = false
		t.activity = false
	}
	return true
}

// noteRunning clears an idle acknowledgement that predates a new running
// status.
func (t *turnTracker) noteRunning() { t.cancelIdle = false }

// beginCancel marks a cancel request as outstanding.
func (t *turnTracker) beginCancel() { t.cancelIdle = false }

// noteIdleWhileCancelling records that the Host became idle before the cancel
// response arrived. The cancel acknowledgement still owns the final phase.
func (t *turnTracker) noteIdleWhileCancelling() { t.cancelIdle = true }

// current reports whether id still names the active prompt.
func (t *turnTracker) current(id uint64) bool { return t.prompt == id }

func (t *turnTracker) deliveryBarrier() <-chan error { return t.delivery }

// finishTurn clears the prompt after an accepted cancel or a completed turn.
func (t *turnTracker) finishTurn() {
	t.awaiting = false
	t.activity = false
	t.cancelIdle = false
}

// cancelTimedOut keeps the turn running for a late idle but removes the
// pending-claim barrier.
func (t *turnTracker) cancelTimedOut() { t.awaiting = false }

// deliveryFailed ends the local turn after the prompt RPC failed.
func (t *turnTracker) deliveryFailed() {
	t.awaiting = false
	t.activity = false
}

// restore commits the activity state from a resync snapshot.
func (t *turnTracker) restore(running bool) {
	t.awaiting = false
	t.activity = running
}

// reset invalidates the current prompt and delivery barrier for a new session
// or Host generation.
func (t *turnTracker) reset() {
	t.prompt++
	t.awaiting = false
	t.activity = false
	t.cancelIdle = false
	t.delivery = nil
}

// clear ends the turn state when the Host fails; pending cancel bookkeeping is
// owned by the caller's phase reset.
func (t *turnTracker) clear() {
	t.awaiting = false
	t.activity = false
}
