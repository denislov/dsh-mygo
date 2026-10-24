package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"dsh-mygo/internal/runtimes"
	"dsh-mygo/internal/view"
	"github.com/egoist/mygo"
)

const runtimeCheckTimeout = 15 * time.Second

// runtimeSwitch is one host restart around a candidate runtime. The selection
// is committed only after the candidate initializes.
type runtimeSwitch struct {
	target   runtimes.Runtime
	previous runtimes.Runtime
	resumeID string
}

func (a *App) runtimeAvailable() bool { return a.options.Runtimes != nil }

func (a *App) pendingRuntimeSwitch() (runtimes.Runtime, bool) {
	a.switchMu.Lock()
	defer a.switchMu.Unlock()
	if a.switchRuntime == nil {
		return runtimes.Runtime{}, false
	}
	return a.switchRuntime.target, true
}

func (a *App) hasPendingRuntimeSwitch() bool {
	_, ok := a.pendingRuntimeSwitch()
	return ok
}

// switchNeedsResume disables the new-session fallback when replacing a host:
// a candidate that cannot restore the current session must roll back instead
// of silently starting a different conversation.
func (a *App) switchNeedsResume() bool {
	a.switchMu.Lock()
	defer a.switchMu.Unlock()
	return a.switchRuntime != nil && a.switchRuntime.previous.ID != ""
}

func (a *App) beginRuntimeOperation(status string) (context.Context, uint64, bool) {
	if !a.runtimeAvailable() || a.model.Runtime.Busy || a.model.Runtime.Switching || a.shuttingDown.Load() {
		return nil, 0, false
	}
	if a.runtimeCancel != nil {
		a.runtimeCancel()
	}
	a.runtimeGeneration++
	generation := a.runtimeGeneration
	ctx, cancel := context.WithCancel(context.Background())
	a.runtimeCancel = cancel
	a.model.Runtime.Checking = false
	a.model.Runtime.Busy = true
	a.model.Runtime.Progress = 0
	a.model.Runtime.Status = status
	a.model.Runtime.Error = ""
	a.model.Runtime.Notice = ""
	return ctx, generation, true
}

func (a *App) postRuntime(gen uint64, fn func()) {
	a.post(func() {
		if gen != a.runtimeGeneration || a.shuttingDown.Load() {
			return
		}
		fn()
	})
}

func (a *App) finishRuntimeOperation(gen uint64, err error, notice string) {
	a.postRuntime(gen, func() {
		a.model.Runtime.Busy = false
		a.model.Runtime.Progress = 0
		a.model.Runtime.Status = ""
		if err != nil {
			a.model.Runtime.Error = runtimeErrorMessage(err)
			a.logf("runtime operation failed: %v", err)
			return
		}
		a.model.Runtime.Error = ""
		a.model.Runtime.Notice = notice
		a.refreshRuntimeChoicesLocked()
	})
}

func (a *App) reportRuntimeProgress(gen uint64, stage string, ratio float32) {
	a.postRuntime(gen, func() {
		a.model.Runtime.Status = stage
		a.model.Runtime.Progress = ratio
	})
}

// DetectRuntimes probes the dsh command on PATH and any runtime left by an
// older installation. It never selects anything.
func (a *App) DetectRuntimes() {
	if !a.runtimeAvailable() || a.model.Runtime.Busy || a.model.Runtime.Checking || a.model.Runtime.Switching {
		return
	}
	a.model.Runtime.Checking = true
	a.model.Runtime.Notice = ""
	a.model.Runtime.Candidates = nil
	a.runtimeGeneration++
	generation := a.runtimeGeneration
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), runtimeCheckTimeout)
		defer cancel()
		external, probeErr := runtimes.ProbeExternal(ctx, "")
		legacy, hasLegacy := a.options.Runtimes.LegacyCandidate()
		a.post(func() {
			if generation != a.runtimeGeneration || a.shuttingDown.Load() {
				return
			}
			a.model.Runtime.Checking = false
			a.model.Runtime.Candidates = nil
			if probeErr == nil {
				runtime := external.Runtime()
				choice := runtimeChoice(runtime)
				choice.Compatible = external.Compatible
				choice.Reason = external.Reason
				choice.Path = external.Command
				choice.Detail = "Installed command"
				a.model.Runtime.Candidates = append(a.model.Runtime.Candidates, choice)
			}
			if hasLegacy {
				choice := runtimeChoice(legacy)
				choice.Detail = "Bundled with a previous app installation"
				a.model.Runtime.Candidates = append(a.model.Runtime.Candidates, choice)
			}
			if probeErr != nil && !hasLegacy {
				a.model.Runtime.Notice = "No installed DSH was found. Download a runtime bundle or choose a dsh command."
				a.logf("runtime detection: %v", probeErr)
			}
		})
	}()
}

