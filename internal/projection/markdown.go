package projection

import "strings"

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

func assistantSource(blocks []contentBlock) (string, bool) {
	var result strings.Builder
	count := 0
	appendText := func(text string) bool {
		for _, character := range text {
			if count == maxMessageRunes {
				return false
			}
			result.WriteRune(character)
			count++
		}
		return true
	}
	for _, block := range blocks {
		text := ""
		switch block.Type {
		case "text":
			text = block.Text
		case "image":
			text = "[image]"
		case "file":
			text = "[file]"
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		if result.Len() > 0 && !appendText("\n\n") {
			return result.String() + "…", true
		}
		if !appendText(text) {
			return result.String() + "…", true
		}
	}
	return result.String(), false
}
