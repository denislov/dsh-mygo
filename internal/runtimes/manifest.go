// Package runtimes owns installed DSH runtime bundles, their catalog and
// launch metadata. It never starts the host itself; callers turn a validated
// runtime into a host.Config.
package runtimes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"dsh-mygo/internal/version"
)

const (
	manifestName        = "manifest.json"
	hostBundleName      = "host"
	entryName           = "mygo-host.mjs"
	maxManifestBytes    = 64 << 10
	maxCatalogBytes     = 1 << 20
	maxCatalogRuntimes  = 128
	maxArchiveBytes     = 1 << 30
	maxExtractedBytes   = 4 << 30
	maxExtractedEntries = 250_000
	maxArchiveFileBytes = 512 << 20
	maxPathBytes        = 1024
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,160}$`)
var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Manifest is the runtime bundle identity written by the build and verified
// before installation and on every start.
type Manifest struct {
	SchemaVersion   int    `json:"schemaVersion"`
	Kind            string `json:"kind"`
	ID              string `json:"id"`
	DSH             string `json:"dsh"`
	Node            string `json:"node"`
	Platform        string `json:"platform"`
	Arch            string `json:"arch"`
	ProtocolVersion int    `json:"protocolVersion"`
	HostVersion     string `json:"hostVersion"`
	AppMin          string `json:"appMin"`
	AppMaxExclusive string `json:"appMaxExclusive"`
	Entry           string `json:"entry"`
	HostBundle      string `json:"hostBundle"`
}

// CatalogEntry is one downloadable runtime in runtime-catalog.json.
type CatalogEntry struct {
	ID              string `json:"id"`
	DSH             string `json:"dsh"`
	Node            string `json:"node"`
	Platform        string `json:"platform"`
	Arch            string `json:"arch"`
	ProtocolVersion int    `json:"protocolVersion"`
	HostVersion     string `json:"hostVersion"`
	AppMin          string `json:"appMin"`
	AppMaxExclusive string `json:"appMaxExclusive"`
	Asset           string `json:"asset"`
	SHA256          string `json:"sha256"`
	Size            int64  `json:"size"`
}

// Catalog is the release manifest listing compatible runtime assets.
type Catalog struct {
	SchemaVersion int            `json:"schemaVersion"`
	Runtimes      []CatalogEntry `json:"runtimes"`
}

// Expected is the identity the app requires from a runtime. Platform and Arch
// are the manifest spellings (win32/x64), not Go's GOOS/GOARCH.
type Expected struct {
	Platform string
	Arch     string
	Protocol int
}

func CurrentExpected() Expected {
	return Expected{Platform: platformName(runtime.GOOS), Arch: archName(runtime.GOARCH), Protocol: version.Protocol}
}

func platformName(goos string) string {
	switch goos {
	case "windows":
		return "win32"
	case "darwin":
		return "darwin"
	case "linux":
		return "linux"
	default:
		return goos
	}
}

func archName(goarch string) string {
	switch goarch {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	default:
		return goarch
	}
}

func platformMatches(value string) bool {
	switch runtime.GOOS {
	case "windows":
		return value == "win32"
	default:
		return value == runtime.GOOS
	}
}

func archMatches(value string) bool {
	switch runtime.GOARCH {
	case "amd64":
		return value == "x64"
	default:
		return value == runtime.GOARCH
	}
}

// RuntimeID derives the stable installation id from the bundle identity.
func RuntimeID(manifest Manifest) string {
	return fmt.Sprintf("dsh-%s-node-%s-host-%s-%s-%s", manifest.DSH, manifest.Node, manifest.HostVersion, manifest.Platform, manifest.Arch)
}

// ParseManifest decodes a bounded, validated runtime manifest.
func ParseManifest(data []byte, expected Expected) (Manifest, error) {
	var manifest Manifest
	if len(data) == 0 || len(data) > maxManifestBytes {
		return manifest, fmt.Errorf("invalid runtime manifest size")
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("invalid runtime manifest")
	}
	if err := validateManifest(manifest, expected); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func validateManifest(manifest Manifest, expected Expected) error {
	if manifest.SchemaVersion != 1 || manifest.Kind != "dsh-runtime" {
		return fmt.Errorf("unsupported runtime manifest schema")
	}
	if !identifierPattern.MatchString(manifest.ID) || manifest.ID != RuntimeID(manifest) {
		return fmt.Errorf("runtime manifest id is invalid")
	}
	for _, value := range []string{manifest.DSH, manifest.Node, manifest.HostVersion} {
		if strings.TrimSpace(value) == "" || len(value) > 128 {
			return fmt.Errorf("runtime manifest is missing version fields")
		}
	}
	if expected.Platform != "" && manifest.Platform != expected.Platform {
		return fmt.Errorf("runtime platform %s does not match %s", manifest.Platform, expected.Platform)
	}
	if expected.Arch != "" && manifest.Arch != expected.Arch {
		return fmt.Errorf("runtime architecture %s does not match %s", manifest.Arch, expected.Arch)
	}
	if !platformMatches(manifest.Platform) || !archMatches(manifest.Arch) {
		return fmt.Errorf("runtime targets an unsupported platform")
	}
	if expected.Protocol != 0 && manifest.ProtocolVersion != expected.Protocol {
		return fmt.Errorf("runtime protocol %d does not match %d", manifest.ProtocolVersion, expected.Protocol)
	}
	compatible, err := version.AppVersionInRange(version.App, manifest.AppMin, manifest.AppMaxExclusive)
	if err != nil || !compatible {
		return fmt.Errorf("runtime does not support this app version: %s", version.App)
	}
	for _, path := range []string{manifest.Entry, manifest.HostBundle} {
		if !safeRelativePath(path) {
			return fmt.Errorf("runtime manifest path is invalid")
		}
	}
	if manifest.HostBundle != hostBundleName {
		return fmt.Errorf("runtime manifest host bundle is unsupported")
	}
	return nil
}

func safeRelativePath(value string) bool {
	if value == "" || len(value) > 256 || !fs.ValidPath(value) || value == "." {
		return false
	}
	if strings.ContainsAny(value, `\:`) || strings.HasPrefix(value, "..") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || windowsReserved(part) {
			return false
		}
	}
	return true
}

func windowsReserved(name string) bool {
	base := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}

// ReadManifest reads and validates manifest.json in a runtime directory.
func ReadManifest(dir string, expected Expected) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return Manifest{}, fmt.Errorf("read runtime manifest: %w", err)
	}
	return ParseManifest(data, expected)
}

// ValidateInstalled verifies the runtime tree without executing anything.
func ValidateInstalled(dir string, expected Expected) (Manifest, error) {
	manifest, err := ReadManifest(dir, expected)
	if err != nil {
		return manifest, err
	}
	entry := filepath.Join(dir, filepath.FromSlash(manifest.Entry))
	if info, err := os.Stat(entry); err != nil || !info.Mode().IsRegular() {
		return manifest, fmt.Errorf("runtime entry is missing")
	}
	node := bundledNodePath(dir)
	if node == "" {
		return manifest, fmt.Errorf("runtime Node is missing")
	}
	dshPackage := filepath.Join(dir, "node_modules", "@deepseek-ai", "dsh", "package.json")
	var dsh struct {
		Version string `json:"version"`
	}
	if err := readJSON(dshPackage, &dsh); err != nil {
		return manifest, fmt.Errorf("runtime DSH package is invalid: %w", err)
	}
	if dsh.Version != manifest.DSH {
		return manifest, fmt.Errorf("runtime DSH %s does not match manifest %s", dsh.Version, manifest.DSH)
	}
	hostDir := filepath.Join(dir, filepath.FromSlash(manifest.HostBundle))
	var host struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Engines struct {
			DSH string `json:"dsh"`
		} `json:"engines"`
		DSHMygo struct {
			ProtocolVersion  int `json:"protocolVersion"`
			AppCompatibility struct {
				Min          string `json:"min"`
				MaxExclusive string `json:"maxExclusive"`
			} `json:"appCompatibility"`
		} `json:"dshMygo"`
	}
	if err := readJSON(filepath.Join(hostDir, "package.json"), &host); err != nil {
		return manifest, fmt.Errorf("runtime Host bundle is invalid: %w", err)
	}
	if host.Name != "@dsh-mygo/host" || host.Version != manifest.HostVersion ||
		host.Engines.DSH != manifest.DSH || host.DSHMygo.ProtocolVersion != manifest.ProtocolVersion ||
		host.DSHMygo.AppCompatibility.Min != manifest.AppMin || host.DSHMygo.AppCompatibility.MaxExclusive != manifest.AppMaxExclusive {
		return manifest, fmt.Errorf("runtime Host bundle identity does not match the manifest")
	}
	for _, path := range []string{filepath.Join(hostDir, "index.mjs"), filepath.Join(dir, "mygo-host.mjs")} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return manifest, fmt.Errorf("runtime is incomplete: missing %s", filepath.Base(path))
		}
	}
	return manifest, nil
}

func bundledNodePath(dir string) string {
	for _, candidate := range []string{
		filepath.Join(dir, "node", "node.exe"),
		filepath.Join(dir, "node", "node"),
		filepath.Join(dir, "node", "bin", "node"),
	} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) > maxManifestBytes {
		return errors.New("file is too large")
	}
	return json.Unmarshal(data, value)
}

// ParseCatalog decodes the release catalog and returns only runtimes this app
// can install on this machine.
func ParseCatalog(data []byte, expected Expected) (Catalog, error) {
	var catalog Catalog
	if len(data) == 0 || len(data) > maxCatalogBytes {
		return catalog, fmt.Errorf("invalid runtime catalog size")
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return catalog, fmt.Errorf("invalid runtime catalog")
	}
	if catalog.SchemaVersion != 1 || len(catalog.Runtimes) == 0 || len(catalog.Runtimes) > maxCatalogRuntimes {
		return catalog, fmt.Errorf("unsupported runtime catalog")
	}
	seen := make(map[string]bool, len(catalog.Runtimes))
	filtered := catalog.Runtimes[:0]
	for _, entry := range catalog.Runtimes {
		if !identifierPattern.MatchString(entry.ID) || seen[entry.ID] {
			return catalog, fmt.Errorf("invalid or duplicate runtime id")
		}
		seen[entry.ID] = true
		if entry.Platform != expected.Platform || entry.Arch != expected.Arch || entry.ProtocolVersion != expected.Protocol {
			continue
		}
		if !platformMatches(entry.Platform) || !archMatches(entry.Arch) {
			continue
		}
		if compatible, err := version.AppVersionInRange(version.App, entry.AppMin, entry.AppMaxExclusive); err != nil || !compatible {
			continue
		}
		if entry.ID != RuntimeID(Manifest{
			DSH: entry.DSH, Node: entry.Node, HostVersion: entry.HostVersion,
			Platform: entry.Platform, Arch: entry.Arch,
		}) {
			return catalog, fmt.Errorf("runtime catalog id does not match its identity")
		}
		if !sha256Pattern.MatchString(entry.SHA256) || entry.Size <= 0 || entry.Size > maxArchiveBytes {
			return catalog, fmt.Errorf("runtime catalog checksum is invalid")
		}
		if !validAssetName(entry.Asset) {
			return catalog, fmt.Errorf("runtime catalog asset name is invalid")
		}
		filtered = append(filtered, entry)
	}
	catalog.Runtimes = filtered
	return catalog, nil
}

func validAssetName(name string) bool {
	return regexp.MustCompile(`^dsh-mygo-runtime-[A-Za-z0-9._-]+\.tgz$`).MatchString(name) && !strings.ContainsAny(name, `/\:`)
}