// BrowseDSH lets the user pick a dsh executable that is not on PATH.
func (a *App) BrowseDSH() {
	if !a.runtimeAvailable() || a.model.Runtime.Busy || a.model.Runtime.Checking || a.model.Runtime.Switching {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:   "Choose dsh command",
			Filters: []mygo.FileFilter{{Name: "DSH command", Extensions: []string{"exe", "cmd", "bat"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		a.probeExternal(paths[0])
	}()
}

func (a *App) probeExternal(command string) {
	if !a.runtimeAvailable() || a.model.Runtime.Busy || a.model.Runtime.Checking || a.model.Runtime.Switching {
		return
	}
	a.model.Runtime.Checking = true
	a.model.Runtime.Error = ""
	a.runtimeGeneration++
	generation := a.runtimeGeneration
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), runtimeCheckTimeout)
		defer cancel()
		external, err := runtimes.ProbeExternal(ctx, command)
		a.post(func() {
			if generation != a.runtimeGeneration || a.shuttingDown.Load() {
				return
			}
			a.model.Runtime.Checking = false
			a.model.Runtime.Candidates = nil
			if err != nil {
				a.model.Runtime.Error = "The selected dsh command could not be used. Check Node.js 24+ and the DSH version."
				a.logf("runtime probe: %v", err)
				return
			}
			choice := runtimeChoice(external.Runtime())
			choice.Compatible = external.Compatible
			choice.Reason = external.Reason
			choice.Path = command
			choice.Detail = command
			a.model.Runtime.Candidates = append(a.model.Runtime.Candidates, choice)
		})
	}()
}

