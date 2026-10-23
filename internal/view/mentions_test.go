package view

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestComposerMentionsCompletePaths(t *testing.T) {
	var queries []string
	model := sessionModel()
	model.SessionCWD = "C:/work"
	model.QueryMentions = func(prefix string) {
		queries = append(queries, prefix)
		model.Mention.Candidates = []string{"notes.txt", "src/"}
	}
	tt := tester(model)

	tt.Type("@no")

	if len(queries) == 0 || queries[len(queries)-1] != "no" {
		t.Fatalf("queries = %v", queries)
	}
	if !tt.HasText("notes.txt") || !tt.HasText("src/") {
		t.Fatalf("candidates missing in %v", tt.Texts())
	}
	tt.Key(0, ui.KeyTab)
	if model.Draft != "@notes.txt " {
		t.Fatalf("draft = %q", model.Draft)
	}
	if model.Mention.Active {
		t.Fatal("completion stayed open after choosing a path")
	}
}

func TestComposerMentionEscapeKeepsTypedTextAndSends(t *testing.T) {
	var sent []string
	model := sessionModel()
	model.SessionCWD = "C:/work"
	model.QueryMentions = func(string) { model.Mention.Candidates = []string{"notes.txt"} }
	model.Send = func(text string) { sent = append(sent, text) }
	tt := tester(model)

	tt.Type("@no")
	tt.Key(0, ui.KeyEscape)
	if tt.HasText("notes.txt") {
		t.Fatalf("completion still shown in %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if len(sent) != 1 || sent[0] != "@no" {
		t.Fatalf("sent = %v", sent)
	}
}

func TestMentionTokenStopsAtWhitespace(t *testing.T) {
	if _, _, ok := mentionToken("@notes.txt ", 11, 11); ok {
		t.Fatal("token after a space should not open")
	}
	start, prefix, ok := mentionToken("see @src/ma", 11, 11)
	if !ok || start != 4 || prefix != "src/ma" {
		t.Fatalf("token = %d %q %v", start, prefix, ok)
	}
}
