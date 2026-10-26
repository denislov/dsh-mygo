package app

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"dsh-mygo/internal/host"
	"dsh-mygo/internal/runtimes"
	"dsh-mygo/internal/version"
	"dsh-mygo/internal/view"
)

func TestStartWithoutRuntimeAwaitsSetup(t *testing.T) {
	manager := runtimes.NewManager(t.TempDir(), t.TempDir(), "denislov/dsh-mygo")
	application := New(Options{
		CWD:      t.TempDir(),
		Runtimes: manager,
		Profile:  "mygo",
		Post:     func(fn func()) { fn() },
	})
	application.Start()
	if !application.model.Runtime.SetupRequired {
		t.Fatal("setup was not required without a runtime")
	}
	if application.model.Phase != view.PhaseAwaitingRuntime {
		t.Fatalf("phase = %v, want awaiting runtime", application.model.Phase)
	}
	if application.model.Runtime.Error != "" {
		t.Fatalf("setup should not start with an error: %q", application.model.Runtime.Error)
	}
	if application.currentSupervisor() != nil {
		t.Fatal("host started without a runtime")
	}
}

func TestInstallRuntimeArchiveStartsHost(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	fakeHost, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	manager := runtimes.NewManager(t.TempDir(), t.TempDir(), "denislov/dsh-mygo")
	updates := make(chan func(), 128)
	application := New(Options{
		CWD:      t.TempDir(),
		Runtimes: manager,
		Profile:  "mygo",
		Provider: "test",
		Model:    "echo",
		RuntimeLaunch: func() (RuntimeLaunchPlan, error) {
			return RuntimeLaunchPlan{Config: host.Config{Command: node, Args: []string{fakeHost}}}, nil
		},
		Post: func(fn func()) { updates <- fn },
	})
	defer application.Shutdown(5 * time.Second)
	go application.Start()
	waitFor(t, application, updates, 10*time.Second, func() bool {
		return application.Model().Runtime.SetupRequired
	})

	application.installRuntimeArchive(writeRuntimeArchive(t))
	waitFor(t, application, updates, 15*time.Second, func() bool {
		return application.Model().Phase == view.PhaseIdle
	})
	selected, ok := manager.Selected()
	if !ok {
		t.Fatal("installed runtime was not selected")
	}
	if !validSessionID(application.sessionID) {
		t.Fatalf("session id = %q", application.sessionID)
	}
	if application.model.Runtime.SetupRequired {
		t.Fatal("setup remained required after the host started")
	}
	if selected.Kind != runtimes.KindManaged || selected.ID != application.model.Runtime.Current.ID {
		t.Fatalf("selected runtime = %+v", selected)
	}
}

