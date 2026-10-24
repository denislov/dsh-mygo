package view

import (
	"fmt"
	"strings"

	mgo "github.com/egoist/mygo/ui"
)

// MentionState is the composer's @path completion: which token is open, its
// candidates, and the highlighted one. It is frame-scoped UI state and never
// persisted.
type MentionState struct {
	Active      bool
	Suppressed  bool
	TokenStart  int
	Prefix      string
	Index       int
	Candidates  []string
	CaretTarget int
}

// Reset forgets the open completion, as after a session switch.
func (state *MentionState) Reset() { *state = MentionState{CaretTarget: -1} }

// mentionToken finds the @token ending at a collapsed caret. It returns the
// rune offset of '@' and the typed prefix after it; a token never spans a
// whitespace boundary, so only the file reference under the caret opens.
func mentionToken(text string, selectionStart, selectionEnd int) (int, string, bool) {
	runes := []rune(text)
	if selectionStart != selectionEnd || selectionStart <= 0 || selectionStart > len(runes) {
		return 0, "", false
	}
	const maxTokenRunes = 96
	for index := selectionStart - 1; index >= 0 && index >= selectionStart-maxTokenRunes; index-- {
		switch runes[index] {
		case '@':
			return index, string(runes[index+1 : selectionStart]), true
		case ' ', '\n', '\t', '\r':
			return 0, "", false
		}
	}
	return 0, "", false
}

// selectMention replaces the open token with the chosen path and parks the
// caret after it for the next frame.
func selectMention(model *Model, candidate string) {
	runes := []rune(model.Draft)
	start := model.Mention.TokenStart
	if start < 0 || start > len(runes) {
		return
	}
	end := len(runes)
	caret := start + 1 + len([]rune(model.Mention.Prefix))
	if caret <= len(runes) {
		end = caret
	}
	insertion := []rune("@" + strings.TrimSpace(candidate) + " ")
	model.Draft = string(runes[:start]) + string(insertion) + string(runes[end:])
	model.Mention.Active = false
	model.Mention.Candidates = nil
	model.Mention.Prefix = ""
	model.Mention.CaretTarget = start + len(insertion)
	if model.DraftChanged != nil {
		model.DraftChanged()
	}
}

// mentionKey consumes the keys that drive the completion list. It reports
// whether the key was handled so Enter can still send when nothing is open.
func mentionKey(model *Model, key mgo.TextKey) bool {
	state := &model.Mention
	if !state.Active {
		return false
	}
	switch key.Key {
	case mgo.KeyUp:
		if state.Index > 0 {
			state.Index--
		}
		return true
	case mgo.KeyDown:
		if state.Index < len(state.Candidates)-1 {
			state.Index++
		}
		return true
	case mgo.KeyTab:
		if len(state.Candidates) == 0 {
			return false
		}
		selectMention(model, state.Candidates[state.Index])
		return true
	case mgo.KeyEnter:
		if key.Mods != 0 || len(state.Candidates) == 0 {
			return false
		}
		selectMention(model, state.Candidates[state.Index])
		return true
	case mgo.KeyEscape:
		state.Active = false
		state.Suppressed = true
		return true
	default:
		return false
	}
}

// composerMentions maps the caret onto the @token under it, asks for candidates
// when the token changes, and shows them anchored to the composer.
func composerMentions(c *mgo.Context, area mgo.Element, model *Model) {
	state := &model.Mention
	selectionStart, selectionEnd := area.TextSelection()
	tokenStart, prefix, ok := mentionToken(model.Draft, selectionStart, selectionEnd)
	if !ok {
		state.Active = false
		state.Suppressed = false
		state.Prefix = ""
		state.Candidates = nil
		return
	}
	if tokenStart != state.TokenStart || prefix != state.Prefix {
		state.TokenStart = tokenStart
		state.Prefix = prefix
		state.Index = 0
		state.Candidates = nil
		state.Suppressed = false
		state.Active = true
		if model.QueryMentions != nil {
			model.QueryMentions(prefix)
		}
	}
	if state.Suppressed {
		state.Active = false
		return
	}
	state.Active = true
	tokens := tokensFor(c.Theme())
	panel := mgo.Popover(c, area, &state.Active, func() {
		mgo.Column(c).Padding(space4).Gap(space2).Background(tokens.SurfaceRaised).
			Border(1, tokens.Border).Radius(radiusMd).Children(func() {
			if len(state.Candidates) == 0 {
				mgo.Text(c, model.T("No matching files")).FontSize(textMicro).TextColor(tokens.TextMuted).Padding(space4)
				return
			}
			for index, candidate := range state.Candidates {
				button := mgo.Button(c.Key(fmt.Sprintf("mention-%d", index)), candidate).
					FontSize(textCaption).Padding(space4, space8).Radius(radiusSm)
				if index == state.Index {
					button.Background(tokens.AccentSoft).TextColor(tokens.Accent)
				}
				if button.Clicked() {
					selectMention(model, candidate)
					return
				}
			}
		})
	})
	if panel.Dismissed() {
		state.Suppressed = true
		state.Active = false
	}
}
