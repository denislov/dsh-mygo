package view

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"dsh-mygo/internal/projection"
)

func scrollingHistoryModel() *Model {
	m := sessionModel()
	m.History = HistoryState{Available: true, Start: 300}
	for i := range 40 {
		m.Items = append(m.Items, projection.Item{ID: fmt.Sprint(i), Kind: projection.KindAssistant, Title: "Assistant", Text: fmt.Sprintf("Answer %d\n\nA complete paragraph to scroll through.", i)})
	}
	return m
}

func TestPrependingHistoryKeepsReadingAnchorAndDoesNotDuplicateLoad(t *testing.T) {
	m := scrollingHistoryModel()
	loads := 0
	m.History.LoadEarlier = func() { loads++; m.History.Busy = true }
	tt := tester(m)
	tt.Scroll(600, 160, 0, -100000)
	before, ok := tt.Find("Answer 0")
	if !ok {
		t.Fatal("original anchor missing")
	}
	var older []projection.Item
	for i := range 10 {
		older = append(older, projection.Item{ID: fmt.Sprint("older-", i), Kind: projection.KindAssistant, Title: "Assistant", Text: fmt.Sprintf("Older answer %d\n\nA complete older paragraph.", i)})
	}
	m.Items = append(older, m.Items...)
	m.History.Start = 280
	m.History.Busy, m.SessionBusy = false, false
	tt.Frame()
	after, ok := tt.Find("Answer 0")
	if !ok || before.Y != after.Y {
		t.Fatalf("prepend moved reading anchor: %+v / %+v", before, after)
	}
	if loads != 1 {
		t.Fatalf("new frame scheduled a duplicate read: %d", loads)
	}
}

func TestAutomaticHistoryWorksInsideOneTallMessage(t *testing.T) {
	m := scrollingHistoryModel()
	m.Items = []projection.Item{{ID: "tall", Kind: projection.KindAssistant, Title: "Assistant", Text: strings.Repeat("A paragraph in one long assistant response.\n\n", 80)}}
	loads := 0
	m.History.LoadEarlier = func() { loads++; m.History.Busy = true }
	tt := tester(m)
	if loads != 0 {
		t.Fatal("tail of a tall message triggered earlier history")
	}
	for range 50 {
		tt.Scroll(600, 160, 0, -100)
	}
	if loads != 1 {
		t.Fatalf("scroll within one row did not load earlier history: %d", loads)
	}
}

func TestHistoryLoadsOnScrollAndReturnsWithFloatingArrow(t *testing.T) {
	m := scrollingHistoryModel()
	loads := 0
	m.History.LoadEarlier = func() { loads++; m.History.Busy = true }
	tt := tester(m)
	if tt.HasText("Load earlier messages") || tt.HasText("Return to latest") {
		t.Fatal("history actions rendered as visible text")
	}
	if loads != 0 {
		t.Fatal("latest view loaded earlier history before scrolling")
	}
	tt.Scroll(600, 160, 0, -100000)
	tt.Advance(200 * time.Millisecond)
	if loads != 1 {
		t.Fatalf("scroll did not load exactly once: %d", loads)
	}
	tt.Frame()
	tt.Frame()
	if loads != 1 {
		t.Fatal("rebuild duplicated history request")
	}
	arrow, ok := tt.Find("Return to latest")
	input, _ := tt.Find("Message")
	if !ok || arrow.W > 44 || arrow.Y+arrow.H > input.Y {
		t.Fatalf("latest action is not a floating icon above input: %+v / %+v", arrow, input)
	}
	if err := tt.Click("Return to latest"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !m.List.AtEnd() {
		t.Fatal("arrow did not return to the loaded tail")
	}
}

func TestCanceledHistoryCanLoadAgainWithoutLockingSidebar(t *testing.T) {
	m := scrollingHistoryModel()
	loads := 0
	m.History.LoadEarlier = func() { loads++; m.History.Busy = true }
	selected := ""
	m.SwitchSession = func(id string) { selected = id }
	tt := tester(m)
	tt.Scroll(600, 160, 0, -100000)
	if loads != 1 || !m.History.Busy {
		t.Fatal("scroll did not start the background read")
	}
	if err := tt.Click("Session two"); err != nil {
		t.Fatal(err)
	}
	if selected != "two" {
		t.Fatal("background read disabled sidebar navigation")
	}
	m.History.FinishRead()
	tt.Frame()
	if loads != 2 {
		t.Fatal("canceled boundary could not be read again")
	}
}

func TestSwitchingSessionKeepsSidebarRowsFixed(t *testing.T) {
	m := scrollingHistoryModel()
	tt := tester(m)
	before, _ := tt.Find("Session two")
	m.SessionBusy, m.SessionSwitching = true, true
	tt.Frame()
	after, _ := tt.Find("Session two")
	loading, ok := tt.Find("Loading session history…")
	if before.Y != after.Y || !ok || loading.X < sidebarWidth {
		t.Fatalf("loading shifted sidebar or rendered inside it: before=%+v after=%+v loading=%+v", before, after, loading)
	}
}
