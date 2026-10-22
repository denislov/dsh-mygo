package view

import (
	"testing"
	"time"

	"dsh-mygo/internal/projection"
)

const markdownExample = "# Native heading\n\n**Bold text** and `inline`.\n\n- first item\n- second item\n\n```go\n  fmt.Println(\"你好\")\n```\n"

func markdownModel() *Model {
	model := NewModel()
	model.Phase = PhaseIdle
	model.Items = []projection.Item{{ID: "seq:1", Kind: projection.KindAssistant, Title: "Assistant", Text: markdownExample}}
	return model
}

func TestAssistantMarkdownAndExactCopy(t *testing.T) {
	model := markdownModel()
	tester := tester(model)
	for _, text := range []string{"Native heading", "Bold text and inline.", "first item", "go"} {
		if !tester.HasText(text) {
			t.Fatalf("rendered Markdown missing %q", text)
		}
	}
	if err := tester.Click("Copy code"); err != nil {
		t.Fatal(err)
	}
	if tester.Clipboard() != "  fmt.Println(\"你好\")\n" {
		t.Fatalf("code copy changed whitespace: %q", tester.Clipboard())
	}
	if err := tester.Click("Copy Markdown"); err != nil {
		t.Fatal(err)
	}
	if tester.Clipboard() != markdownExample {
		t.Fatal("message copy lost Markdown source")
	}
}

func TestStreamingCodeCopyAndCommit(t *testing.T) {
	model := markdownModel()
	model.Items[0].ID = "stream:s1:1"
	model.Items[0].State = "streaming"
	model.Items[0].Text = "```go\n  partial()\n"
	tester := tester(model)
	tester.SetClipboard("unchanged")
	_ = tester.Click("Copy code")
	if tester.Clipboard() != "unchanged" {
		t.Fatal("open code fence was copied as finished")
	}
	model.Items = []projection.Item{{ID: "seq:2", Kind: projection.KindAssistant, Title: "Assistant", Text: "```go\n  complete()\n```\n"}}
	tester.Frame()
	if err := tester.Click("Copy code"); err != nil {
		t.Fatal(err)
	}
	if tester.Clipboard() != "  complete()\n" {
		t.Fatal("committed code copy retained partial source")
	}
}

func TestMarkdownDoesNotOpenUnsafeLinksOrFetchImages(t *testing.T) {
	model := markdownModel()
	model.Items[0].Text = "[bad](javascript:alert(1)) [local](file:///tmp/secret) [relative](../secret) [good](https://example.com)\n\n![Image placeholder](https://example.com/image.png)"
	tester := tester(model)
	for _, label := range []string{"bad", "local", "relative"} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if len(tester.OpenedURLs()) != 0 || model.Markdown.LoadImage != nil || model.Markdown.HTML != nil {
		t.Fatal("untrusted Markdown gained external capabilities")
	}
	if err := tester.Click("good"); err != nil {
		t.Fatal(err)
	}
	if len(tester.OpenedURLs()) != 1 || tester.OpenedURLs()[0] != "https://example.com" {
		t.Fatal("safe link did not use explicit click")
	}
}

func TestUserTextAndTruncatedMessagesStayLiteral(t *testing.T) {
	model := markdownModel()
	model.Items[0].Kind = projection.KindUser
	model.Items[0].Title = "You"
	tester := tester(model)
	if !tester.HasText(markdownExample) || tester.HasText("Copy code") {
		t.Fatal("user input interpreted as Markdown")
	}
	model.Items[0].Kind = projection.KindAssistant
	model.Items[0].Truncated = true
	tester.Frame()
	if !tester.HasText("Truncated message: only the visible prefix is available.") || tester.HasText("Copy code") {
		t.Fatal("truncated code presented as complete")
	}
	if err := tester.Click("Copy visible text"); err != nil {
		t.Fatal(err)
	}
	if tester.Clipboard() != markdownExample {
		t.Fatal("visible-prefix copy changed text")
	}
}

func TestInterruptedCodeCannotBeCopiedAsComplete(t *testing.T) {
	model := markdownModel()
	model.Items[0].State = "interrupted"
	model.Items[0].Detail = "Interrupted"
	tester := tester(model)
	tester.SetClipboard("unchanged")
	_ = tester.Click("Copy code")
	if tester.Clipboard() != "unchanged" || !tester.HasText("Interrupted code (copy disabled)") {
		t.Fatal("interrupted code exposed a complete-code copy action")
	}
	if err := tester.Click("Copy partial Markdown"); err != nil {
		t.Fatal(err)
	}
	if tester.Clipboard() != markdownExample {
		t.Fatal("partial source copy changed Markdown")
	}
}

func TestCopyFeedbackDoesNotFollowAnotherMessageOrStreamUpdate(t *testing.T) {
	model := markdownModel()
	tester := tester(model)
	if err := tester.Click("Copy Markdown"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("Message copied") {
		t.Fatal("copy feedback missing")
	}
	model.Items[0].ID = "seq:2"
	tester.Frame()
	if tester.HasText("Message copied") {
		t.Fatal("copy status moved to another message")
	}
	model.Items[0].ID = "seq:1"
	model.Items[0].State = "streaming"
	model.Items[0].Text = "New content"
	tester.Frame()
	if tester.HasText("Message copied") || !tester.HasText("Copy current text") {
		t.Fatal("copy status survived changed content")
	}
}

func TestMarkdownLinkSchemesAreExplicitlyRestricted(t *testing.T) {
	for _, address := range []string{"file:///secret", "javascript:alert(1)", "data:text/html,hello", "mailto:a@example.com", "../relative", "//example.com", "https://user:pass@example.com", "https://example.com\n", "https://", "https://example.com/a b"} {
		if safeMarkdownLink(address) {
			t.Fatalf("unsafe scheme allowed: %q", address)
		}
	}
	if !safeMarkdownLink("https://example.com/docs?q=go#code") {
		t.Fatal("valid HTTP(S) destination rejected")
	}
}

func TestToolMarkdownIsOnlyLiteralRecordedOutput(t *testing.T) {
	source := "# Tool output\n\n```sh\necho tool\n```\n"
	model := NewModel()
	model.Phase = PhaseIdle
	model.Items = []projection.Item{{ID: "tool:1", Kind: projection.KindTool, Title: "Tool · read", Detail: source, State: "done", Tool: &projection.ToolDetails{Result: source}}}
	tester := tester(model)
	if err := tester.Click("Tool · read"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(300 * time.Millisecond)
	if !tester.HasText(source) || tester.HasText("Copy code") {
		t.Fatal("tool preview interpreted as assistant Markdown")
	}
	if err := tester.Click("View details"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.Click("Result"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText(source) || tester.HasText("Copy code") {
		t.Fatal("tool result interpreted as assistant Markdown")
	}
}
