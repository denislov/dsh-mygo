package preferences

import (
	"context"
	"sync"
	"time"
)

// Writer coalesces immutable UI snapshots. One writer serializes disk work;
// stale timers never overwrite a newer submission, including after a flush.
type Writer struct {
	mu       sync.Mutex
	disk     sync.Mutex
	store    *Store
	delay    time.Duration
	pending  Snapshot
	revision uint64
	saved    uint64
	timer    *time.Timer
	stopped  bool
	result   func(uint64, error)
}

func NewWriter(store *Store, delay time.Duration, result func(uint64, error)) *Writer {
	return &Writer{store: store, delay: delay, result: result}
}
func (writer *Writer) Submit(state Snapshot) uint64 {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.stopped {
		return writer.revision
	}
	writer.revision++
	revision := writer.revision
	writer.pending = Clone(state)
	if writer.timer != nil {
		writer.timer.Stop()
	}
	writer.timer = time.AfterFunc(writer.delay, func() { writer.write(revision) })
	return revision
}
func (writer *Writer) write(revision uint64) error {
	writer.disk.Lock()
	defer writer.disk.Unlock()
	writer.mu.Lock()
	if revision != writer.revision || revision <= writer.saved || writer.stopped {
		writer.mu.Unlock()
		return nil
	}
	state := writer.pending
	writer.mu.Unlock()
	err := writer.store.Save(state)
	writer.mu.Lock()
	if err == nil {
		writer.saved = revision
	}
	writer.mu.Unlock()
	if writer.result != nil {
		writer.result(revision, err)
	}
	return err
}
func (writer *Writer) Flush(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		for {
			if ctx.Err() != nil {
				done <- ErrUnavailable
				return
			}
			writer.mu.Lock()
			if writer.timer != nil {
				writer.timer.Stop()
			}
			revision := writer.revision
			clean := revision <= writer.saved || writer.stopped
			writer.mu.Unlock()
			if clean {
				done <- nil
				return
			}
			if err := writer.write(revision); err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ErrUnavailable
	}
}
func (writer *Writer) Stop() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.stopped = true
	if writer.timer != nil {
		writer.timer.Stop()
	}
}
