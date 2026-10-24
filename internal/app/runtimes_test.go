package app

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
	if application.supervisor != nil {
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

func writeRuntimeArchive(t *testing.T) string {
	t.Helper()
	manifest := runtimes.Manifest{
		SchemaVersion:   1,
		Kind:            "dsh-runtime",
		DSH:             version.DSH,
		Node:            "24.14.0",
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
