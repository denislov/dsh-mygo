// Package platform isolates small operating-system integrations.
package platform

import (
	"os/exec"
	"runtime"
)

// Reveal opens a directory in the platform file manager without waiting for it
// to close.
func Reveal(path string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("explorer.exe", path)
	case "darwin":
		command = exec.Command("open", path)
	default:
		command = exec.Command("xdg-open", path)
	}
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}
