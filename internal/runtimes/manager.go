package runtimes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/version"
)

const stateFileName = "state.json"
const maxStateBytes = 64 << 10

// Kind identifies where a runtime comes from.
type Kind string

const (
	KindManaged  Kind = "managed"
	KindExternal Kind = "external"
	KindLegacy   Kind = "legacy"
	KindDirect   Kind = "direct"
)

// Runtime is one persisted or ad-hoc runtime selection.
type Runtime struct {
	ID           string `json:"id"`
	Kind         Kind   `json:"kind"`
	Dir          string `json:"dir,omitempty"`
	Command      string `json:"command,omitempty"`
	NodePath     string `json:"nodePath,omitempty"`
	DSH          string `json:"dsh"`
	Node         string `json:"node,omitempty"`
	HostVersion  string `json:"hostVersion,omitempty"`
	InstalledAt  string `json:"installedAt,omitempty"`
	DisplayLabel string `json:"-"`
}

type state struct {
	Version  int       `json:"version"`
	Selected string    `json:"selected,omitempty"`
	Runtimes []Runtime `json:"runtimes"`
}

// Manager owns the local runtime store. Methods are safe for concurrent use.
type Manager struct {
	Root         string
	ResourcesDir string
	Repository   string
	APIBase      string
	HTTPClient   *http.Client
	DirectDir    string

	allowInsecure bool

	mu      sync.Mutex
	loaded  bool
	state   state
	direct  *Runtime
	catalog catalogCache
}

type catalogCache struct {
	release CatalogRelease
	at      time.Time
}

// NewManager creates a runtime store under root. No I/O happens until Load.
func NewManager(root, resourcesDir, repository string) *Manager {
	return &Manager{
		Root:         root,
		ResourcesDir: resourcesDir,
		Repository:   repository,
		APIBase:      "https://api.github.com",
		HTTPClient:   http.DefaultClient,
		state:        state{Version: 1, Runtimes: []Runtime{}},
	}
}

// Load reads state.json. A corrupt file is moved aside and reported while the
// store continues with an empty selection.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loaded {
		return nil
	}
	m.loaded = true
	m.state = state{Version: 1, Runtimes: []Runtime{}}
	if m.DirectDir != "" {
		runtime, err := validateDirect(m.DirectDir)
		if err != nil {
			return err
		}
		m.direct = &runtime
	}
	path := filepath.Join(m.Root, stateFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read runtime state: %w", err)
	}
	if len(data) > maxStateBytes {
		return m.corruptState(path, "runtime state is too large")
	}
	var loaded state
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&loaded); err != nil {
		return m.corruptState(path, "runtime state is invalid")
	}
	if err := validateState(loaded); err != nil {
		return m.corruptState(path, err.Error())
	}
	m.state = loaded
	return nil
}

func (m *Manager) corruptState(path, reason string) error {
	backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().UnixNano())
	_ = os.Rename(path, backup)
	return fmt.Errorf("%s; the previous file was kept at %s", reason, backup)
}

func validateState(value state) error {
	if value.Version != 1 {
		return fmt.Errorf("unsupported runtime state version")
	}
	seen := make(map[string]bool, len(value.Runtimes))
	for _, entry := range value.Runtimes {
		if !identifierPattern.MatchString(entry.ID) || seen[entry.ID] {
			return fmt.Errorf("runtime state contains an invalid id")
		}
		seen[entry.ID] = true
		switch entry.Kind {
		case KindManaged:
			if !filepath.IsAbs(entry.Dir) {
				return fmt.Errorf("managed runtime path is not absolute")
			}
		case KindExternal:
			if !filepath.IsAbs(entry.Command) || !filepath.IsAbs(entry.NodePath) {
				return fmt.Errorf("external runtime path is not absolute")
			}
		case KindLegacy:
			if !filepath.IsAbs(entry.Dir) {
				return fmt.Errorf("legacy runtime path is not absolute")
			}
		default:
			return fmt.Errorf("runtime state contains an unsupported kind")
		}
		if entry.DSH == "" || entry.HostVersion == "" {
			return fmt.Errorf("runtime state is missing identity fields")
		}
	}
	if value.Selected != "" && !seen[value.Selected] {
		return fmt.Errorf("selected runtime is missing from the state")
	}
	return nil
}

