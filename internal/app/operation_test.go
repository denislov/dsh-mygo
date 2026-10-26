package app

import (
	"testing"
	"time"
)

func TestOperationSlotInvalidatesAndCancelsOnlyItsOwnGeneration(t *testing.T) {
	var slot operationSlot
	firstCtx, first, firstCancel := slot.begin(time.Second)
	if !slot.valid(first) {
		t.Fatal("first operation was not current")
	}
	secondCtx, second, secondCancel := slot.begin(time.Second)
	if first == second || slot.valid(first) || !slot.valid(second) {
		t.Fatalf("operation identities = %d/%d", first, second)
	}
	select {
	case <-firstCtx.Done():
	default:
		t.Fatal("superseded operation context was not cancelled")
	}
	if secondCtx.Err() != nil {
		t.Fatal("new operation context was cancelled")
	}
	slot.invalidate()
	select {
	case <-secondCtx.Done():
	default:
		t.Fatal("invalidated operation context was not cancelled")
	}
	if slot.valid(second) {
		t.Fatal("invalidated operation remained current")
	}
	firstCancel()
	secondCancel()
}
