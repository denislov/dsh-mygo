package runtimes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"dsh-mygo/internal/version"
)

const minNodeMajor = 24

var versionPattern = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)(?:-[0-9A-Za-z.-]+)?`)

// External is a validated existing DSH command and its Node runtime.
type External struct {
	Command     string
	Node        string
	DSHVersion  string
	NodeVersion string
	Compatible  bool
	Reason      string
}

// Runtime converts a compatible external installation into a store entry.
func (e External) Runtime() Runtime {
	sum := sha256.Sum256([]byte(strings.ToLower(e.Command)))
	return Runtime{
		ID:           "external-" + hex.EncodeToString(sum[:6]),
		Kind:         KindExternal,
		Command:      e.Command,
		NodePath:     e.Node,
		DSH:          e.DSHVersion,
		Node:         e.NodeVersion,
		HostVersion:  version.App,
		DisplayLabel: "Existing DSH installation",
	}
}

// ProbeExternal checks the dsh command on PATH (or the supplied path) and its
// Node runtime. Version mismatches are reported in the result rather than as
// an error so the setup UI can explain them.
func ProbeExternal(ctx context.Context, command string) (External, error) {
	if command == "" {
		found, err := exec.LookPath("dsh")
		if err != nil {
			return External{}, fmt.Errorf("dsh was not found on PATH")
		}
		command = found
	}
	absolute, err := filepath.Abs(command)
	if err != nil {
		return External{}, fmt.Errorf("resolve dsh command: %w", err)
	}
	absolute = stablePath(absolute)
	if strings.ContainsAny(absolute, "\"\x00") {
		return External{}, fmt.Errorf("dsh command contains unsupported characters")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return External{}, fmt.Errorf("Node.js %d+ was not found on PATH", minNodeMajor)
	}
	node, err = filepath.Abs(node)
	if err != nil {
		return External{}, fmt.Errorf("resolve Node: %w", err)
	}
	node = stablePath(node)
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	nodeOutput, err := runVersion(probeCtx, node, "--version")
	if err != nil {
		return External{}, fmt.Errorf("could not read the Node version")
	}
	nodeVersion, err := parseVersion(nodeOutput)
	if err != nil {
		return External{}, fmt.Errorf("could not parse the Node version")
	}
	dshOutput, err := runVersion(probeCtx, absolute, "--version")
	if err != nil {
		return External{}, fmt.Errorf("could not read the dsh version")
	}
	dshVersion, err := parseVersion(dshOutput)
	if err != nil {
		return External{}, fmt.Errorf("could not parse the dsh version")
	}
	result := External{Command: absolute, Node: node, DSHVersion: dshVersion, NodeVersion: nodeVersion, Compatible: true}
	major, _ := strconv.Atoi(strings.Split(nodeVersion, ".")[0])
	if major < minNodeMajor {
		result.Compatible = false
		result.Reason = fmt.Sprintf("Node %s is too old; %d+ is required", nodeVersion, minNodeMajor)
	}
	if dshVersion != version.DSH {
		result.Compatible = false
		result.Reason = fmt.Sprintf("DSH %s is installed; this app requires %s", dshVersion, version.DSH)
	}
	return result, nil
}

// stablePath resolves junction and symlink components so a version-manager
// shim directory that is recreated per shell does not get persisted.
func stablePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" {
		return resolved
	}
	return path
}

func runVersion(ctx context.Context, command string, args ...string) (string, error) {
	var cmd *exec.Cmd
	switch strings.ToLower(filepath.Ext(command)) {
	case ".cmd", ".bat":
		cmdArgs := append([]string{"/d", "/s", "/c", "call", command}, args...)
		cmd = exec.CommandContext(ctx, "cmd.exe", cmdArgs...)
	default:
		cmd = exec.CommandContext(ctx, command, args...)
	}
	output, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && len(exit.Stderr) > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exit.Stderr)))
		}
		return "", err
	}
	return string(output), nil
}

func parseVersion(output string) (string, error) {
	match := versionPattern.FindString(output)
	if match == "" {
		return "", fmt.Errorf("no version found")
	}
	return strings.TrimPrefix(match, "v"), nil
}
