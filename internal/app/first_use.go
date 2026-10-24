package app

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"dsh-mygo/internal/view"
	"github.com/egoist/mygo"
)

func (a *App) UseWorkingFolder() {
	if a.canChooseSession() && a.model.SessionCWD != "" {
		a.model.Setup.WorkspaceRequired = false
	}
}

// Choosing a project creates a new Host-owned conversation; an existing
// conversation's working directory and draft are never rewritten.
func (a *App) ChooseProjectFolder() {
	if !a.canChooseSession() {
		return
	}
	id, generation, cwd := a.model.SessionID, a.hostGeneration.Load(), a.model.SessionCWD
	a.model.Setup.Picking, a.model.Setup.Error = true, ""
	pick := a.options.PickDirectory
	if pick == nil {
		title := a.model.T("Choose project folder")
		pick = func(current string) (string, error) {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: title, DefaultPath: current, Directory: true})
			if err != nil || len(paths) == 0 {
				return "", err
			}
			return paths[0], nil
		}
	}
	go func() {
		folder, err := pick(cwd)
		if err == nil && folder != "" {
			if len(folder) > 32768 || !utf8.ValidString(folder) || !filepath.IsAbs(folder) || strings.IndexFunc(folder, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
				err = os.ErrInvalid
			} else if info, statErr := os.Stat(folder); statErr != nil {
				err = statErr
			} else if !info.IsDir() {
				err = os.ErrInvalid
			}
		}
		a.post(func() {
			if a.shuttingDown.Load() || a.hostGeneration.Load() != generation || a.model.SessionID != id || !a.model.Setup.Picking {
				return
			}
			a.model.Setup.Picking = false
			if a.model.Phase != view.PhaseIdle {
				return
			}
			if err != nil {
				a.model.Setup.Error = "Could not open the project folder. Choose an existing folder and retry"
				return
			}
			if folder == "" {
				return
			}
			if filepath.Clean(folder) == filepath.Clean(cwd) {
				a.UseWorkingFolder()
				return
			}
			a.loadSessionInDirectory("", folder)
		})
	}()
}
