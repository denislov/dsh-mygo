package projection

import "strings"

// clipMarkdown preserves source whitespace and returns a truncation marker so
// streaming copy never edits the visible prefix. Do not replace it with the
// whitespace-trimming summary clip.
func clipMarkdown(source string, limit int) (string, bool) {
	count := 0
	for offset := range source {
		if count == limit {
			return strings.Clone(source[:offset]) + "…", true
		}
		count++
	}
	return source, false
}

// contentRun is one adjacent group of reasoning or visible text blocks. The
// committed log keeps reasoning and text blocks in model order, so runs map
// directly onto transcript rows and streaming previews.
type contentRun struct {
	reasoning bool
	text      string
	runes     int
	truncated bool
}

func (run *contentRun) append(text string, limit int) {
	remaining := limit - run.runes
	for _, character := range text {
		if remaining == 0 {
			run.truncated = true
			return
		}
		run.text += string(character)
		run.runes++
		remaining--
	}
}

// assistantRuns splits a message into bounded reasoning and text runs without
// leaking reasoning into the visible assistant source.
func assistantRuns(blocks []contentBlock) []contentRun {
	runs := make([]contentRun, 0, 4)
	add := func(reasoning bool, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		limit := maxMessageRunes
		if reasoning {
			limit = maxReasoningRunes
		}
		if last := len(runs) - 1; last >= 0 && runs[last].reasoning == reasoning {
			if runs[last].runes > 0 {
				runs[last].append("\n\n", limit)
			}
			runs[last].append(text, limit)
			return
		}
		if len(runs) >= maxAssistantParts {
			runs[len(runs)-1].truncated = true
			return
		}
		run := contentRun{reasoning: reasoning}
		run.append(text, limit)
		runs = append(runs, run)
	}
	for _, block := range blocks {
		switch block.Type {
		case "reasoning":
			add(true, block.Text)
		case "text":
			add(false, block.Text)
		case "image":
			add(false, "[image]")
		case "file":
			add(false, "[file]")
		}
	}
	return runs
}
