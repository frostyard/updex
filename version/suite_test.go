package version

import (
	"errors"
	"testing"
)

func TestParseAnySuitePatternMatchesEverySuite(t *testing.T) {
	p, err := ParseAnySuitePattern("foo_@v_" + SuiteWildcard + "_x86-64.raw")
	if err != nil {
		t.Fatalf("ParseAnySuitePattern() error = %v", err)
	}
	tests := []struct {
		name      string
		filename  string
		wantVer   string
		wantSuite string
		wantOK    bool
	}{
		{"trixie", "foo_1.0_13_x86-64.raw", "1.0", "13", true},
		{"forky", "foo_2.0~rc1_14_x86-64.raw", "2.0~rc1", "14", true},
		{"dotted suite", "foo_1.0_13.1_x86-64.raw", "1.0", "13.1", true},
		{"letters in suite", "foo_1.0_forky-1~a_x86-64.raw", "1.0", "forky-1~a", true},
		// A '_' is not part of a suite: images of a component foo_bar never match foo.
		{"other component", "foo_bar_1.0_13_x86-64.raw", "", "", false},
		{"empty suite", "foo_1.0__x86-64.raw", "", "", false},
		{"upper case suite", "foo_1.0_A_x86-64.raw", "", "", false},
		{"other arch", "foo_1.0_13_arm64.raw", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := p.ExtractVersion(tt.filename)
			if ok != tt.wantOK || v != tt.wantVer {
				t.Errorf("ExtractVersion(%q) = %q, %v; want %q, %v", tt.filename, v, ok, tt.wantVer, tt.wantOK)
			}
			s, ok := p.Suite(tt.filename)
			if ok != tt.wantOK || s != tt.wantSuite {
				t.Errorf("Suite(%q) = %q, %v; want %q, %v", tt.filename, s, ok, tt.wantSuite, tt.wantOK)
			}
			if got := p.Matches(tt.filename); got != tt.wantOK {
				t.Errorf("Matches(%q) = %v, want %v", tt.filename, got, tt.wantOK)
			}
		})
	}
}

func TestParseAnySuitePatternSuiteBeforeVersion(t *testing.T) {
	// The version is the first @v group, also when a suite comes first.
	p, err := ParseAnySuitePattern("foo_" + SuiteWildcard + "_@v.raw")
	if err != nil {
		t.Fatalf("ParseAnySuitePattern() error = %v", err)
	}
	v, s, ok := ExtractSuiteParsed("foo_14_3.2.raw", []*Pattern{p})
	if !ok || v != "3.2" || s != "14" {
		t.Errorf("ExtractSuiteParsed() = %q, %q, %v; want 3.2, 14, true", v, s, ok)
	}
}

func TestParseAnySuitePatternRepeatedSuiteMustAgree(t *testing.T) {
	p, err := ParseAnySuitePattern(SuiteWildcard + "/foo_@v_" + SuiteWildcard + ".raw")
	if err != nil {
		t.Fatalf("ParseAnySuitePattern() error = %v", err)
	}
	if !p.Matches("13/foo_1.0_13.raw") {
		t.Error("Matches() = false for the same suite twice, want true")
	}
	if p.Matches("13/foo_1.0_14.raw") {
		t.Error("Matches() = true for 2 different suites, want false")
	}
}

func TestParsePatternKeepsLiteralsAndNUL(t *testing.T) {
	// ParsePattern gives no meaning to SuiteWildcard or to "@w": both stay literal.
	p, err := ParsePattern("foo_@v_@w_" + SuiteWildcard + ".raw")
	if err != nil {
		t.Fatalf("ParsePattern() error = %v", err)
	}
	if p.Matches("foo_1.0_@w_13.raw") {
		t.Error("ParsePattern treats SuiteWildcard as a wildcard")
	}
	if !p.Matches("foo_1.0_@w_" + SuiteWildcard + ".raw") {
		t.Error("ParsePattern does not keep @w and NUL literal")
	}
	if _, ok := p.Suite("foo_1.0_@w_" + SuiteWildcard + ".raw"); ok {
		t.Error("Suite() reports a suite for a pattern without suite wildcard")
	}

	// "@w" is also literal in an any-suite pattern.
	q, err := ParseAnySuitePattern("foo_@v_@w.raw")
	if err != nil {
		t.Fatalf("ParseAnySuitePattern() error = %v", err)
	}
	if q.Matches("foo_1.0_13.raw") || !q.Matches("foo_1.0_@w.raw") {
		t.Error("ParseAnySuitePattern changes the meaning of a literal @w")
	}
}

func TestParseAnySuitePatternErrors(t *testing.T) {
	if _, err := ParseAnySuitePattern(""); !errors.Is(err, ErrEmptyPattern) {
		t.Errorf("empty pattern error = %v, want ErrEmptyPattern", err)
	}
	if _, err := ParseAnySuitePattern("foo_" + SuiteWildcard + ".raw"); !errors.Is(err, ErrMissingVersionPlaceholder) {
		t.Errorf("pattern without @v error = %v, want ErrMissingVersionPlaceholder", err)
	}
	ps, err := ParseAnySuitePatterns([]string{"bad", "foo_@v_" + SuiteWildcard + ".raw"})
	if len(ps) != 1 || !errors.Is(err, ErrMissingVersionPlaceholder) {
		t.Errorf("ParseAnySuitePatterns() = %d patterns, %v; want 1, ErrMissingVersionPlaceholder", len(ps), err)
	}
}
