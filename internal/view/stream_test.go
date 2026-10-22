package view

import (
	"testing"

	"dsh-mygo/internal/projection"
)

func TestStreamingRowBecomesOneCommittedRowAndCanReturn(t *testing.T) {
	model := NewModel()
	model.Phase = PhaseRunning
	model.Draft = "Next draft"
	model.Items = []projection.Item{{ID: "stream:s1:1", Kind: projection.KindAssistant, Title: "Assistant", Text: "Live prefix", Detail: "Streaming…", State: "streaming"}}
	tester := tester(model)
	if !tester.HasText("Live prefix") || !tester.HasText("Streaming…") {
		t.Fatal("partial output not rendered")
	}
	model.Items = []projection.Item{{ID: "seq:1", Kind: projection.KindAssistant, Title: "Assistant", Text: "Live prefix complete"}}
	tester.Frame()
	if tester.HasText("Streaming…") || !tester.HasText("Live prefix complete") || model.Draft != "Next draft" {
		t.Fatal("commit retained preview or lost draft")
	}
	model.Items = append(model.Items, projection.Item{ID: "stream:s1:2", Kind: projection.KindAssistant, Title: "Assistant", Text: "Second prefix", Detail: "Streaming…", State: "streaming"})
	tester.Frame()
	if !tester.HasText("Second prefix") {
		t.Fatal("new stream did not appear")
	}
	model.Items = model.Items[:1]
	tester.Frame()
	if tester.HasText("Second prefix") {
		t.Fatal("discarded preview retained")
	}
}
