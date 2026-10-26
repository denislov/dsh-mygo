package app

import (
	"context"
	"sync"

	"github.com/egoist/mygo/plugins/terminal"
)

// workspaceComponent owns native project resources independently of the Host.
// A project generation invalidates reads and destroys terminals; the per-panel
// request tokens only replace that panel's latest read. Hiding a panel touches
// neither.
type workspaceComponent struct {
	project   workspaceProject
	preview   workspaceRequest
	diff      workspaceRequest
	terminal  workspaceRequest
	terminals workspaceTerminals
}

type workspaceProject struct {
	generation uint64 // main thread
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
}

type workspaceRequest struct {
	generation, request uint64 // main thread
}

type workspaceTerminals struct {
	mu      sync.Mutex
	entries map[*terminal.Terminal]bool
}

// switchProject invalidates every read and destroys terminals from the old
// root. The caller clears the model's project-scoped display state.
func (w *workspaceComponent) switchProject() uint64 {
	w.project.generation++
	w.project.cancelReads()
	w.preview.generation = w.project.generation
	w.diff.generation = w.project.generation
	w.terminal.generation = w.project.generation
	w.terminals.closeAll(false)
	return w.project.generation
}

func (w *workspaceComponent) current(generation uint64) bool {
	return generation == w.project.generation
}

func (p *workspaceProject) readContext(shuttingDown bool) context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx == nil {
		p.ctx, p.cancel = context.WithCancel(context.Background())
	}
	if shuttingDown {
		p.cancel()
	}
	return p.ctx
}

func (p *workspaceProject) cancelReads() {
	p.mu.Lock()
	cancel := p.cancel
	p.ctx, p.cancel = nil, nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *workspaceRequest) begin(generation uint64) (uint64, uint64) {
	r.generation = generation
	r.request++
	return generation, r.request
}

func (r *workspaceRequest) current(generation, request uint64) bool {
	return r.generation == generation && r.request == request
}

// track records a terminal that finished starting. A terminal whose project
// changed while it started is closed instead.
func (t *workspaceTerminals) track(term *terminal.Terminal, accept bool) bool {
	if term == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !accept {
		go term.Close()
		return false
	}
	if t.entries == nil {
		t.entries = make(map[*terminal.Terminal]bool)
	}
	t.entries[term] = true
	return true
}

func (t *workspaceTerminals) discard(term *terminal.Terminal) {
	if term == nil {
		return
	}
	t.mu.Lock()
	delete(t.entries, term)
	t.mu.Unlock()
	go term.Close()
}

func (t *workspaceTerminals) closeAll(wait bool) {
	t.mu.Lock()
	terms := t.entries
	t.entries = nil
	t.mu.Unlock()
	if len(terms) == 0 {
		return
	}
	close := func() {
		for term := range terms {
			term.Close()
		}
	}
	if wait {
		close()
		return
	}
	go close()
}
