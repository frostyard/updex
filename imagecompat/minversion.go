package imagecompat

import (
	"strings"
)

// MinImageVersionKey is the extension-release key that holds the minimum
// base image version a sysext revision needs. systemd-sysext ignores it.
const MinImageVersionKey = "UPDEX_MIN_IMAGE_VERSION"

// MinImageVersion returns the value of MinImageVersionKey in an
// extension-release map and whether the key is set. A key that is set to an
// empty string counts as set.
func MinImageVersion(release map[string]string) (value string, set bool) {
	value, set = release[MinImageVersionKey]
	return value, set
}

// CheckMinImageVersion reports whether a sysext revision may be linked on a
// host whose next-boot image has version imageVersion.
//
// When release does not set MinImageVersionKey, ok is true and reason is
// empty. When the key is set, both the key value and imageVersion must be
// digits only (snosi uses 14-digit timestamps); they are compared as
// numbers, with no limit on their length. When imageVersion is lower than the
// key value, or when either value is empty or not numeric, ok is false and
// reason is "needs image >= <value>", where <value> is the key value. A key
// value that is not numeric is shown with control characters replaced by "?"
// and cut at 64 bytes, because the text goes to logs and terminals.
func CheckMinImageVersion(release map[string]string, imageVersion string) (ok bool, reason string) {
	minValue, set := MinImageVersion(release)
	if !set {
		return true, ""
	}
	reason = "needs image >= " + displayValue(minValue)
	if !isDigits(minValue) || !isDigits(imageVersion) {
		return false, reason
	}
	if compareDigits(imageVersion, minValue) < 0 {
		return false, reason
	}
	return true, ""
}

// isDigits reports whether s is a non-empty string of ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// compareDigits compares two digit-only strings as numbers without
// conversion, so a value of any length cannot overflow. It returns -1, 0 or
// 1.
func compareDigits(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return strings.Compare(a, b)
}

const maxDisplayLen = 64

func displayValue(v string) string {
	if isDigits(v) && len(v) <= maxDisplayLen {
		return v
	}
	if len(v) > maxDisplayLen {
		v = v[:maxDisplayLen]
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '\uFFFD' {
			return '?'
		}
		return r
	}, v)
}
