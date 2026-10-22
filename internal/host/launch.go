package host

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LaunchOptions selects the DeepSeek Harness host command.
type LaunchOptions struct {
	// DshDir is a DeepSeek Harness source checkout. It runs the TypeScript CLI
	// entry through tsx and takes precedence over the installed dsh command.
	DshDir string
	// Profile is the DSH profile to load when DshDir runs a source checkout.
	Profile string
	// CWD becomes the host working directory; it is also the harness session
	// directory unless the caller changes it later.
	CWD string
	// Command overrides the host executable. Args are passed to it unchanged.
	Command string
	// Args are passed to Command when Command is set.
	Args []string
	// DSHHome overrides DSH_HOME for the child process.
	DSHHome string
}

// ResolveLaunch builds the host process configuration for the selected mode.
// A Command override wins over DshDir, which wins over the installed dsh
// executable on PATH.
func ResolveLaunch(options LaunchOptions) (Config, error) {
	environment := os.Environ()
	if options.DSHHome != "" {
		absoluteHome, err := filepath.Abs(options.DSHHome)
		if err != nil {
			return Config{}, fmt.Errorf("resolve DSH_HOME: %w", err)
		}
		environment = setEnv(environment, "DSH_HOME", absoluteHome)
	}
	if options.Command != "" {
		return Config{Command: options.Command, Args: options.Args, Dir: options.CWD, Env: environment}, nil
	}
	if options.DshDir != "" {
		absoluteDir, err := filepath.Abs(options.DshDir)
		if err != nil {
			return Config{}, fmt.Errorf("resolve dsh-dir: %w", err)
		}
		entry := filepath.Join(absoluteDir, "apps", "cli", "src", "bin.ts")
		if _, err := os.Stat(entry); err != nil {
			return Config{}, fmt.Errorf("dsh-dir has no source CLI entry %s: %w", entry, err)
		}
		if options.Profile == "" {
			return Config{}, fmt.Errorf("profile is required for a source checkout")
		}
		return Config{
			Command: "node",
			Args:    []string{"--import", "tsx/esm", entry, "--profile", options.Profile},
			Dir:     absoluteDir,
			Env:     environment,
		}, nil
	}
	if options.Profile == "" {
		return Config{}, fmt.Errorf("profile is required")
	}
	return Config{Command: "dsh", Args: []string{"--profile", options.Profile}, Dir: options.CWD, Env: environment}, nil
}

func setEnv(environment []string, key, value string) []string {
	prefix := strings.ToUpper(key) + "="
	filtered := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(strings.ToUpper(entry), prefix) {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, key+"="+value)
}
