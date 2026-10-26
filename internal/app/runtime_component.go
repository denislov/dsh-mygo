package app

import (
	"context"
	"sync"
)

// runtimeSwitchStage makes the candidate transaction explicit. It is diagnostic
// and test-observable; the committed device selection remains the only success
// marker.
type runtimeSwitchStage string

const (
	runtimeSwitchPreparing    runtimeSwitchStage = "preparing"
	runtimeSwitchStopping     runtimeSwitchStage = "stopping"
	runtimeSwitchInitializing runtimeSwitchStage = "initializing"
	runtimeSwitchCommitting   runtimeSwitchStage = "committing"
	runtimeSwitchRollingBack  runtimeSwitchStage = "rolling-back"
)

// runtimeComponent owns runtime-install/check generations and the one pending
// runtime switch transaction. Download/install capabilities stay in
// runtimes.Manager.
type runtimeComponent struct {
	runtimeGeneration uint64
	runtimeCancel     context.CancelFunc
	switchMu          sync.Mutex
	switchRuntime     *runtimeSwitch
}

func (r *runtimeComponent) beginOperation() (context.Context, uint64) {
	if r.runtimeCancel != nil {
		r.runtimeCancel()
	}
	r.runtimeGeneration++
	generation := r.runtimeGeneration
	ctx, cancel := context.WithCancel(context.Background())
	r.runtimeCancel = cancel
	return ctx, generation
}

func (r *runtimeComponent) invalidate() {
	r.runtimeGeneration++
	if r.runtimeCancel != nil {
		r.runtimeCancel()
		r.runtimeCancel = nil
	}
}

func (r *runtimeComponent) current(generation uint64) bool {
	return generation == r.runtimeGeneration
}

func (r *runtimeComponent) cancel() {
	if r.runtimeCancel != nil {
		r.runtimeCancel()
		r.runtimeCancel = nil
	}
}

func (r *runtimeComponent) setSwitch(switching *runtimeSwitch) {
	r.switchMu.Lock()
	r.switchRuntime = switching
	r.switchMu.Unlock()
}

func (r *runtimeComponent) switchSnapshot() *runtimeSwitch {
	r.switchMu.Lock()
	defer r.switchMu.Unlock()
	return r.switchRuntime
}

func (r *runtimeComponent) clearSwitch() *runtimeSwitch {
	r.switchMu.Lock()
	switching := r.switchRuntime
	r.switchRuntime = nil
	r.switchMu.Unlock()
	return switching
}

func (r *runtimeComponent) markStage(stage runtimeSwitchStage) {
	r.switchMu.Lock()
	if r.switchRuntime != nil {
		r.switchRuntime.stage = stage
	}
	r.switchMu.Unlock()
}
