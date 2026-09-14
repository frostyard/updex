package version

import (
	"regexp"
	"testing"
)

var fuzzVersion = regexp.MustCompile(`^[0-9A-Za-z.+:~_-]+$`)

func FuzzPatternRoundTrip(f *testing.F) {
	for _, seed := range []string{
		"1.2.3",
		"2:1.0~rc1-3+deb12u1",
		"2026.09.14",
	} {
		f.Add(seed)
	}

	pattern, err := ParsePattern("updex-@v.tar.gz")
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, version string) {
		if !fuzzVersion.MatchString(version) {
			t.Skip()
		}

		filename := pattern.BuildFilename(version)
		if !pattern.Matches(filename) {
			t.Fatalf("built filename %q does not match pattern", filename)
		}

		extracted, ok := pattern.ExtractVersion(filename)
		if !ok {
			t.Fatalf("ExtractVersion(%q) did not match", filename)
		}
		if extracted != version {
			t.Fatalf("ExtractVersion(%q) = %q, want %q", filename, extracted, version)
		}
	})
}

func FuzzCompareInvariants(f *testing.F) {
	for _, seed := range [][2]string{
		{"1.2.3", "1.2.4"},
		{"2:1.0~rc1-3", "2:1.0-1"},
		{"2026.09.14", "2025.12.31"},
		{"not a version", "???"},
	} {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, a, b string) {
		if got := Compare(a, a); got != 0 {
			t.Fatalf("Compare(%q, itself) = %d, want 0", a, got)
		}

		ab, ba := Compare(a, b), Compare(b, a)
		if compareSign(ab) != -compareSign(ba) {
			t.Fatalf("Compare(%q, %q) = %d, Compare(%q, %q) = %d", a, b, ab, b, a, ba)
		}
	})
}

func compareSign(value int) int {
	switch {
	case value < 0:
		return -1
	case value > 0:
		return 1
	default:
		return 0
	}
}
