package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"dsh-mygo/internal/preferences"
)

type pendingDraft struct{ sessionID, text string }
type localEditingState struct {
	store       *preferences.Store
	writer      *preferences.Writer
	ready       bool
	loadError   error
	revision    uint64
	lastSession string
	pending     map[uint64]pendingDraft
	retrying    bool
}

func (a *App) configureLocalState() {
	if (a.options.PreferencesPath == "" && !a.options.PreferencesUnavailable) || a.protocol.enabled() {
		return
	}
	local := &localEditingState{store: preferences.NewStore(a.options.PreferencesPath), pending: make(map[uint64]pendingDraft)}
	local.writer = preferences.NewWriter(local.store, 400*time.Millisecond, func(revision uint64, err error) {
		if a.shuttingDown.Load() {
			return
		}
		a.post(func() {
			if revision != local.revision || a.shuttingDown.Load() {
				return
			}
			if err != nil {
				a.model.LocalState.Error = err.Error()
			} else {
				a.model.LocalState.Error = ""
			}
		})
	})
	a.local = local
	a.model.Prefs.Ready = false
	a.model.LocalState.Retry = a.SaveLocalState
	a.model.LocalState.QuitWithoutSaving = func() { a.RequestQuit(true) }
	if a.options.PreferencesUnavailable {
		a.model.Prefs.Ready = true
		local.ready = true
		local.loadError = preferences.ErrUnavailable
		a.model.LocalState.Error = preferences.ErrUnavailable.Error()
	}
}

// Called by Start's worker before the Host is created. The UI only receives
// immutable loaded data; neither the model nor a UI context enters disk I/O.
func (a *App) loadLocalState() string {
	if a.local == nil {
		return ""
	}
	if a.options.PreferencesUnavailable {
		return ""
	}
	state, notice, err := a.local.store.Load()
	if err == nil {
		a.hostLocale.Store(state.UI.Locale)
	}
	a.post(func() {
		if a.shuttingDown.Load() {
			return
		}
		a.local.ready = true
		a.model.Prefs.Ready = true
		a.local.loadError = err
		if err != nil {
			a.model.LocalState.Error = err.Error()
			return
		}
		a.drafts = state.Drafts
		a.acceptUIPreferences(state.UI)
		a.model.Prefs.Persisted = true
		a.local.lastSession = state.LastSessionID
		a.model.LocalState.Notice = notice
	})
	if err != nil {
		return ""
	}
	return state.LastSessionID
}

func (a *App) localSnapshot() preferences.Snapshot {
	state := preferences.Empty()
	state.UI = a.model.Prefs.Value
	for id, text := range a.drafts {
		if validSessionID(id) && text != "" {
			state.Drafts[id] = text
		}
	}
	if a.local != nil {
		state.LastSessionID = a.local.lastSession
	}
	if validSessionID(a.sessionID) {
		state.LastSessionID = a.sessionID
		if a.model.Draft == "" {
			delete(state.Drafts, a.sessionID)
		} else {
			state.Drafts[a.sessionID] = a.model.Draft
		}
	}
	// Until the Host confirms delivery, retain submitted text as recoverable
	// editing state. It is never automatically re-sent on restart.
	if a.local != nil {
		ids := make([]uint64, 0, len(a.local.pending))
		for id := range a.local.pending {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
		for _, id := range ids {
			pending := a.local.pending[id]
			text := state.Drafts[pending.sessionID]
			if text != "" && text != pending.text {
				text = pending.text + "\n\n" + text
			} else {
				text = pending.text
			}
			state.Drafts[pending.sessionID] = text
		}
	}
	return state
}

// DraftChanged runs after the native input binding has updated Model.Draft.
func (a *App) DraftChanged() { a.persistLocalState() }
func (a *App) persistLocalState() {
	if a.local == nil || !a.local.ready || a.local.loadError != nil || a.shuttingDown.Load() {
		return
	}
	a.local.revision = a.local.writer.Submit(a.localSnapshot())
}
func (a *App) SaveLocalState() {
	if a.model.LocalState.Closing {
		return
	}
	a.persistLocalState()
	if a.local == nil {
		return
	}
	if a.local.loadError != nil {
		a.retryLocalLoad()
		return
	}
	writer := a.local.writer
	revision := a.local.revision
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := writer.Flush(ctx)
		a.post(func() {
			if revision == a.local.revision && !a.shuttingDown.Load() {
				if err != nil {
					a.model.LocalState.Error = err.Error()
				} else {
					a.model.LocalState.Error = ""
				}
			}
		})
	}()
}

