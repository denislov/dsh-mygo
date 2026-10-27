// Package version is the Go view of the release identity. versions.json at the
// repository root is authoritative; the tests keep this package in sync.
package version

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	App             = "0.1.2"
	Protocol        = 1
	DSH             = "0.2.1-rc.2"
	Repository      = "denislov/dsh-mygo"
	AppMin          = "0.1.1"
	AppMaxExclusive = "0.2.0"
)

// CompareAppVersion compares two plain numeric release versions (major.minor.patch).
// Pre-release suffixes are intentionally rejected: app compatibility bounds use
// released numeric versions only.
func CompareAppVersion(left, right string) (int, error) {
	a, err := parseAppVersion(left)
	if err != nil {
		return 0, err
	}
	b, err := parseAppVersion(right)
	if err != nil {
		return 0, err
	}
	for i := range a {
		if a[i] < b[i] {
			return -1, nil
		}
		if a[i] > b[i] {
			return 1, nil
		}
	}
	return 0, nil
}

// AppVersionInRange reports whether value satisfies min <= value < maxExclusive.
func AppVersionInRange(value, min, maxExclusive string) (bool, error) {
	lower, err := CompareAppVersion(value, min)
	if err != nil {
		return false, err
	}
	upper, err := CompareAppVersion(value, maxExclusive)
	if err != nil {
		return false, err
	}
	return lower >= 0 && upper < 0, nil
}

func parseAppVersion(value string) ([3]int, error) {
	var parts [3]int
	fields := strings.Split(value, ".")
	if len(fields) != 3 {
		return parts, fmt.Errorf("invalid app version %q", value)
	}
	for i, field := range fields {
		if field == "" {
			return parts, fmt.Errorf("invalid app version %q", value)
		}
		for _, r := range field {
			if r < '0' || r > '9' {
				return parts, fmt.Errorf("invalid app version %q", value)
			}
		}
		number, err := strconv.Atoi(field)
		if err != nil {
			return parts, fmt.Errorf("invalid app version %q", value)
		}
		parts[i] = number
	}
	return parts, nil
}
