// Package ids contains the shared string-identifier rule for session-like ids.
// JSON-RPC request ids have a separate string-or-safe-integer rule and do not
// use this package.
package ids

// MaxIdentifierBytes bounds ids before they reach maps, logs or file scopes.
const MaxIdentifierBytes = 128

// ValidIdentifier accepts the ASCII identifier spelling used by session,
// approval and question ids.
func ValidIdentifier(value string) bool {
	if len(value) == 0 || len(value) > MaxIdentifierBytes {
		return false
	}
	for _, character := range value {
		if character != '-' && character != '_' && !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}
