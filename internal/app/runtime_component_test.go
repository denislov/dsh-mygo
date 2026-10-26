package app

import (
	"testing"

	"dsh-mygo/internal/runtimes"
)

func TestRuntimeComponentInvalidatesOperationsAndTracksSwitchStage(t *testing.T) {
	var runtime runtimeComponent
	ctx, first := runtime.beginOperation()
	if !runtime.current(first) {
		t.Fatal("runtime operation was not current")
	}
	_, second := runtime.beginOperation()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("new runtime operation did not cancel the previous one")
	}
	if runtime.current(first) || !runtime.current(second) {
		t.Fatal("runtime generation was not replaced")
	}
	switching := &runtimeSwitch{target: runtimes.Runtime{ID: "target"}, previous: runtimes.Runtime{ID: "previous"}, stage: runtimeSwitchPreparing}
	runtime.setSwitch(switching)
	runtime.markStage(runtimeSwitchInitializing)
	if got := runtime.switchSnapshot(); got == nil || got.stage != runtimeSwitchInitializing {
		t.Fatalf("switch = %+v", got)
	}
	if runtime.clearSwitch() != switching || runtime.switchSnapshot() != nil {
		t.Fatal("switch transaction was not cleared")
	}
	runtime.invalidate()
	if runtime.current(second) {
		t.Fatal("invalidation kept a runtime operation")
	}
}
