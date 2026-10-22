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
	// ResourcesDir is the app's resources directory. A bundled runtime there
	// takes precedence over a dsh executable on PATH.
	ResourcesDir string
	// DSHHome overrides DSH_HOME for the child process.
	DSHHome string
}

// ResolveLaunch builds the host process configuration for the selected mode.
// A Command override wins over DshDir, which wins over a bundled runtime under
// ResourcesDir, which wins over the installed dsh executable on PATH.
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
	if options.ResourcesDir != "" {
		if config, ok, err := bundledLaunch(options.ResourcesDir, options.Profile, options.CWD, environment); err != nil {
			return Config{}, err
		} else if ok {
			return config, nil
		}
	}
	return Config{Command: "dsh", Args: []string{"--profile", options.Profile}, Dir: options.CWD, Env: environment}, nil
}

// bundledLaunch selects the private mygo-host entry, then the bundled dsh CLI.
// A partial runtime is an error so a packaged app never silently falls back to
// whatever dsh happens to be on PATH.
func bundledLaunch(resourcesDir, profile, cwd string, environment []string) (Config, bool, error) {
	runtimeDir := filepath.Join(resourcesDir, "runtime")
	hostEntry := filepath.Join(runtimeDir, "mygo-host.mjs")
	if isFile(hostEntry) {
		node, err := bundledNode(runtimeDir)
		if err != nil {
			return Config{}, false, err
		}
		return Config{
			Command: node,
			Args:    []string{hostEntry, profile},
			Dir:     cwd,
			Env:     prependPath(environment, filepath.Dir(node)),
		}, true, nil
	}
	dshEntry := filepath.Join(runtimeDir, "node_modules", "@deepseek-ai", "dsh", "lib", "bin.js")
	if isFile(dshEntry) {
		node, err := bundledNode(runtimeDir)
		if err != nil {
			return Config{}, false, err
		}
		return Config{
			Command: node,
			Args:    []string{dshEntry, "--profile", profile},
			Dir:     cwd,
			Env:     prependPath(environment, filepath.Dir(node)),
		}, true, nil
	}
	return Config{}, false, nil
}

func bundledNode(runtimeDir string) (string, error) {
	for _, candidate := range []string{
		filepath.Join(runtimeDir, "node", "node.exe"),
		filepath.Join(runtimeDir, "node", "node"),
		filepath.Join(runtimeDir, "node", "bin", "node"),
	} {
		if isFile(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("bundled runtime is incomplete: missing Node under %s", filepath.Join(runtimeDir, "node"))
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func prependPath(environment []string, dir string) []string {
	current := ""
	for _, entry := range environment {
		if strings.HasPrefix(strings.ToUpper(entry), "PATH=") {
			current = entry[len("PATH="):]
			break
		}
	}
	value := dir
	if current != "" {
		value += string(os.PathListSeparator) + current
	}
	return setEnv(environment, "PATH", value)
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
