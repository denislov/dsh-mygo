package app

import "testing"

func TestTurnTrackerClaimCancelAndResetSequences(t *testing.T) {
	delivery := make(chan error, 1)
	var turn turnTracker
	id := turn.submit(delivery)
	if id != 1 || !turn.awaiting || turn.activity || turn.cancelIdle || turn.deliveryBarrier() != delivery {
		t.Fatalf("submitted = %+v", turn)
	}
	if turn.acknowledgeIdle() {
		t.Fatal("idle before claim ended the turn")
	}
	if !turn.awaiting || turn.activity {
		t.Fatalf("early idle changed state = %+v", turn)
	}
	turn.noteActivity()
	if !turn.activity {
		t.Fatal("activity was not recorded")
	}
	turn.beginCancel()
	turn.noteIdleWhileCancelling()
	if !turn.cancelIdle {
		t.Fatal("cancel idle was not recorded")
	}
	turn.noteRunning()
	if turn.cancelIdle {
		t.Fatal("running status kept the cancel idle")
	}
	turn.noteIdleWhileCancelling()
	turn.finishTurn()
	if turn.awaiting || turn.activity || turn.cancelIdle {
		t.Fatalf("finished = %+v", turn)
	}

	next := turn.submit(nil)
	if next != 2 || !turn.current(next) || turn.current(id) {
		t.Fatalf("prompt identity = %d / current %d", next, turn.prompt)
	}
	turn.noteActivity()
	if !turn.acknowledgeIdle() || turn.awaiting || turn.activity {
		t.Fatalf("acknowledged idle = %+v", turn)
	}
	turn.noteActivity()
	turn.cancelTimedOut()
	if turn.awaiting || !turn.activity {
		t.Fatalf("cancel timeout = %+v", turn)
	}
	turn.restore(true)
	if turn.awaiting || !turn.activity {
		t.Fatalf("restored running = %+v", turn)
	}
	turn.restore(false)
	if turn.awaiting || turn.activity {
		t.Fatalf("restored idle = %+v", turn)
	}
	turn.submit(nil)
	before := turn.prompt
	turn.reset()
	if turn.prompt != before+1 || turn.awaiting || turn.activity || turn.cancelIdle || turn.deliveryBarrier() != nil || turn.current(before) {
		t.Fatalf("reset = %+v", turn)
	}
	turn.submit(nil)
	turn.noteActivity()
	turn.clear()
	if turn.awaiting || turn.activity {
		t.Fatalf("cleared = %+v", turn)
	}
}

func TestTurnTrackerIdleWithoutOwnedPromptStillProceeds(t *testing.T) {
	var turn turnTracker
	turn.noteActivity()
	if !turn.acknowledgeIdle() {
		t.Fatal("idle without an owned prompt was blocked")
	}
	if !turn.activity {
		t.Fatal("idle without an owned prompt changed activity")
	}
}