// InstallRuntimeFile asks for a runtime archive and installs it.
func (a *App) InstallRuntimeFile() {
	if !a.runtimeAvailable() || a.model.Runtime.Busy || a.model.Runtime.Checking || a.model.Runtime.Switching {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:   "Choose runtime bundle",
			Filters: []mygo.FileFilter{{Name: "Runtime bundle", Extensions: []string{"tgz", "gz"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		a.installRuntimeArchive(paths[0])
	}()
}

func (a *App) installRuntimeArchive(path string) {
	ctx, generation, ok := a.beginRuntimeOperation("Installing runtime")
	if !ok {
		return
	}
	go func() {
		installed, err := a.options.Runtimes.InstallArchive(ctx, path, func(ratio float32) {
			a.reportRuntimeProgress(generation, "Installing runtime", ratio)
		})
		if err != nil {
			a.finishRuntimeOperation(generation, err, "")
			return
		}
		a.postRuntime(generation, func() { a.installedRuntimeReady(installed) })
	}()
}

func (a *App) installedRuntimeReady(installed runtimes.Runtime) {
	a.model.Runtime.Busy = false
	a.model.Runtime.Progress = 0
	a.model.Runtime.Status = ""
	a.model.Runtime.Notice = "Runtime installed."
	a.refreshRuntimeChoicesLocked()
	if a.model.SettingsOpen {
		a.model.SettingsNotice = "Runtime installed. Use it to switch."
		return
	}
	a.activateRuntime(installed.ID)
}

// DownloadRuntime downloads the newest compatible runtime, or a specific
// catalog entry when id is non-empty.
func (a *App) DownloadRuntime(id string) {
	ctx, generation, ok := a.beginRuntimeOperation("Checking runtime releases")
	if !ok {
		return
	}
	go func() {
		release, err := a.options.Runtimes.FetchCatalog(ctx, true)
		if err != nil {
			a.finishRuntimeOperation(generation, err, "")
			return
		}
		entry, ok := selectCatalogEntry(release, id)
		if !ok {
			a.finishRuntimeOperation(generation, errors.New("no compatible runtime is available for this app version"), "")
			return
		}
		a.reportRuntimeProgress(generation, "Downloading runtime", 0)
		installed, err := a.options.Runtimes.InstallDownload(ctx, release, entry, func(stage string, ratio float32) {
			a.reportRuntimeProgress(generation, stage, ratio)
		})
		if err != nil {
			a.finishRuntimeOperation(generation, err, "")
			return
		}
		a.postRuntime(generation, func() { a.installedRuntimeReady(installed) })
	}()
}

// CheckRuntimeUpdates refreshes the downloadable runtime list without starting
// an install.
func (a *App) CheckRuntimeUpdates() {
	ctx, generation, ok := a.beginRuntimeOperation("Checking runtime releases")
	if !ok {
		return
	}
	go func() {
		release, err := a.options.Runtimes.FetchCatalog(ctx, true)
		if err != nil {
			a.finishRuntimeOperation(generation, err, "")
			return
		}
		choices := make([]view.RuntimeChoice, 0, len(release.Catalog.Runtimes))
		for _, entry := range release.Catalog.Runtimes {
			choices = append(choices, view.RuntimeChoice{
				ID: entry.ID, Kind: string(runtimes.KindManaged), Label: "Runtime bundle",
				DSH: entry.DSH, Node: entry.Node, HostVersion: entry.HostVersion,
				Detail: formatBytes(entry.Size), Compatible: true,
			})
		}
		a.postRuntime(generation, func() {
			a.model.Runtime.Busy = false
			a.model.Runtime.Progress = 0
			a.model.Runtime.Status = ""
			a.model.Runtime.Available = choices
			if len(choices) == 0 {
				a.model.Runtime.Notice = "No compatible runtime bundle is available for this app version."
			} else {
				a.model.Runtime.Notice = ""
			}
		})
	}()
}

// UseExternalRuntime probes and adds an installed dsh command. In setup it
// starts that runtime; from Settings it becomes an installed entry and is
// switched to when the host is idle.
func (a *App) UseExternalRuntime(command string) {
	if !a.runtimeAvailable() || a.model.Runtime.Busy || a.model.Runtime.Checking || a.model.Runtime.Switching {
		return
	}
	ctx, generation, ok := a.beginRuntimeOperation("Checking installed DSH")
	if !ok {
		return
	}
	go func() {
		external, err := runtimes.ProbeExternal(ctx, command)
		if err != nil {
			a.finishRuntimeOperation(generation, err, "")
			return
		}
		if !external.Compatible {
			a.finishRuntimeOperation(generation, errors.New(external.Reason), "")
			return
		}
		installed, err := a.options.Runtimes.AddExternal(external)
		if err != nil {
			a.finishRuntimeOperation(generation, err, "")
			return
		}
		a.postRuntime(generation, func() {
			a.model.Runtime.Busy = false
			a.model.Runtime.Progress = 0
			a.model.Runtime.Status = ""
			a.refreshRuntimeChoicesLocked()
			if a.model.SettingsOpen {
				a.model.SettingsNotice = "Runtime added. Use it to switch."
				a.ActivateRuntime(installed.ID)
				return
			}
			a.activateRuntime(installed.ID)
		})
	}()
}

// UseLegacyRuntime adds the runtime left beside a previous installation.
func (a *App) UseLegacyRuntime() {
	if !a.runtimeAvailable() || a.model.Runtime.Busy || a.model.Runtime.Switching {
		return
	}
	candidate, ok := a.options.Runtimes.LegacyCandidate()
	if !ok {
		a.model.Runtime.Error = "The runtime bundled with the previous installation is no longer usable."
		return
	}
	installed, err := a.options.Runtimes.AddLegacy(candidate)
	if err != nil {
		a.model.Runtime.Error = runtimeErrorMessage(err)
		return
	}
	a.refreshRuntimeChoicesLocked()
	if a.model.SettingsOpen {
		a.ActivateRuntime(installed.ID)
		return
	}
	a.activateRuntime(installed.ID)
}

// UseInstalledRuntime starts an installed runtime from the setup screen.
func (a *App) UseInstalledRuntime(id string) {
	if !a.runtimeAvailable() {
		return
	}
	a.activateRuntime(id)
}

// ActivateRuntime switches a running app to another installed runtime.
func (a *App) ActivateRuntime(id string) {
	if !a.runtimeAvailable() || !a.model.CanActivateRuntime() {
		return
	}
	target, ok := a.options.Runtimes.Find(id)
	if !ok {
		a.model.Runtime.Error = "The selected runtime is no longer installed."
		a.refreshRuntimeChoicesLocked()
		return
	}
	selected, hasSelected := a.options.Runtimes.Selected()
	if hasSelected && selected.ID == target.ID {
		a.model.Runtime.Notice = "This runtime is already selected."
		return
	}
	if hasSelected && selected.Kind == runtimes.KindDirect {
		a.model.Runtime.Error = "The development runtime is fixed for this launch."
		return
	}
	a.beginRuntimeSwitch(target, selected, a.resumeSessionID())
}

func (a *App) resumeSessionID() string {
	resumeID := a.sessionID
	if resumeID == "" && a.local != nil {
		resumeID = a.local.lastSession
	}
	if a.model.SessionPanel.ActiveArchived {
		return ""
	}
	return resumeID
}

// activateRuntime starts a runtime chosen on the setup screen. The selection
// is committed only after the new host initializes, like a Settings switch.
func (a *App) activateRuntime(id string) {
	if !a.runtimeAvailable() {
		return
	}
	target, ok := a.options.Runtimes.Find(id)
	if !ok {
		a.model.Runtime.Error = "The selected runtime is no longer installed."
		a.refreshRuntimeChoicesLocked()
		return
	}
	previous, _ := a.options.Runtimes.Selected()
	a.beginRuntimeSwitch(target, previous, a.resumeSessionID())
}

func (a *App) beginRuntimeSwitch(target, previous runtimes.Runtime, resumeID string) {
	if !a.runtimeAvailable() || a.model.Runtime.Switching || a.model.Runtime.Busy {
		return
	}
	a.switchMu.Lock()
	a.switchRuntime = &runtimeSwitch{target: target, previous: previous, resumeID: resumeID}
	a.switchMu.Unlock()
	a.cancelSessionOperation()
	a.cancelResync()
	a.CloseSubagents()
	a.hostGeneration.Add(1)
	a.model.Runtime.Switching = true
	a.model.Runtime.SetupRequired = false
	a.model.Runtime.Busy = false
	a.model.Runtime.Checking = false
	a.model.Runtime.Progress = 0
	a.model.Runtime.Status = ""
	a.model.Runtime.Error = ""
	a.model.Runtime.Notice = ""
	a.model.Runtime.RemoveConfirm = ""
	a.model.SettingsError = ""
	a.model.SettingsNotice = ""
	a.model.Phase = view.PhaseStarting
	a.model.Error = ""
	a.model.ErrorDetail = ""
	a.model.RecoveryOpen = false
	a.model.Recoverable = false
	a.model.Restarting = false
	a.logf("runtime switch: %s -> %s", previous.ID, target.ID)
	go func() {
		a.stopHost()
		a.startHost(false, resumeID)
	}()
}

// commitRuntimeSwitch persists the candidate after its host initialized.
func (a *App) commitRuntimeSwitch() (runtimes.Runtime, error) {
	a.switchMu.Lock()
	switching := a.switchRuntime
	a.switchMu.Unlock()
	if switching == nil {
		return runtimes.Runtime{}, nil
	}
	if err := a.options.Runtimes.Select(switching.target.ID); err != nil {
		return runtimes.Runtime{}, err
	}
	a.switchMu.Lock()
	if a.switchRuntime == switching {
		a.switchRuntime = nil
	}
	a.switchMu.Unlock()
	return switching.target, nil
}

// failRuntimeSwitch stops the candidate and restores the previous runtime. A
// first start without a previous runtime returns to setup.
func (a *App) failRuntimeSwitch(cause error) {
	a.switchMu.Lock()
	switching := a.switchRuntime
	a.switchRuntime = nil
	a.switchMu.Unlock()
	if switching == nil {
		return
	}
	a.logf("runtime switch failed: %v", cause)
	a.stopHost()
	if switching.previous.ID == "" {
		a.post(func() {
			a.model.Runtime.Switching = false
			a.model.Runtime.SetupRequired = true
			a.model.Phase = view.PhaseAwaitingRuntime
			a.model.Runtime.Error = "Could not start this runtime. Choose another runtime or install a new one."
			a.refreshRuntimeChoicesLocked()
		})
		return
	}
	if a.options.PrepareProfile != nil {
		if err := a.options.PrepareProfile(a.options.Runtimes.HostBundleDir(switching.previous)); err != nil {
			a.logf("restore previous profile: %v", err)
		}
	}
	a.post(func() {
		a.model.Runtime.Error = ""
		a.model.Phase = view.PhaseStarting
		a.model.SettingsError = "Could not switch runtime. The previous runtime was restored."
		a.refreshRuntimeChoicesLocked()
	})
	a.startHost(false, switching.resumeID)
}

// OpenRuntimeSettings closes the recovery dialog and opens the runtime section.
func (a *App) OpenRuntimeSettings() {
	if !a.runtimeAvailable() {
		return
	}
	a.model.RecoveryOpen = false
	a.model.Error = ""
	a.model.ErrorDetail = ""
	a.model.RecoveryError = ""
	a.model.SettingsOpen = true
	a.model.SettingsConnection = false
	a.model.SettingsSection = view.SettingsSectionRuntime
	a.model.SettingsError = ""
	a.model.SettingsNotice = ""
	a.model.Runtime.Error = ""
	a.model.Runtime.Notice = ""
	a.model.Runtime.RemoveConfirm = ""
	a.refreshRuntimeChoicesLocked()
}

// RemoveRuntime forgets an unselected runtime and deletes managed files.
func (a *App) RemoveRuntime(id string) {
	if !a.runtimeAvailable() || a.model.Runtime.Checking {
		return
	}
	if a.model.Runtime.Current.ID == id {
		a.model.Runtime.Error = "Switch to another runtime before removing this one."
		return
	}
	_, generation, ok := a.beginRuntimeOperation("Removing runtime")
	if !ok {
		return
	}
	a.model.Runtime.RemoveConfirm = ""
	go func() {
		if err := a.options.Runtimes.Remove(id); err != nil {
			a.finishRuntimeOperation(generation, err, "")
			return
		}
		a.postRuntime(generation, func() {
			a.model.Runtime.Busy = false
			a.model.Runtime.Progress = 0
			a.model.Runtime.Status = ""
			a.model.Runtime.Error = ""
			a.model.Runtime.Notice = "Runtime removed."
			a.refreshRuntimeChoicesLocked()
		})
	}()
}

// CancelRuntimeOperation cancels the current download/install/probe.
func (a *App) CancelRuntimeOperation() {
	if a.runtimeCancel != nil {
		a.runtimeCancel()
		a.runtimeCancel = nil
	}
	a.runtimeGeneration++
	a.model.Runtime.Busy = false
	a.model.Runtime.Checking = false
	a.model.Runtime.Progress = 0
	a.model.Runtime.Status = ""
	a.model.Runtime.Notice = "Runtime operation cancelled."
}

func (a *App) refreshRuntimeChoicesLocked() {
	if !a.runtimeAvailable() {
		return
	}
	selected, ok, list := a.options.Runtimes.Snapshot()
	if ok {
		a.model.Runtime.Current = runtimeChoice(selected)
	} else {
		a.model.Runtime.Current = view.RuntimeChoice{}
	}
	installed := make([]view.RuntimeChoice, 0, len(list))
	for _, runtime := range list {
		installed = append(installed, runtimeChoice(runtime))
	}
	a.model.Runtime.Installed = installed
}

func runtimeChoice(runtime runtimes.Runtime) view.RuntimeChoice {
	label := "Managed runtime"
	detail := runtime.Dir
	switch runtime.Kind {
	case runtimes.KindExternal:
		label = "Existing DSH installation"
		detail = runtime.Command
	case runtimes.KindLegacy:
		label = "Runtime bundled with a previous installation"
	case runtimes.KindDirect:
		label = "Development runtime"
	}
	return view.RuntimeChoice{
		ID: runtime.ID, Kind: string(runtime.Kind), Label: label,
		DSH: runtime.DSH, Node: runtime.Node, HostVersion: runtime.HostVersion,
		Path: runtime.Dir, Detail: detail, Compatible: true,
	}
}

func selectCatalogEntry(release runtimes.CatalogRelease, id string) (runtimes.CatalogEntry, bool) {
	if len(release.Catalog.Runtimes) == 0 {
		return runtimes.CatalogEntry{}, false
	}
	if id == "" {
		return release.Catalog.Runtimes[0], true
	}
	for _, entry := range release.Catalog.Runtimes {
		if entry.ID == id {
			return entry, true
		}
	}
	return runtimes.CatalogEntry{}, false
}

func runtimeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, runtimes.ErrNoRuntime):
		return "A DSH runtime is required before the host can start."
	case strings.Contains(strings.ToLower(err.Error()), "checksum"):
		return "The runtime file failed verification. Try again or import a local bundle."
	case strings.Contains(strings.ToLower(err.Error()), "no runtime"):
		return "No compatible runtime is available for this app version."
	default:
		return "Could not complete the runtime operation. Check the logs and try again."
	}
}

func formatBytes(size int64) string {
	if size <= 0 {
		return ""
	}
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}