func TestSwitchRuntimeCommitsAfterInitialize(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	fakeHost, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	manager := runtimes.NewManager(t.TempDir(), t.TempDir(), "denislov/dsh-mygo")
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	first, err := manager.InstallArchive(context.Background(), writeRuntimeArchiveNode(t, "24.14.0"), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.InstallArchive(context.Background(), writeRuntimeArchiveNode(t, "24.15.0"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Select(first.ID); err != nil {
		t.Fatal(err)
	}

	updates := make(chan func(), 128)
	application := New(Options{
		CWD:      t.TempDir(),
		Runtimes: manager,
		Profile:  "mygo",
		Provider: "test",
		Model:    "echo",
		RuntimeLaunch: func() (RuntimeLaunchPlan, error) {
			return RuntimeLaunchPlan{Config: host.Config{Command: node, Args: []string{fakeHost, "--sessions", "--accept-resume", "--models"}}}, nil
		},
		Post: func(fn func()) { updates <- fn },
	})
	defer application.Shutdown(5 * time.Second)
	go application.Start()
	waitFor(t, application, updates, 10*time.Second, func() bool {
		return application.Model().Phase == view.PhaseIdle
	})
	originalSession := application.sessionID
	if originalSession == "" {
		t.Fatal("host did not create a session")
	}
	wantRoute := selectStartupTestRoute(t, application, updates, "test")

	application.OpenRuntimeSettings()
	if !application.model.SettingsOpen || application.model.SettingsSection != view.SettingsSectionRuntime {
		t.Fatal("runtime settings did not open")
	}
	application.ActivateRuntime(second.ID)
	waitFor(t, application, updates, 15*time.Second, func() bool {
		return !application.Model().Runtime.Switching && application.Model().Phase == view.PhaseIdle &&
			application.Model().Runtime.Current.ID == second.ID
	})
	selected, ok := manager.Selected()
	if !ok || selected.ID != second.ID {
		t.Fatalf("selected = %+v, %v", selected, ok)
	}
	if application.sessionID != originalSession {
		t.Fatalf("session = %q, want %q", application.sessionID, originalSession)
	}
	if application.model.ActiveRoute != wantRoute {
		t.Fatalf("runtime switch lost selected route: %+v", application.model.ActiveRoute)
	}
	if application.model.SettingsNotice != "Runtime switched." {
		t.Fatalf("settings notice = %q", application.model.SettingsNotice)
	}
}

func TestSwitchRuntimeRollsBackOnFailure(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	fakeHost, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-mygo-host.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	manager := runtimes.NewManager(t.TempDir(), t.TempDir(), "denislov/dsh-mygo")
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	first, err := manager.InstallArchive(context.Background(), writeRuntimeArchiveNode(t, "24.14.0"), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.InstallArchive(context.Background(), writeRuntimeArchiveNode(t, "24.15.0"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Select(first.ID); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	updates := make(chan func(), 128)
	application := New(Options{
		CWD:      t.TempDir(),
		Runtimes: manager,
		Profile:  "mygo",
		Provider: "test",
		Model:    "echo",
		RuntimeLaunch: func() (RuntimeLaunchPlan, error) {
			if calls.Add(1) == 2 {
				return RuntimeLaunchPlan{}, errors.New("candidate host failed")
			}
			return RuntimeLaunchPlan{Config: host.Config{Command: node, Args: []string{fakeHost, "--sessions", "--accept-resume", "--models"}}}, nil
		},
		Post: func(fn func()) { updates <- fn },
	})
	defer application.Shutdown(5 * time.Second)
	go application.Start()
	waitFor(t, application, updates, 10*time.Second, func() bool {
		return application.Model().Phase == view.PhaseIdle
	})
	originalSession := application.sessionID

	wantRoute := selectStartupTestRoute(t, application, updates, "test")
	application.OpenRuntimeSettings()
	application.ActivateRuntime(second.ID)
	waitFor(t, application, updates, 15*time.Second, func() bool {
		return !application.Model().Runtime.Switching && application.Model().Phase == view.PhaseIdle &&
			application.Model().SettingsError != ""
	})
	selected, ok := manager.Selected()
	if !ok || selected.ID != first.ID {
		t.Fatalf("selection after failure = %+v, %v", selected, ok)
	}
	if application.model.Runtime.Current.ID != first.ID {
		t.Fatalf("current runtime = %+v", application.model.Runtime.Current)
	}
	if application.sessionID != originalSession {
		t.Fatalf("session = %q, want %q", application.sessionID, originalSession)
	}
	if calls.Load() < 3 {
		t.Fatalf("runtime launches = %d, want a rollback restart", calls.Load())
	}
	if application.model.ActiveRoute != wantRoute {
		t.Fatalf("runtime rollback lost selected route: %+v", application.model.ActiveRoute)
	}
}

func TestRemoveUnselectedRuntime(t *testing.T) {
	manager := runtimes.NewManager(t.TempDir(), t.TempDir(), "denislov/dsh-mygo")
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	first, err := manager.InstallArchive(context.Background(), writeRuntimeArchiveNode(t, "24.14.0"), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.InstallArchive(context.Background(), writeRuntimeArchiveNode(t, "24.15.0"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Select(first.ID); err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 64)
	application := New(Options{
		Runtimes: manager,
		Profile:  "mygo",
		Post:     func(fn func()) { updates <- fn },
	})
	application.refreshRuntimeChoicesLocked()

	application.RemoveRuntime(first.ID)
	if application.model.Runtime.Error == "" {
		t.Fatal("selected runtime removal was not rejected")
	}
	application.RemoveRuntime(second.ID)
	waitFor(t, application, updates, 5*time.Second, func() bool {
		return !application.Model().Runtime.Busy && application.Model().Runtime.Notice == "Runtime removed."
	})
	if _, ok := manager.Find(second.ID); ok {
		t.Fatal("removed runtime is still installed")
	}
	selected, ok := manager.Selected()
	if !ok || selected.ID != first.ID {
		t.Fatalf("selected = %+v, %v", selected, ok)
	}
	if application.model.Runtime.Notice != "Runtime removed." {
		t.Fatalf("notice = %q", application.model.Runtime.Notice)
	}
}

func writeRuntimeArchive(t *testing.T) string {
	t.Helper()
	return writeRuntimeArchiveNode(t, "24.14.0")
}

func writeRuntimeArchiveNode(t *testing.T, node string) string {
	t.Helper()
	manifest := runtimes.Manifest{
		SchemaVersion:   1,
		Kind:            "dsh-runtime",
		DSH:             version.DSH,
		Node:            node,
		Platform:        runtimes.CurrentExpected().Platform,
		Arch:            runtimes.CurrentExpected().Arch,
		ProtocolVersion: version.Protocol,
		HostVersion:     version.App,
		AppMin:          version.AppMin,
		AppMaxExclusive: version.AppMaxExclusive,
		Entry:           "mygo-host.mjs",
		HostBundle:      "host",
	}
	manifest.ID = runtimes.RuntimeID(manifest)
	manifestData, _ := json.Marshal(manifest)
	hostData, _ := json.Marshal(map[string]any{
		"name":    "@dsh-mygo/host",
		"version": manifest.HostVersion,
		"engines": map[string]string{"node": ">=24", "dsh": manifest.DSH},
		"dshMygo": map[string]any{
			"protocolVersion": manifest.ProtocolVersion,
			"appCompatibility": map[string]string{
				"min": manifest.AppMin, "maxExclusive": manifest.AppMaxExclusive,
			},
		},
	})
	entries := []struct {
		name string
		body string
		mode int64
	}{
		{"manifest.json", string(manifestData), 0o644},
		{"host/package.json", string(hostData), 0o644},
		{"host/index.mjs", "// host\n", 0o644},
		{"mygo-host.mjs", "// entry\n", 0o644},
		{"node/node.exe", "fake node", 0o644},
		{"node_modules/@deepseek-ai/dsh/package.json", `{"version":"` + manifest.DSH + `"}`, 0o644},
	}
	path := filepath.Join(t.TempDir(), "runtime.tgz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(entry.body)), Typeflag: tar.TypeReg}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestShutdownCancelsRuntimeOperation(t *testing.T) {
	manager := runtimes.NewManager(t.TempDir(), t.TempDir(), "denislov/dsh-mygo")
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	application := New(Options{
		Runtimes: manager,
		Profile:  "mygo",
		Post:     func(fn func()) { fn() },
	})
	ctx, cancel := context.WithCancel(context.Background())
	application.runtimeCancel = cancel
	application.Shutdown(time.Second)
	if ctx.Err() == nil {
		t.Fatal("shutdown did not cancel the runtime operation")
	}
}

func TestRuntimeSettingsOpenFromRecovery(t *testing.T) {
	manager := runtimes.NewManager(t.TempDir(), t.TempDir(), "denislov/dsh-mygo")
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	application := New(Options{
		Runtimes: manager,
		Profile:  "mygo",
		Post:     func(fn func()) { fn() },
	})
	application.model.Phase = view.PhaseFailed
	application.model.RecoveryOpen = true
	application.model.Recoverable = true
	application.model.Error = "The host exited"

	application.OpenRuntimeSettings()
	if application.model.RecoveryOpen || !application.model.SettingsOpen ||
		application.model.SettingsSection != view.SettingsSectionRuntime {
		t.Fatalf("runtime settings did not replace recovery: %+v", application.model)
	}
	application.CloseSettings()
	if !application.model.RecoveryOpen {
		t.Fatal("closing runtime settings did not restore the recovery dialog")
	}
}
