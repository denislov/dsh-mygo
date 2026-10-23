package preferences

import "errors"

var ErrUI = errors.New("Choose valid preferences and distinct command shortcuts")

type UISettings struct {
	Theme       string `json:"theme"`
	Locale      string `json:"locale"`
	Send        string `json:"send"`
	NewSession  string `json:"newSession"`
	Preferences string `json:"preferences"`
	Stop        string `json:"stop"`
}

func DefaultUI() UISettings {
	return UISettings{Theme: "system", Locale: "en-US", Send: "Enter", NewSession: "Ctrl+N", Preferences: "Ctrl+,", Stop: "Ctrl+."}
}
func CommandChoices() []string {
	return []string{"None", "Ctrl+N", "Ctrl+Shift+N", "Ctrl+,", "Ctrl+Shift+P", "Ctrl+.", "Ctrl+Shift+X"}
}
func (p UISettings) Validate() error {
	invalid := ErrUI
	if p.Theme != "system" && p.Theme != "light" && p.Theme != "dark" || p.Locale != "en-US" && p.Locale != "zh-CN" || p.Send != "Enter" && p.Send != "Ctrl+Enter" {
		return invalid
	}
	seen := map[string]bool{}
	for _, key := range []string{p.NewSession, p.Preferences, p.Stop} {
		allowed := false
		for _, choice := range CommandChoices() {
			if choice == key {
				allowed = true
			}
		}
		if !allowed || key != "None" && seen[key] {
			return invalid
		}
		seen[key] = true
	}
	return nil
}
