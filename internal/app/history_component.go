package app

import "context"

// historyComponent owns the history/resync cut, status revision and negotiated
// history capabilities. The view model is only a published projection; the
// component never reads it back as truth.
type historyComponent struct {
	resync            *resyncState
	statusRevision    uint64
	hostStatus        string
	canStreamBaseline bool
	canHistoryWindow  bool
	canRelease        bool
}

// startResync stores the fixed-cut job after cancelling any previous one.
func (h *historyComponent) startResync(ctx context.Context, cancel context.CancelFunc, id string) *resyncState {
	h.cancelResync()
	job := &resyncState{ctx: ctx, cancel: cancel, id: id}
	h.resync = job
	return job
}

func (h *historyComponent) cancelResync() {
	if h.resync != nil {
		h.resync.cancel()
		h.resync = nil
	}
}

// commitSnapshot publishes a validated resync/history cut. It never lowers the
// status revision of the same agent.
func (h *historyComponent) commitSnapshot(status string, revision uint64) {
	h.hostStatus = status
	if revision >= h.statusRevision {
		h.statusRevision = revision
	}
}

// hostChanged drops history cut and negotiated capabilities for a new Host
// generation. A new Agent rebuilds its stream revision; same-generation resync
// keeps the high-water mark in its snapshot.
func (h *historyComponent) hostChanged() {
	h.cancelResync()
	h.statusRevision = 0
	h.hostStatus = ""
	h.canStreamBaseline = false
	h.canHistoryWindow = false
	h.canRelease = false
}
