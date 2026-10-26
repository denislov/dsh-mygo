package app

import (
	"context"
	"testing"
)

func TestHistoryComponentHostChangeCancelsCutAndResetsCapabilities(t *testing.T) {
	var history historyComponent
	ctx, cancel := context.WithCancel(context.Background())
	job := history.startResync(ctx, cancel, "s1")
	history.statusRevision = 4
	history.hostStatus = "running"
	history.canStreamBaseline, history.canHistoryWindow, history.canRelease = true, true, true
	history.startResync(ctx, func() {}, "s2")
	select {
	case <-ctx.Done():
	default:
		t.Fatal("starting a new resync did not cancel the previous cut")
	}
	if history.resync == nil || history.resync.id != "s2" || job == history.resync {
		t.Fatal("new resync job was not installed")
	}
	history.hostChanged()
	if history.resync != nil || history.statusRevision != 0 || history.hostStatus != "" || history.canStreamBaseline || history.canHistoryWindow || history.canRelease {
		t.Fatal("host change kept history cut or capabilities")
	}
}

func TestHistoryComponentSnapshotNeverLowersRevision(t *testing.T) {
	var history historyComponent
	history.commitSnapshot("running", 5)
	history.commitSnapshot("idle", 4)
	if history.statusRevision != 5 || history.hostStatus != "idle" {
		t.Fatalf("snapshot = %+v", history)
	}
	history.commitSnapshot("running", 6)
	if history.statusRevision != 6 || history.hostStatus != "running" {
		t.Fatalf("snapshot = %+v", history)
	}
}
