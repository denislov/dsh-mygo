package app

import (
	"encoding/json"
	"testing"
)

func TestSubagentComponentKeepsLiveStateAndInspectionSeparate(t *testing.T) {
	var component subagentComponent
	session := "parent"
	component.sessionOf = func() string { return session }
	component.reset(session)

	if !component.applyStatus(json.RawMessage(`{"parentSessionId":"parent","childSessionId":"child","provider":"p"}`), false) {
		t.Fatal("current parent status was rejected")
	}
	if component.applyStatus(json.RawMessage(`{"parentSessionId":"other","childSessionId":"child"}`), false) {
		t.Fatal("foreign parent status was accepted")
	}
	if got := component.summariesView(); len(got) != 1 || !got[0].Running {
		t.Fatalf("summaries = %+v", got)
	}

	component.openFor("parent")
	ctx, read, cancel, ok := component.beginRead(7, false)
	if !ok || read.parent != "parent" || read.child != "" {
		t.Fatalf("catalog read = %+v, %v", read, ok)
	}
	component.close()
	if component.acceptRead(read, 7) {
		t.Fatal("closed panel accepted a late read")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("close did not cancel the read")
	}
	cancel()
	if len(component.summariesView()) != 1 {
		t.Fatal("closing the panel cleared live status")
	}

	component.openFor("parent")
	if _, _, _, ok := component.beginRead(7, true); ok {
		t.Fatal("earlier page was allowed without a selected child")
	}
	component.selectChild("child")
	_, first, cancelFirst, ok := component.beginRead(7, false)
	if !ok {
		t.Fatal("first page read was rejected")
	}
	cancelFirst()
	page, ok := component.commitPage(first, subagentHistoryPage{Start: 10, End: 12, Next: 12, Inherited: 5, HasMore: true, EndReason: "completed"})
	if !ok || page.earlier || page.start != 10 || !page.hasMore {
		t.Fatalf("first page = %+v, %v", page, ok)
	}
	if before, ok := component.before(); !ok || before != 10 {
		t.Fatalf("before = %d, %v", before, ok)
	}
	_, earlier, cancelEarlier, ok := component.beginRead(7, true)
	if !ok || !earlier.earlier {
		t.Fatalf("earlier read = %+v, %v", earlier, ok)
	}
	cancelEarlier()
	if _, ok := component.commitPage(earlier, subagentHistoryPage{Start: 4, End: 9, Next: 10, Inherited: 6, HasMore: true}); ok {
		t.Fatal("page with a different inherited prefix was accepted")
	}

	session = "other"
	if component.acceptRead(read, 7) {
		t.Fatal("late read survived a parent change")
	}
	component.sessionChanged(session)
	if component.open || component.summariesView() != nil {
		t.Fatal("session change kept the previous parent state")
	}
	component.hostChanged()
}
