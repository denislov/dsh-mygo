package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedactSecrets(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		input  string
		absent []string
	}{
		{
			name:   "api key assignment",
			input:  "DEEPSEEK_API_KEY=sk-1234567890abcdef\nnormal line",
			absent: []string{"sk-1234567890abcdef"},
		},
		{
			name:   "bearer token",
			input:  "Authorization: Bearer abcdef1234567890\nnormal line",
			absent: []string{"abcdef1234567890"},
		},
		{
			name:   "password",
			input:  "password: hunter2\nnormal line",
			absent: []string{"hunter2"},
		},
		{
			name:   "secret",
			input:  `secret = "abcdef123456"`,
			absent: []string{"abcdef123456"},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			got := redactSecrets(scenario.input)
			if !strings.Contains(got, "[REDACTED]") {
				t.Fatalf("redaction marker missing: %q", got)
			}
			for _, value := range scenario.absent {
				if strings.Contains(got, value) {
					t.Fatalf("secret %q survived redaction: %q", value, got)
				}
			}
			if !strings.Contains(got, "normal line") && scenario.input != `secret = "abcdef123456"` {
				t.Fatalf("ordinary text was removed: %q", got)
			}
		})
	}
}

func TestWriteCrashReportNamesAndRedacts(t *testing.T) {
	dir := t.TempDir()
	report := crashReport{
		Time:     time.Date(2026, 10, 23, 3, 4, 5, 0, time.UTC),
		App:      "0.1.0",
		Protocol: 1,
		DSH:      "0.2.1-rc.2",
		Platform: "windows/amd64",
		Session:  "session-1",
		Source:   "host",
		Summary:  "The host exited",
		Detail:   "exit status 1\nDEEPSEEK_API_KEY=sk-1234567890abcdef",
		PID:      4242,
		Stderr:   "Authorization: Bearer abcdef1234567890\nplugin failed",
	}
	path, err := writeCrashReport(dir, report)
	if err != nil {
		t.Fatalf("writeCrashReport: %v", err)
	}
	if want := filepath.Join(dir, "crash-20261023T030405Z-host.log"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"dsh-mygo crash report", "source: host", "session: session-1", "pid: 4242", "plugin failed"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in report:\n%s", want, text)
		}
	}
	for _, secret := range []string{"sk-1234567890abcdef", "abcdef1234567890"} {
		if strings.Contains(text, secret) {
			t.Errorf("secret %q leaked into report:\n%s", secret, text)
		}
	}
}

func TestWriteCrashReportAvoidsNameCollisions(t *testing.T) {
	dir := t.TempDir()
	report := crashReport{Time: time.Date(2026, 10, 23, 3, 4, 5, 0, time.UTC), Source: "host", Summary: "first"}
	first, err := writeCrashReport(dir, report)
	if err != nil {
		t.Fatal(err)
	}
	report.Summary = "second"
	second, err := writeCrashReport(dir, report)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("crash reports shared %q", first)
	}
	if !strings.Contains(filepath.Base(second), "-2.log") {
		t.Fatalf("second path = %q", second)
	}
}

func TestFailureWritesCrashReport(t *testing.T) {
	dir := t.TempDir()
	application := New(Options{
		LogDir:           dir,
		PreflightSummary: "Could not prepare the host",
		Preflight: func() error {
			return errors.New("preflight failed with sk-1234567890abcdef")
		},
	})
	application.Start()
	defer application.closeLog()

	deadline := time.Now().Add(3 * time.Second)
	var path string
	for time.Now().Before(deadline) {
		matches, err := filepath.Glob(filepath.Join(dir, "crash-*-app.log"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) > 0 {
			path = matches[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if path == "" {
		t.Fatal("no crash report was written")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "summary: Could not prepare the host") {
		t.Fatalf("report missing summary:\n%s", text)
	}
	if strings.Contains(text, "sk-1234567890abcdef") {
		t.Fatalf("secret leaked:\n%s", text)
	}
}

func TestAppWritesLogFile(t *testing.T) {
	dir := t.TempDir()
	application := New(Options{LogDir: dir})
	application.logf("hello from the log test")
	application.closeLog()

	data, err := os.ReadFile(filepath.Join(dir, "dsh-mygo.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello from the log test") {
		t.Fatalf("log = %q", data)
	}
}