// Snapshot returns a copy of the selected runtime and installed entries.
func (m *Manager) Snapshot() (Runtime, bool, []Runtime) {
	m.mu.Lock()
	defer m.mu.Unlock()
	selected, ok := m.selectedLocked()
	list := make([]Runtime, 0, len(m.state.Runtimes)+1)
	if m.direct != nil {
		list = append(list, *m.direct)
	}
	for _, entry := range m.state.Runtimes {
		if entry.Dir != "" && entry.Kind == KindManaged {
			entry.DisplayLabel = entry.ID
		}
		list = append(list, entry)
	}
	return selected, ok, list
}

func (m *Manager) Selected() (Runtime, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.selectedLocked()
}

func (m *Manager) selectedLocked() (Runtime, bool) {
	if m.direct != nil {
		return *m.direct, true
	}
	if m.state.Selected == "" {
		return Runtime{}, false
	}
	for _, entry := range m.state.Runtimes {
		if entry.ID == m.state.Selected {
			return entry, true
		}
	}
	return Runtime{}, false
}

// Find returns an installed or direct runtime by id.
func (m *Manager) Find(id string) (Runtime, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.direct != nil && m.direct.ID == id {
		return *m.direct, true
	}
	for _, entry := range m.state.Runtimes {
		if entry.ID == id {
			return entry, true
		}
	}
	return Runtime{}, false
}

// Select persists the selected runtime. Direct runtimes are already fixed.
func (m *Manager) Select(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.direct != nil {
		if m.direct.ID == id {
			return nil
		}
		return fmt.Errorf("a direct runtime is already selected")
	}
	if _, ok := m.findLocked(id); !ok {
		return fmt.Errorf("runtime %s is not installed", id)
	}
	previous := m.state.Selected
	m.state.Selected = id
	if err := m.saveLocked(); err != nil {
		m.state.Selected = previous
		return err
	}
	return nil
}

func (m *Manager) findLocked(id string) (Runtime, bool) {
	for _, entry := range m.state.Runtimes {
		if entry.ID == id {
			return entry, true
		}
	}
	return Runtime{}, false
}

// AddExternal records a validated existing DSH installation without copying it.
func (m *Manager) AddExternal(candidate External) (Runtime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := candidate.Runtime()
	if existing, exists := m.findLocked(runtime.ID); exists {
		return existing, nil
	}
	m.state.Runtimes = append(m.state.Runtimes, runtime)
	if err := m.saveLocked(); err != nil {
		m.state.Runtimes = m.state.Runtimes[:len(m.state.Runtimes)-1]
		return Runtime{}, err
	}
	return runtime, nil
}

// AddLegacy records a validated runtime left beside an older installation.
func (m *Manager) AddLegacy(candidate Runtime) (Runtime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	candidate.Kind = KindLegacy
	candidate.DisplayLabel = "Runtime bundled with a previous installation"
	if _, exists := m.findLocked(candidate.ID); exists {
		return candidate, nil
	}
	m.state.Runtimes = append(m.state.Runtimes, candidate)
	if err := m.saveLocked(); err != nil {
		m.state.Runtimes = m.state.Runtimes[:len(m.state.Runtimes)-1]
		return Runtime{}, err
	}
	return candidate, nil
}

func (m *Manager) selectLocked(id string) error {
	previous := m.state.Selected
	m.state.Selected = id
	if err := m.saveLocked(); err != nil {
		m.state.Selected = previous
		return err
	}
	return nil
}

// Remove forgets an unselected runtime and deletes managed files.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.direct != nil && m.direct.ID == id {
		return fmt.Errorf("the direct runtime cannot be removed")
	}
	if m.state.Selected == id {
		return fmt.Errorf("switch to another runtime before removing this one")
	}
	index := -1
	var entry Runtime
	for i, candidate := range m.state.Runtimes {
		if candidate.ID == id {
			index = i
			entry = candidate
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("runtime %s is not installed", id)
	}
	var trash string
	if entry.Kind == KindManaged {
		trash = filepath.Join(m.Root, ".trash-"+entry.ID)
		_ = os.RemoveAll(trash)
		if err := os.Rename(entry.Dir, trash); err != nil {
			return fmt.Errorf("remove runtime files: %w", err)
		}
	}
	m.state.Runtimes = append(m.state.Runtimes[:index], m.state.Runtimes[index+1:]...)
	if err := m.saveLocked(); err != nil {
		m.state.Runtimes = append(m.state.Runtimes, Runtime{})
		copy(m.state.Runtimes[index+1:], m.state.Runtimes[index:])
		m.state.Runtimes[index] = entry
		if trash != "" {
			_ = os.Rename(trash, entry.Dir)
		}
		return err
	}
	if trash != "" {
		_ = os.RemoveAll(trash)
	}
	return nil
}

