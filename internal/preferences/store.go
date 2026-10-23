// Package preferences owns device-local editing state, never Host history.
package preferences

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"
)

const MaxDraftBytes = 64 * 1024
const MaxTotalBytes = 1024 * 1024
const MaxDrafts = 128
const maxFileBytes = 8 * 1024 * 1024

var ErrBudget = errors.New("Draft storage limit reached (64 KiB per draft, 1 MiB total, 128 conversations)")
var ErrConflict = errors.New("Local drafts changed in another window; copy unsaved text before restarting")
var ErrVersion = errors.New("Local state belongs to a newer app version and will not be overwritten")
var ErrUnavailable = errors.New("Could not save local drafts; keep this window open and retry")
var errInvalid = errors.New("Invalid local state")

type Snapshot struct {
	Version       int               `json:"version"`
	LastSessionID string            `json:"lastSessionId"`
	Drafts        map[string]string `json:"drafts"`
}

func Empty() Snapshot { return Snapshot{Version: 1, Drafts: make(map[string]string)} }
func Clone(state Snapshot) Snapshot {
	result := state
	result.Drafts = make(map[string]string, len(state.Drafts))
	for id, text := range state.Drafts {
		result.Drafts[id] = text
	}
	return result
}
func validID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if c != '_' && c != '-' && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func Validate(state Snapshot) error {
	if state.Version != 1 || state.Drafts == nil || state.LastSessionID != "" && !validID(state.LastSessionID) {
		return errInvalid
	}
	if len(state.Drafts) > MaxDrafts {
		return ErrBudget
	}
	total := 0
	for id, text := range state.Drafts {
		if !validID(id) || !utf8.ValidString(text) {
			return errInvalid
		}
		total += len(text)
		if len(text) > MaxDraftBytes || total > MaxTotalBytes {
			return ErrBudget
		}
	}
	return nil
}
func decode(raw []byte) (Snapshot, error) {
	var state Snapshot
	var required struct {
		Version *int               `json:"version"`
		Last    *string            `json:"lastSessionId"`
		Drafts  map[string]*string `json:"drafts"`
	}
	if len(raw) > maxFileBytes || !utf8.Valid(raw) || json.Unmarshal(raw, &required) != nil || required.Version == nil {
		return state, errInvalid
	}
	if *required.Version > 1 {
		return state, ErrVersion
	}
	if required.Last == nil || required.Drafts == nil {
		return state, errInvalid
	}
	for _, value := range required.Drafts {
		if value == nil {
			return state, errInvalid
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil {
		return Snapshot{}, errInvalid
	}
	return state, Validate(state)
}
func StatePath(root, home, profile string) string {
	absolute, err := filepath.Abs(home)
	if err != nil {
		absolute = home
	}
	identity := filepath.Clean(absolute)
	if runtime.GOOS == "windows" {
		identity = strings.ToLower(identity)
	}
	hash := sha256.Sum256([]byte(identity + "\x00" + profile))
	return filepath.Join(root, "editing-"+hex.EncodeToString(hash[:16])+".json")
}

type Store struct {
	mu      sync.Mutex
	path    string
	loaded  bool
	hash    [32]byte
	existed bool
}

func NewStore(path string) *Store { return &Store{path: path} }
func read(path string) ([]byte, bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, ErrUnavailable
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return nil, true, ErrUnavailable
	}
	return raw, true, nil
}
func (store *Store) Load() (Snapshot, string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(store.path), 0700); err != nil {
		return Empty(), "", ErrUnavailable
	}
	unlock, err := lock(store.path + ".lock")
	if err != nil {
		return Empty(), "", ErrUnavailable
	}
	defer unlock()
	raw, existed, err := read(store.path)
	if err != nil {
		return Empty(), "", err
	}
	state := Empty()
	notice := ""
	if existed {
		state, err = decode(raw)
		if err == ErrVersion {
			return Empty(), "", err
		}
		if err != nil {
			// Retain the first corrupt original, without ever replacing that backup.
			backup, openErr := os.OpenFile(store.path+".corrupt", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if openErr == nil {
				closeErr := backup.Close()
				if closeErr != nil || os.Rename(store.path, store.path+".corrupt") != nil {
					os.Remove(store.path + ".corrupt")
					return Empty(), "", ErrUnavailable
				}
				raw, existed = nil, false
			} else if !errors.Is(openErr, os.ErrExist) {
				return Empty(), "", ErrUnavailable
			}
			state = Empty()
			notice = "Local editing state was damaged. A backup was kept; defaults are in use."
		}
	}
	store.hash, store.existed, store.loaded = sha256.Sum256(raw), existed, true
	return state, notice, nil
}
func (store *Store) Save(state Snapshot) error {
	if err := Validate(state); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil || len(raw) > maxFileBytes {
		return ErrBudget
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.loaded {
		return ErrUnavailable
	}
	unlock, err := lock(store.path + ".lock")
	if err != nil {
		return ErrUnavailable
	}
	defer unlock()
	current, existed, err := read(store.path)
	if err != nil {
		return err
	}
	if existed != store.existed || sha256.Sum256(current) != store.hash {
		return ErrConflict
	}
	temporary, err := os.CreateTemp(filepath.Dir(store.path), ".editing-*")
	if err != nil {
		return ErrUnavailable
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err = temporary.Write(raw); err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err != nil || closeErr != nil {
		return ErrUnavailable
	}
	if err = os.Rename(name, store.path); err != nil {
		return ErrUnavailable
	}
	store.hash, store.existed = sha256.Sum256(raw), true
	return nil
}
