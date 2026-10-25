package workspace

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

type Change struct{ Path, Status string }
type Changes struct {
	Branch string
	Files  []Change
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1024*1024 {
		return 0, errors.New("Git output exceeds the preview limit")
	}
	return b.Buffer.Write(p)
}

func git(ctx context.Context, root string, args ...string) (string, error) {
	path, err := Resolve(root, "")
	if err != nil {
		return "", err
	}
	args = append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = path
	out := &boundedOutput{}
	cmd.Stdout = out
	if cmd.Run() != nil {
		return "", errors.New("Git information is unavailable")
	}
	return out.String(), nil
}

func ReadChanges(ctx context.Context, root string) (Changes, error) {
	raw, err := git(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		return Changes{}, err
	}
	branch, _ := git(ctx, root, "branch", "--show-current")
	result := Changes{Branch: strings.TrimSpace(branch)}
	fields := strings.Split(raw, "\x00")
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if len(field) < 4 {
			continue
		}
		result.Files = append(result.Files, Change{Status: field[:2], Path: field[3:]})
		if strings.ContainsAny(field[:2], "RC") {
			i++
		}
		if len(result.Files) > MaxEntries {
			return Changes{}, errors.New("Too many changed files")
		}
	}
	return result, nil
}

func ReadDiff(ctx context.Context, root, path string) (string, error) {
	// Deleted paths cannot be resolved; their lexical location still must stay
	// inside the root. Git treats the final argument literally as a pathspec.
	if path == "" || strings.ContainsAny(path, "\x00\r\n") || strings.Contains(path, ":") || strings.HasPrefix(path, "/") || strings.Contains(strings.ReplaceAll(path, "\\", "/"), "../") || path == ".." {
		return "", errors.New("Invalid changed file")
	}
	return git(ctx, root, "--literal-pathspecs", "diff", "--no-ext-diff", "--no-textconv", "--no-color", "HEAD", "--", path)
}