// HostBundleDir is the profile seed source for a runtime.
func (m *Manager) HostBundleDir(runtime Runtime) string {
	switch runtime.Kind {
	case KindManaged, KindDirect:
		candidate := filepath.Join(runtime.Dir, hostBundleName)
		if info, err := os.Stat(filepath.Join(candidate, "package.json")); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return filepath.Join(m.ResourcesDir, hostBundleName)
}

// Identity is the host bundle and DSH identity expected from initialize.
func Identity(runtime Runtime) (hostVersion, dshVersion string) {
	return runtime.HostVersion, runtime.DSH
}

// Launch builds the host process configuration for the selected runtime.
func (m *Manager) Launch(profile, cwd, dshHome string) (host.Config, string, Runtime, error) {
	runtime, ok := m.Selected()
	if !ok {
		return host.Config{}, "", Runtime{}, ErrNoRuntime
	}
	hostBundle := m.HostBundleDir(runtime)
	var config host.Config
	var err error
	switch runtime.Kind {
	case KindExternal:
		config, err = host.ExternalLaunch(host.ExternalLaunchOptions{
			Command: runtime.Command, Node: runtime.NodePath, Profile: profile, CWD: cwd, DSHHome: dshHome,
		})
	default:
		config, err = host.RuntimeLaunch(host.RuntimeLaunchOptions{
			RuntimeDir: runtime.Dir, Profile: profile, CWD: cwd, DSHHome: dshHome,
		})
	}
	if err != nil {
		return host.Config{}, "", Runtime{}, err
	}
	return config, hostBundle, runtime, nil
}

// ErrNoRuntime means the store has no selected runtime.
var ErrNoRuntime = errors.New("no DSH runtime is selected")

func (m *Manager) saveLocked() error {
	if err := os.MkdirAll(m.Root, 0o755); err != nil {
		return fmt.Errorf("create runtime store: %w", err)
	}
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode runtime state: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(m.Root, stateFileName)
	temp, err := os.CreateTemp(m.Root, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create runtime state: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write runtime state: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync runtime state: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close runtime state: %w", err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("replace runtime state: %w", err)
	}
	return nil
}

// CleanupStaging removes abandoned extraction and download directories.
func (m *Manager) CleanupStaging() {
	entries, err := os.ReadDir(m.Root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".staging-") || strings.HasPrefix(entry.Name(), ".trash-") {
			_ = os.RemoveAll(filepath.Join(m.Root, entry.Name()))
		}
	}
}

// LegacyCandidate returns a validated runtime left beside an older installation.
func (m *Manager) LegacyCandidate() (Runtime, bool) {
	dir := filepath.Join(m.ResourcesDir, "runtime")
	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil || len(data) > maxManifestBytes {
		return Runtime{}, false
	}
	var legacy struct {
		App             string `json:"app"`
		ProtocolVersion int    `json:"protocolVersion"`
		DSH             string `json:"dsh"`
		Node            string `json:"node"`
		Platform        string `json:"platform"`
		Arch            string `json:"arch"`
	}
	if json.Unmarshal(data, &legacy) != nil || legacy.ProtocolVersion != version.Protocol || legacy.DSH != version.DSH ||
		!platformMatches(legacy.Platform) || !archMatches(legacy.Arch) {
		return Runtime{}, false
	}
	if bundledNodePath(dir) == "" || !fileExists(filepath.Join(dir, entryName)) {
		return Runtime{}, false
	}
	var dsh struct {
		Version string `json:"version"`
	}
	if readJSON(filepath.Join(dir, "node_modules", "@deepseek-ai", "dsh", "package.json"), &dsh) != nil || dsh.Version != legacy.DSH {
		return Runtime{}, false
	}
	id := "legacy-bundled"
	return Runtime{
		ID: id, Kind: KindLegacy, Dir: dir, DSH: legacy.DSH, Node: legacy.Node,
		HostVersion: version.App, DisplayLabel: "Runtime bundled with a previous installation",
	}, true
}

func validateDirect(dir string) (Runtime, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return Runtime{}, fmt.Errorf("resolve direct runtime: %w", err)
	}
	manifest, err := ValidateInstalled(absolute, CurrentExpected())
	if err != nil {
		return Runtime{}, err
	}
	sum := sha256.Sum256([]byte(strings.ToLower(absolute)))
	return Runtime{
		ID:           "direct-" + hex.EncodeToString(sum[:6]),
		Kind:         KindDirect,
		Dir:          absolute,
		DSH:          manifest.DSH,
		Node:         manifest.Node,
		HostVersion:  manifest.HostVersion,
		DisplayLabel: "Development runtime",
	}, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
