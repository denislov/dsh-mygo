package ids

import (
	"strings"
	"testing"
)

func TestValidIdentifier(t *testing.T) {
	for _, value := range []string{"s1", "session_1", "A-B"} {
		if !ValidIdentifier(value) {
			t.Fatalf("ValidIdentifier(%q) = false", value)
		}
	}
	for _, value := range []string{"", "bad space", "a.b", "路径", "line\nbreak", "nul\x00id"} {
		if ValidIdentifier(value) {
			t.Fatalf("ValidIdentifier(%q) = true", value)
		}
	}
	if !ValidIdentifier(strings.Repeat("a", MaxIdentifierBytes)) {
		t.Fatal("maximum-length identifier rejected")
	}
	if ValidIdentifier(strings.Repeat("a", MaxIdentifierBytes+1)) {
		t.Fatal("oversized identifier accepted")
	}
}
