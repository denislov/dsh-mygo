package app

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

// diagnostics owns the log file, its lock and the redaction policy. Callers
// pass already-bounded summaries and details; credentials never appear in the
// formatted reports.
type diagnostics struct {
	dir    string
	mu     sync.Mutex
	logger *log.Logger
	file   *os.File
}

func newDiagnostics(dir string) *diagnostics {
	diagnostics := &diagnostics{dir: dir}
	diagnostics.open()
	return diagnostics
}

func (d *diagnostics) open() {
	if d == nil || d.dir == "" {
		return
	}
	if err := os.MkdirAll(d.dir, 0o755); err != nil {
		d.logger = log.New(os.Stderr, "", log.LstdFlags|log.LUTC)
		d.logger.Printf("create log directory %s: %v", d.dir, err)
		return
	}
	path := filepath.Join(d.dir, "dsh-mygo.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		d.logger = log.New(os.Stderr, "", log.LstdFlags|log.LUTC)
		d.logger.Printf("open log %s: %v", path, err)
		return
	}
	d.file = file
	d.logger = log.New(file, "", log.LstdFlags|log.LUTC)
}

func (d *diagnostics) logf(format string, args ...any) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.logger != nil {
		d.logger.Printf(format, args...)
	}
}

func (d *diagnostics) close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file != nil {
		_ = d.file.Close()
		d.file = nil
	}
	d.logger = nil
}

// capture writes one crash report off the main thread. The file writer owns
// name allocation and redaction of summary, detail and stderr.
func (d *diagnostics) capture(report crashReport) {
	if d == nil || d.dir == "" {
		return
	}
	go func() {
		d.logf("failure: %s", report.Summary)
		if report.Detail != "" {
			d.logf("detail: %s", redactSecrets(report.Detail))
		}
		path, err := writeCrashReport(d.dir, report)
		if err != nil {
			d.logf("crash report failed: %v", err)
			return
		}
		d.logf("crash report: %s", path)
	}()
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])(api[_-]?key|token|secret|password)\s*[:=]\s*["']?[A-Za-z0-9._~+/=-]{6,}["']?`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/-]{6,}=*`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{6,}`),
}

// redactSecrets removes credential-shaped values before they reach a log or a
// crash report. It deliberately does not attempt to classify ordinary text.
func redactSecrets(text string) string {
	for index, pattern := range secretPatterns {
		if index == 0 {
			text = pattern.ReplaceAllString(text, "${1}[REDACTED]")
			continue
		}
		text = pattern.ReplaceAllString(text, "[REDACTED]")
	}
	return text
}
