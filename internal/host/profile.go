package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// EnsureProfile creates the named DSH profile from the bundled host package
// when the profile does not exist. It never overwrites an existing profile.
func EnsureProfile(dshHome, bundleDir, profile string) error {
	if dshHome == "" || bundleDir == "" || profile == "" {
		return errors.New("host: profile seed requires a home, bundle, and profile name")
	}
	profileDir := filepath.Join(dshHome, "profiles", profile)
	if _, err := os.Stat(filepath.Join(profileDir, "package.json")); err == nil {
		return nil
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
