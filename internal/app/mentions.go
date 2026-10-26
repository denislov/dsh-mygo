package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// mentionCandidates bounds one completion list.
const mentionCandidates = 40

// QueryMentions lists paths under the session working directory that complete
// the typed prefix. Reads run on a goroutine and publish through Post; results
// for a superseded prefix or another session are dropped.
func (a *App) QueryMentions(prefix string) {
	if a.options.SDKProtocol {
		return
	}
	cwd := a.model.SessionCWD
	sessionID := a.attachments.session()
	seq := a.attachments.nextMention()
	go func() {
		candidates := listMentionCandidates(cwd, prefix)
		a.post(func() {
			if a.shuttingDown.Load() || !a.attachments.acceptMention(seq, sessionID) {
				return
			}
			a.model.Mention.Candidates = candidates
			a.model.Mention.Index = 0
		})
	}()
}

// listMentionCandidates completes one directory level: directories come first
// with a trailing separator, hidden entries only when the prefix asks for them.
func listMentionCandidates(cwd, prefix string) []string {
	if cwd == "" {
		return nil
	}
	directory := cwd
	display := ""
	if index := strings.LastIndex(prefix, "/"); index >= 0 {
		display = prefix[:index+1]
		directory = filepath.Join(cwd, filepath.FromSlash(prefix[:index]))
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	needle := strings.ToLower(prefix[strings.LastIndex(prefix, "/")+1:])
	hidden := strings.HasPrefix(needle, ".")
	candidates := make([]string, 0, mentionCandidates)
	for _, entry := range entries {
		name := entry.Name()
		if !hidden && strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), needle) {
			continue
		}
		path := display + name
		if entry.IsDir() {
			path += "/"
		}
		candidates = append(candidates, path)
	}
	sort.Slice(candidates, func(left, right int) bool {
		leftDir := strings.HasSuffix(candidates[left], "/")
		rightDir := strings.HasSuffix(candidates[right], "/")
		if leftDir != rightDir {
			return leftDir
		}
		return strings.ToLower(candidates[left]) < strings.ToLower(candidates[right])
	})
	if len(candidates) > mentionCandidates {
		candidates = candidates[:mentionCandidates]
	}
	return candidates
}
