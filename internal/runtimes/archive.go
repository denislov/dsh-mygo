package runtimes

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InstallArchive extracts a runtime bundle, validates it and records an
// installed entry. The selected runtime is not changed.
func (m *Manager) InstallArchive(ctx context.Context, archivePath string, progress func(float32)) (Runtime, error) {
	info, err := os.Stat(archivePath)
	if err != nil {
		return Runtime{}, fmt.Errorf("read runtime archive: %w", err)
	}
	if info.Size() <= 0 || info.Size() > maxArchiveBytes {
		return Runtime{}, fmt.Errorf("runtime archive size is invalid")
	}
	manifest, err := readArchiveManifest(archivePath)
	if err != nil {
		return Runtime{}, err
	}
	target := filepath.Join(m.Root, manifest.ID)
	if existing, err := os.Stat(target); err == nil && existing.IsDir() {
		if _, validateErr := ValidateInstalled(target, CurrentExpected()); validateErr != nil {
			return Runtime{}, fmt.Errorf("runtime %s is already installed but invalid; remove it first", manifest.ID)
		}
		m.mu.Lock()
		found, ok := m.findLocked(manifest.ID)
		m.mu.Unlock()
		if ok {
			return found, nil
		}
		return runtimeFromManifest(manifest, target), nil
	}
	if err := os.MkdirAll(m.Root, 0o755); err != nil {
		return Runtime{}, fmt.Errorf("create runtime store: %w", err)
	}
	staging, err := os.MkdirTemp(m.Root, ".staging-")
	if err != nil {
		return Runtime{}, fmt.Errorf("create runtime staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	if progress != nil {
		progress(0)
	}
	if err := extractArchive(ctx, archivePath, staging); err != nil {
		return Runtime{}, err
	}
	if _, err := ValidateInstalled(staging, CurrentExpected()); err != nil {
		return Runtime{}, err
	}
	if err := os.Rename(staging, target); err != nil {
		return Runtime{}, fmt.Errorf("commit runtime: %w", err)
	}
	installed := runtimeFromManifest(manifest, target)
	m.mu.Lock()
	m.state.Runtimes = append(m.state.Runtimes, installed)
	err = m.saveLocked()
	if err != nil {
		m.state.Runtimes = m.state.Runtimes[:len(m.state.Runtimes)-1]
	}
	m.mu.Unlock()
	if err != nil {
		_ = os.RemoveAll(target)
		return Runtime{}, err
	}
	if progress != nil {
		progress(1)
	}
	return installed, nil
}

func runtimeFromManifest(manifest Manifest, dir string) Runtime {
	return Runtime{
		ID:          manifest.ID,
		Kind:        KindManaged,
		Dir:         dir,
		DSH:         manifest.DSH,
		Node:        manifest.Node,
		HostVersion: manifest.HostVersion,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func readArchiveManifest(archivePath string) (Manifest, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return Manifest{}, fmt.Errorf("open runtime archive: %w", err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return Manifest{}, fmt.Errorf("runtime archive is not gzip")
	}
	defer reader.Close()
	tarReader := tar.NewReader(reader)
	header, err := tarReader.Next()
	if err != nil {
		return Manifest{}, fmt.Errorf("runtime archive is empty")
	}
	if header.Name != manifestName || (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) {
		return Manifest{}, fmt.Errorf("runtime archive must start with manifest.json")
	}
	if header.Size <= 0 || header.Size > maxManifestBytes {
		return Manifest{}, fmt.Errorf("runtime manifest size is invalid")
	}
	data, err := io.ReadAll(io.LimitReader(tarReader, maxManifestBytes+1))
	if err != nil || int64(len(data)) != header.Size {
		return Manifest{}, fmt.Errorf("could not read runtime manifest")
	}
	return ParseManifest(data, CurrentExpected())
}

func extractArchive(ctx context.Context, archivePath, dest string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open runtime archive: %w", err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("runtime archive is not gzip")
	}
	defer reader.Close()
	tarReader := tar.NewReader(reader)
	seen := make(map[string]bool)
	entries := 0
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read runtime archive: %w", err)
		}
		if header.Typeflag == tar.TypeXHeader || header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		entries++
		if entries > maxExtractedEntries {
			return fmt.Errorf("runtime archive contains too many entries")
		}
		name := header.Name
		if header.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
		}
		if err := validateArchivePath(name); err != nil {
			return err
		}
		key := strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("runtime archive contains duplicate paths")
		}
		seen[key] = true
		target := filepath.Join(dest, filepath.FromSlash(name))
		if !within(dest, target) {
			return fmt.Errorf("runtime archive path escapes the staging directory")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create runtime directory: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maxArchiveFileBytes {
				return fmt.Errorf("runtime archive contains an oversized file")
			}
			if total+header.Size > maxExtractedBytes {
				return fmt.Errorf("runtime archive expands beyond the allowed size")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create runtime directory: %w", err)
			}
			mode := os.FileMode(0o644)
			if header.FileInfo().Mode()&0o111 != 0 {
				mode = 0o755
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode)
			if err != nil {
				return fmt.Errorf("create runtime file: %w", err)
			}
			written, copyErr := io.CopyN(out, tarReader, header.Size)
			closeErr := out.Close()
			if copyErr != nil || written != header.Size {
				return fmt.Errorf("extract runtime file: %w", copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close runtime file: %w", closeErr)
			}
			total += header.Size
		default:
			return fmt.Errorf("runtime archive contains an unsupported entry type")
		}
	}
	if entries == 0 {
		return fmt.Errorf("runtime archive is empty")
	}
	return nil
}

func validateArchivePath(name string) error {
	if name == "" || len(name) > maxPathBytes || strings.ContainsRune(name, 0) || strings.ContainsAny(name, "\\:") {
		return fmt.Errorf("runtime archive contains an invalid path")
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return fmt.Errorf("runtime archive contains an absolute path")
	}
	if !fs.ValidPath(name) {
		return fmt.Errorf("runtime archive contains an invalid path")
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || windowsReserved(part) {
			return fmt.Errorf("runtime archive contains an unsafe path")
		}
		for _, r := range part {
			if r < 0x20 || r == 0x7f {
				return fmt.Errorf("runtime archive contains an invalid path")
			}
		}
	}
	return nil
}

func within(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
