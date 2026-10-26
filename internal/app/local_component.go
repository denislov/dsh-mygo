package app

// localComponent owns device-local editing state: session drafts and the
// preferences-backed writer. It never reads the Host log and never writes a
// second copy of session history.
type localComponent struct {
	drafts map[string]string
	local  *localEditingState
	quit   quitCoordinator
}

func (l *localComponent) setDraft(id, text string) {
	if l.drafts == nil {
		l.drafts = make(map[string]string)
	}
	l.drafts[id] = text
}

func (l *localComponent) draft(id string) string { return l.drafts[id] }

// quitCoordinator makes the exit request single-flight: a failed save returns
// to editable state, while a successful stop can be observed once.
type quitCoordinator struct {
	closing bool
	ready   bool
}

func (q *quitCoordinator) begin() bool {
	if q.closing || q.ready {
		return false
	}
	q.closing = true
	return true
}

func (q *quitCoordinator) fail() { q.closing = false }

func (q *quitCoordinator) complete() {
	q.closing = false
	q.ready = true
}

func (q *quitCoordinator) isReady() bool { return q.ready }
