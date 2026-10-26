package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type crashReport struct {
	Time     time.Time
	App      string
	Protocol int
	DSH      string
	Platform string
	Session  string
	Source   string
	Summary  string
	Detail   string
	PID      int
	Stderr   string
}

// writeCrashReport writes one owner-only report and returns its path. Reports
// sharing a second get a numeric suffix instead of overwriting each other.
func writeCrashReport(dir string, report crashReport) (string, error) {
	if dir == "" {
		return "", errors.New("crash report: empty log directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	source := report.Source
	if source == "" {
		source = "app"
	}
	base := fmt.Sprintf("crash-%s-%s", report.Time.UTC().Format("20060102T150405Z"), source)
	content := renderCrashReport(report)
	for attempt := 1; attempt < 1000; attempt++ {
		name := base + ".log"
		if attempt > 1 {
			name = fmt.Sprintf("%s-%d.log", base, attempt)
		}
		path := filepath.Join(dir, name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, writeErr := file.WriteString(content)
		closeErr := file.Close()
		if writeErr != nil {
			return "", writeErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		return path, nil
	}
	return "", errors.New("crash report: too many name collisions")
}

func renderCrashReport(report crashReport) string {
	var builder strings.Builder
	builder.WriteString("dsh-mygo crash report\n")
	fmt.Fprintf(&builder, "time: %s\n", report.Time.UTC().Format(time.RFC3339))
	fmt.Fprintf(&builder, "app: %s\n", report.App)
	fmt.Fprintf(&builder, "protocol: %d\n", report.Protocol)
	fmt.Fprintf(&builder, "dsh: %s\n", report.DSH)
	fmt.Fprintf(&builder, "platform: %s\n", report.Platform)
	fmt.Fprintf(&builder, "source: %s\n", report.Source)
	fmt.Fprintf(&builder, "session: %s\n", report.Session)
	fmt.Fprintf(&builder, "summary: %s\n", redactSecrets(report.Summary))
	if report.PID != 0 {
		fmt.Fprintf(&builder, "pid: %d\n", report.PID)
	}
	builder.WriteString("\ndetail:\n")
	builder.WriteString(redactSecrets(report.Detail))
	builder.WriteString("\n\nstderr tail:\n")
	builder.WriteString(redactSecrets(report.Stderr))
	builder.WriteString("\n")
	return builder.String()
}
