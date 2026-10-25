// Package workspace provides bounded, read-only access to the active project.
package workspace

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const MaxPreviewBytes = 512 * 1024
const MaxEntries = 2000

type Entry struct {
	Path, Name string
	Directory  bool
}
type File struct {
	Path, Text string
	Image      []byte
}

// Resolve checks the actual target as well as its lexical path. Symlinks to
// other projects never grant the file browser access outside its root.
func Resolve(root, relative string) (string, error) {
	invalid := errors.New("File is outside the working folder")
	if !filepath.IsAbs(root) || filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", invalid
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", errors.New("Working folder is unavailable")
	}
	path, err := filepath.EvalSymlinks(filepath.Join(base, relative))
	if err != nil {
		return "", errors.New("File is unavailable")
	}
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", invalid
	}
	return path, nil
}

func ReadDir(root, relative string) ([]Entry, error) {
	path, err := Resolve(root, relative)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("Could not read folder")
	}
	defer f.Close()
	entries, err := f.ReadDir(MaxEntries + 1)
	if err != nil && err != io.EOF {
		return nil, errors.New("Could not read folder")
	}
	if len(entries) > MaxEntries {
		return nil, errors.New("Folder contains too many entries")
	}
	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		rel := filepath.Join(relative, entry.Name())
		target, err := Resolve(root, rel)
		if err != nil {
			continue
		}
		info, err := os.Stat(target)
		if err != nil {
			continue
		}
		result = append(result, Entry{Path: filepath.ToSlash(rel), Name: entry.Name(), Directory: info.IsDir()})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Directory != result[j].Directory {
			return result[i].Directory
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result, nil
}

func ReadFile(root, relative string) (File, error) {
	path, err := Resolve(root, relative)
	if err != nil {
		return File{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return File{}, errors.New("Could not read file")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return File{}, errors.New("File cannot be previewed")
	}
	if info.Size() > MaxPreviewBytes {
		return File{}, errors.New("File is too large to preview")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxPreviewBytes+1))
	if err != nil || len(data) > MaxPreviewBytes {
		return File{}, errors.New("File is too large to preview")
	}
	result := File{Path: relative}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		result.Image = data
	default:
		if !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') {
			return File{}, errors.New("Binary file cannot be previewed")
		}
		result.Text = string(data)
	}
	return result, nil
}
