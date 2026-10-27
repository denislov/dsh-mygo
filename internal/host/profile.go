package host

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// EnsureProfile creates the named DSH profile from the bundled host package
// when the profile does not exist. Existing user configuration stays intact;
// an older app-owned Host package is backed up before it is upgraded.
func EnsureProfile(dshHome, bundleDir, profile string) error {
	if dshHome == "" || bundleDir == "" || profile == "" {
		return errors.New("host: profile seed requires a home, bundle, and profile name")
	}
	if profile == "." || profile == ".." || strings.ContainsAny(profile, `:/\`) {
		return errors.New("host: invalid profile name")
	}
	profileDir := filepath.Join(dshHome, "profiles", profile)
	if _, err := os.Stat(filepath.Join(profileDir, "package.json")); err == nil {
		return upgradeHostBundle(profileDir, bundleDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("host: inspect profile %s: %w", profileDir, err)
	}
	for _, required := range []string{"package.json", "index.mjs"} {
		path := filepath.Join(bundleDir, required)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return fmt.Errorf("host: bundled host bundle is incomplete: %s", path)
		}
	}

	profilesDir := filepath.Join(dshHome, "profiles")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		return fmt.Errorf("host: create profiles directory: %w", err)
	}
	staging, err := os.MkdirTemp(profilesDir, "."+profile+"-")
	if err != nil {
		return fmt.Errorf("host: create profile staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	hostTarget := filepath.Join(staging, "node_modules", "@dsh-mygo", "host")
	if err := copyTree(bundleDir, hostTarget); err != nil {
		return fmt.Errorf("host: copy host bundle into profile: %w", err)
	}
	manifest := struct {
		Name    string `json:"name"`
		Private bool   `json:"private"`
		Type    string `json:"type"`
		DSH     struct {
			Profile struct {
				Bundles []string `json:"bundles"`
			} `json:"profile"`
		} `json:"dsh"`
	}{Name: "dsh-profile-" + profile, Private: true, Type: "module"}
	manifest.DSH.Profile.Bundles = []string{"@deepseek-ai/dsh-base", "@dsh-mygo/host"}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(staging, "package.json"), data, 0o644); err != nil {
		return fmt.Errorf("host: write profile manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "cordis.patch.yml"), []byte("[]\n"), 0o644); err != nil {
		return fmt.Errorf("host: write profile patch: %w", err)
	}

	if err := os.Rename(staging, profileDir); err != nil {
		// Another instance may have created the profile while we staged ours.
		if _, statErr := os.Stat(filepath.Join(profileDir, "package.json")); statErr == nil {
			return nil
		}
		return fmt.Errorf("host: create profile %s: %w", profileDir, err)
	}
	return nil
}

func upgradeHostBundle(profileDir, bundleDir string) error {
	readIdentity := func(path string) (string, string, error) {
		data, err := os.ReadFile(filepath.Join(path, "package.json"))
		if err != nil {
			return "", "", err
		}
		var identity struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if json.Unmarshal(data, &identity) != nil || identity.Version == "" {
			return "", "", errors.New("host: invalid Host package identity")
		}
		return identity.Name, identity.Version, nil
	}
	if _, err := os.Stat(filepath.Join(bundleDir, "package.json")); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	sourceName, _, err := readIdentity(bundleDir)
	if err != nil || sourceName != "@dsh-mygo/host" {
		return errors.New("host: bundled Host package identity is invalid")
	}
	scope := filepath.Join(profileDir, "node_modules", "@dsh-mygo")
	target := filepath.Join(scope, "host")
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return errors.New("host: profile Host package is not an app-owned directory")
	}
	targetName, _, err := readIdentity(target)
	if err != nil || targetName != sourceName {
		return errors.New("host: cannot verify the installed profile Host package")
	}
	compareTarget := target
	if info.Mode()&os.ModeSymlink != 0 {
		compareTarget, err = filepath.EvalSymlinks(target)
		if err != nil {
			return errors.New("host: cannot resolve the linked development Host")
		}
	}
	// The bundle version is release identity, not a content fingerprint. A
	// development build can change Host code without changing the release
	// version, so an app-owned copy is synchronized by content on every start.
	sourceDigest, err := bundleDigest(bundleDir)
	if err != nil {
		return fmt.Errorf("host: read bundled Host: %w", err)
	}
	targetDigest, compareErr := bundleDigest(compareTarget)
	if compareErr == nil && sourceDigest == targetDigest {
		return nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("host: linked development Host differs from the bundled Host; update its source before starting")
	}
	if info, err := os.Stat(filepath.Join(bundleDir, "index.mjs")); err != nil || !info.Mode().IsRegular() {
		return errors.New("host: bundled Host entry is missing; the installed package was retained")
	}
	resolvedProfile, err := filepath.EvalSymlinks(profileDir)
	if err != nil {
		return err
	}
	resolvedScope, err := filepath.EvalSymlinks(scope)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(resolvedProfile, resolvedScope)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("host: profile Host package points outside its profile")
	}
	target = filepath.Join(resolvedScope, "host")
	staging, err := os.MkdirTemp(resolvedScope, ".host-next-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := copyTree(bundleDir, staging); err != nil {
		return err
	}
	backup, err := os.MkdirTemp(resolvedScope, ".host-backup-")
	if err != nil {
		return err
	}
	if err := os.Remove(backup); err != nil {
		return err
	}
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	if err := os.Rename(staging, target); err != nil {
		if rollbackErr := os.Rename(backup, target); rollbackErr != nil {
			return errors.New("host: Host upgrade failed; the previous package is retained in the profile backup")
		}
		return err
	}
	return nil
}

// bundleDigest identifies one Host bundle by relative paths and file bytes.
// Versions name releases; this digest decides whether an app-owned profile
// copy still matches the bundle shipped by the running app.
func bundleDigest(root string) (string, error) {
	digest := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == "node_modules" {
			return filepath.SkipDir
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			fmt.Fprintf(digest, "d %s\n", relative)
			return nil
		}
		if !entry.Type().IsRegular() && entry.Type()&fs.ModeSymlink == 0 {
			return fmt.Errorf("host: bundle contains unsupported file %s", relative)
		}
		// copyTree follows file links, so the digest compares their content.
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(digest, "f %s %d\n", relative, len(data))
		_, err = digest.Write(data)
		return err
	})
	return fmt.Sprintf("%x", digest.Sum(nil)), err
}

// copyTree copies regular files and directories, skipping dependency trees.
func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == "node_modules" {
			return filepath.SkipDir
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, info.Mode().Perm())
	})
}