func (a *App) retryLocalLoad() {
	if a.options.PreferencesUnavailable {
		a.model.LocalState.Error = preferences.ErrUnavailable.Error()
		return
	}
	if a.local.retrying {
		return
	}
	a.local.retrying = true
	go func() {
		state, notice, err := a.local.store.Load()
		a.post(func() {
			a.local.retrying = false
			if a.shuttingDown.Load() {
				return
			}
			if err != nil {
				a.model.LocalState.Error = err.Error()
				return
			}
			if a.drafts == nil {
				a.drafts = make(map[string]string)
			}
			for id, text := range state.Drafts {
				if _, exists := a.drafts[id]; !exists && id != a.sessionID {
					a.drafts[id] = text
				}
			}
			a.local.loadError = nil
			if !a.model.Prefs.Dirty {
				a.acceptUIPreferences(state.UI)
			}
			a.model.Prefs.Persisted = true
			a.model.LocalState.Notice = notice
			a.SaveLocalState()
		})
	}()
}

func (a *App) offerPreviousDraft(id string) {
	if a.local == nil || a.drafts[id] == "" {
		return
	}
	a.model.LocalState.RecoverDraft = func() {
		if a.model.LocalState.Closing {
			return
		}
		text := a.drafts[id]
		if a.model.Draft == "" {
			a.model.Draft = text
		} else if a.model.Draft != text {
			a.model.Draft = text + "\n\n" + a.model.Draft
		}
		a.model.LocalState.Notice = msgLocalPreviousDraft
		a.model.LocalState.RecoverDraft = nil
		a.persistLocalState()
	}
}
func (a *App) rememberSubmittedDraft(id uint64, text string) {
	if a.local == nil {
		return
	}
	a.local.pending[id] = pendingDraft{sessionID: a.sessionID, text: text}
	a.persistLocalState()
}
func (a *App) settleSubmittedDraft(id uint64, err error) {
	if a.local == nil {
		return
	}
	pending, exists := a.local.pending[id]
	if !exists {
		return
	}
	delete(a.local.pending, id)
	if err != nil {
		text := a.drafts[pending.sessionID]
		if pending.sessionID == a.sessionID {
			text = a.model.Draft
		}
		if text == "" {
			text = pending.text
		} else if text != pending.text {
			text = pending.text + "\n\n" + text
		}
		if a.drafts == nil {
			a.drafts = make(map[string]string)
		}
		a.drafts[pending.sessionID] = text
		if pending.sessionID == a.sessionID {
			a.model.Draft = text
		}
	}
	a.persistLocalState()
}
func (a *App) ReadyToQuit() bool { return a.quit.isReady() }

// RequestQuit captures final UI edits, then flushes and stops in a worker.
// A failed save leaves the window open; discarding requires an explicit action.
func (a *App) RequestQuit(discard bool) {
	if a.shuttingDown.Load() || !a.quit.begin() {
		return
	}
	a.persistLocalState()
	a.model.LocalState.Closing = true
	var loadErr error
	if a.local != nil {
		loadErr = a.local.loadError
	}
	go func() {
		var err error
		if !discard && a.local != nil {
			if loadErr != nil {
				err = loadErr
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err = a.local.writer.Flush(ctx)
				cancel()
			}
		}
		if err != nil {
			a.post(func() {
				a.quit.fail()
				a.model.LocalState.Closing = false
				a.model.LocalState.Error = err.Error()
			})
			return
		}
		if a.local != nil && discard {
			a.local.writer.Stop()
		}
		a.Shutdown(5 * time.Second)
		a.options.Post(func() {
			a.quit.complete()
			if a.options.Quit != nil {
				a.options.Quit()
			}
		})
	}()
}

func originalSubmittedText(draft, text string) string {
	if strings.TrimSpace(draft) == strings.TrimSpace(text) {
		return draft
	}
	return text
}

func localRestoreNotice() string {
	return msgLocalRestoreFailed
}
