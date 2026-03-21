package version

import (
	"fmt"
	"strings"
	"testing"
)

func TestParsePattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr error
	}{
		{
			name:    "simple version pattern",
			pattern: "myext_@v.raw",
			wantErr: nil,
		},
		{
			name:    "pattern with compression suffix",
			pattern: "myext_@v.raw.xz",
			wantErr: nil,
		},
		{
			name:    "pattern with multiple placeholders",
			pattern: "myext_@v_@u.raw",
			wantErr: nil,
		},
		{
			name:    "empty pattern",
			pattern: "",
			wantErr: ErrEmptyPattern,
		},
		{
			name:    "pattern without version placeholder",
			pattern: "myext_1.0.0.raw",
			wantErr: ErrMissingVersionPlaceholder,
		},
		{
			name:    "pattern with special regex chars",
			pattern: "my.ext_@v.raw",
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePattern(tt.pattern)
			if err != tt.wantErr {
				t.Errorf("ParsePattern() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPattern_ExtractVersion(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		filename    string
		wantVersion string
		wantOK      bool
	}{
		{
			name:        "simple version",
			pattern:     "myext_@v.raw",
			filename:    "myext_1.2.3.raw",
			wantVersion: "1.2.3",
			wantOK:      true,
		},
		{
			name:        "version with xz suffix",
			pattern:     "myext_@v.raw.xz",
			filename:    "myext_2.0.0.raw.xz",
			wantVersion: "2.0.0",
			wantOK:      true,
		},
		{
			name:        "version with prerelease",
			pattern:     "myext_@v.raw",
			filename:    "myext_1.0.0-rc1.raw",
			wantVersion: "1.0.0-rc1",
			wantOK:      true,
		},
		{
			name:        "version with build metadata",
			pattern:     "myext_@v.raw",
			filename:    "myext_1.0.0+build123.raw",
			wantVersion: "1.0.0+build123",
			wantOK:      true,
		},
		{
			name:        "non-matching filename",
			pattern:     "myext_@v.raw",
			filename:    "other_1.2.3.raw",
			wantVersion: "",
			wantOK:      false,
		},
		{
			name:        "wrong extension",
			pattern:     "myext_@v.raw",
			filename:    "myext_1.2.3.img",
			wantVersion: "",
			wantOK:      false,
		},
		{
			name:        "pattern with UUID placeholder",
			pattern:     "myext_@v_@u.raw",
			filename:    "myext_1.0.0_550e8400-e29b-41d4-a716-446655440000.raw",
			wantVersion: "1.0.0",
			wantOK:      true,
		},
		{
			name:        "date-based version",
			pattern:     "myext_@v.raw",
			filename:    "myext_20240115.raw",
			wantVersion: "20240115",
			wantOK:      true,
		},
		{
			name:        "debian version with epoch",
			pattern:     "docker_@v_amd64.raw",
			filename:    "docker_5:29.1.5-1~debian.13~trixie_amd64.raw",
			wantVersion: "5:29.1.5-1~debian.13~trixie",
			wantOK:      true,
		},
		{
			name:        "debian version with tilde",
			pattern:     "incus_@v_amd64.raw",
			filename:    "incus_1:6.20-debian13-202601150536_amd64.raw",
			wantVersion: "1:6.20-debian13-202601150536",
			wantOK:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParsePattern(tt.pattern)
			if err != nil {
				t.Fatalf("ParsePattern() error = %v", err)
			}

			gotVersion, gotOK := p.ExtractVersion(tt.filename)
			if gotVersion != tt.wantVersion {
				t.Errorf("ExtractVersion() version = %v, want %v", gotVersion, tt.wantVersion)
			}
			if gotOK != tt.wantOK {
				t.Errorf("ExtractVersion() ok = %v, want %v", gotOK, tt.wantOK)
			}
		})
	}
}

func TestPattern_Matches(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		filename string
		want     bool
	}{
		{
			name:     "matching file",
			pattern:  "myext_@v.raw",
			filename: "myext_1.2.3.raw",
			want:     true,
		},
		{
			name:     "non-matching file",
			pattern:  "myext_@v.raw",
			filename: "other_1.2.3.raw",
			want:     false,
		},
		{
			name:     "partial match should fail",
			pattern:  "myext_@v.raw",
			filename: "prefix_myext_1.2.3.raw",
			want:     false,
		},
		{
			name:     "suffix should fail",
			pattern:  "myext_@v.raw",
			filename: "myext_1.2.3.raw.bak",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParsePattern(tt.pattern)
			if err != nil {
				t.Fatalf("ParsePattern() error = %v", err)
			}

			if got := p.Matches(tt.filename); got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPattern_BuildFilename(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		version string
		want    string
	}{
		{
			name:    "simple pattern",
			pattern: "myext_@v.raw",
			version: "1.2.3",
			want:    "myext_1.2.3.raw",
		},
		{
			name:    "pattern with xz",
			pattern: "myext_@v.raw.xz",
			version: "2.0.0",
			want:    "myext_2.0.0.raw.xz",
		},
		{
			name:    "pattern with other placeholders stripped",
			pattern: "myext_@v_@u.raw",
			version: "1.0.0",
			want:    "myext_1.0.0_.raw",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParsePattern(tt.pattern)
			if err != nil {
				t.Fatalf("ParsePattern() error = %v", err)
			}

			if got := p.BuildFilename(tt.version); got != tt.want {
				t.Errorf("BuildFilename() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPattern_Raw(t *testing.T) {
	pattern := "myext_@v.raw"
	p, err := ParsePattern(pattern)
	if err != nil {
		t.Fatalf("ParsePattern() error = %v", err)
	}

	if got := p.Raw(); got != pattern {
		t.Errorf("Raw() = %v, want %v", got, pattern)
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		v1   string
		v2   string
		want int
	}{
		{
			name: "equal versions",
			v1:   "1.0.0",
			v2:   "1.0.0",
			want: 0,
		},
		{
			name: "v1 less than v2",
			v1:   "1.0.0",
			v2:   "2.0.0",
			want: -1,
		},
		{
			name: "v1 greater than v2",
			v1:   "2.0.0",
			v2:   "1.0.0",
			want: 1,
		},
		{
			name: "patch version comparison",
			v1:   "1.0.1",
			v2:   "1.0.0",
			want: 1,
		},
		{
			name: "minor version comparison",
			v1:   "1.1.0",
			v2:   "1.0.9",
			want: 1,
		},
		{
			name: "prerelease less than release",
			v1:   "1.0.0-rc1",
			v2:   "1.0.0",
			want: -1,
		},
		{
			name: "v prefix stripped",
			v1:   "v1.0.0",
			v2:   "1.0.0",
			want: 0,
		},
		{
			name: "date-based versions",
			v1:   "20240115",
			v2:   "20240114",
			want: 1,
		},
		{
			name: "mixed format fallback to string",
			v1:   "abc",
			v2:   "def",
			want: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.v1, tt.v2); got != tt.want {
				t.Errorf("Compare(%q, %q) = %v, want %v", tt.v1, tt.v2, got, tt.want)
			}
		})
	}
}

func TestAtAPlaceholderGPTFlag(t *testing.T) {
	// @a is the GPT NoAuto flag: it must match exactly "0" or "1", not
	// architecture strings like "arm64" or "amd64".
	const pattern = "mypart-@v_@a.raw"
	p, err := ParsePattern(pattern)
	if err != nil {
		t.Fatalf("ParsePattern(%q) error = %v", pattern, err)
	}

	for _, flag := range []string{"0", "1"} {
		filename := fmt.Sprintf("mypart-1.0.0_%s.raw", flag)
		v, ok := p.ExtractVersion(filename)
		if !ok {
			t.Errorf("ExtractVersion(%q) should match", filename)
			continue
		}
		if v != "1.0.0" {
			t.Errorf("ExtractVersion(%q) version = %q, want %q", filename, v, "1.0.0")
		}
	}

	// Architecture strings should not be accepted as @a values.
	for _, arch := range []string{"amd64", "arm64", "x86-64", "riscv64"} {
		filename := fmt.Sprintf("mypart-1.0.0_%s.raw", arch)
		if p.Matches(filename) {
			t.Errorf("Matches(%q) = true, want false (arch strings are not valid @a flags)", filename)
		}
	}
}

func TestPattern_FedoraSysextsStyleWithSpecifierExpansion(t *testing.T) {
	// Fedora-sysexts pattern: <name>-@v-%w-%a.raw
	// Example: docker-1.0.0-39-x86-64.raw (version 39 = Fedora 39)
	tests := []struct {
		name            string
		originalPattern string // Pattern with %w and %a specifiers
		osVersion       string // Expanded %w value (VERSION_ID from os-release)
		arch            string // Expanded %a value (systemd arch)
		filename        string // Filename to match
		expectVersion   string // Expected extracted version
		expectMatch     bool
	}{
		{
			name:            "fedora-sysexts pattern fedora 39 x86-64",
			originalPattern: "docker-@v-%w-%a.raw",
			osVersion:       "39",
			arch:            "x86-64",
			filename:        "docker-1.0.0-39-x86-64.raw",
			expectVersion:   "1.0.0",
			expectMatch:     true,
		},
		{
			name:            "fedora-sysexts pattern ubuntu 22.04 arm64",
			originalPattern: "htop-@v-%w-%a.raw",
			osVersion:       "22.04",
			arch:            "arm64",
			filename:        "htop-7.2.0-22.04-arm64.raw",
			expectVersion:   "7.2.0",
			expectMatch:     true,
		},
		{
			name:            "fedora-sysexts pattern with complex version",
			originalPattern: "docker-@v-%w-%a.raw",
			osVersion:       "39",
			arch:            "x86-64",
			filename:        "docker-5:29.1.5-rc1-39-x86-64.raw",
			expectVersion:   "5:29.1.5-rc1",
			expectMatch:     true,
		},
		{
			name:            "fedora-sysexts pattern with xz compression",
			originalPattern: "docker-@v-%w-%a.raw.xz",
			osVersion:       "39",
			arch:            "arm64",
			filename:        "docker-29.0.0-39-arm64.raw.xz",
			expectVersion:   "29.0.0",
			expectMatch:     true,
		},
		{
			name:            "fedora-sysexts pattern os version mismatch",
			originalPattern: "docker-@v-%w-%a.raw",
			osVersion:       "39",
			arch:            "x86-64",
			filename:        "docker-1.0.0-38-x86-64.raw",
			expectVersion:   "",
			expectMatch:     false,
		},
		{
			name:            "fedora-sysexts pattern arch mismatch",
			originalPattern: "docker-@v-%w-%a.raw",
			osVersion:       "39",
			arch:            "x86-64",
			filename:        "docker-1.0.0-39-arm64.raw",
			expectVersion:   "",
			expectMatch:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate specifier expansion for both %w and %a
			expandedPattern := strings.ReplaceAll(tt.originalPattern, "%w", tt.osVersion)
			expandedPattern = strings.ReplaceAll(expandedPattern, "%a", tt.arch)

			p, err := ParsePattern(expandedPattern)
			if err != nil {
				t.Fatalf("ParsePattern failed: %v", err)
			}

			if p.Matches(tt.filename) != tt.expectMatch {
				t.Errorf("Matches(%q) = %v, want %v", tt.filename, p.Matches(tt.filename), tt.expectMatch)
			}

			if tt.expectMatch {
				version, ok := p.ExtractVersion(tt.filename)
				if !ok {
					t.Errorf("ExtractVersion(%q) returned ok=false, want true", tt.filename)
				}
				if version != tt.expectVersion {
					t.Errorf("ExtractVersion(%q) = %q, want %q", tt.filename, version, tt.expectVersion)
				}
			}
		})
	}
}

func TestExtractVersionMulti_FedoraSysextsPattern(t *testing.T) {
	// Test ExtractVersionMulti with fedora-sysexts pattern
	// This test verifies that ExtractVersionMulti correctly matches and extracts versions
	tests := []struct {
		name              string
		originalPatterns  []string // Patterns with unexpanded specifiers
		expandedPatterns  []string // Patterns after specifier expansion
		filename          string   // Filename to match
		expectVersion     string   // Expected version
		expectOrigPattern string   // Expected matched pattern (original, unexpanded)
		expectOK          bool
	}{
		{
			name:              "fedora-sysexts pattern matches",
			originalPatterns:  []string{"docker-@v-%w-%a.raw"},
			expandedPatterns:  []string{"docker-@v-39-x86-64.raw"},
			filename:          "docker-1.0.0-39-x86-64.raw",
			expectVersion:     "1.0.0",
			expectOrigPattern: "docker-@v-%w-%a.raw",
			expectOK:          true,
		},
		{
			name:              "fedora-sysexts pattern with compression",
			originalPatterns:  []string{"docker-@v-%w-%a.raw.xz"},
			expandedPatterns:  []string{"docker-@v-39-x86-64.raw.xz"},
			filename:          "docker-1.0.0-39-x86-64.raw.xz",
			expectVersion:     "1.0.0",
			expectOrigPattern: "docker-@v-%w-%a.raw.xz",
			expectOK:          true,
		},
		{
			name:              "no pattern matches",
			originalPatterns:  []string{"docker-@v-%w-%a.raw"},
			expandedPatterns:  []string{"docker-@v-39-x86-64.raw"},
			filename:          "htop-1.0.0.raw",
			expectVersion:     "",
			expectOrigPattern: "",
			expectOK:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// In real usage, patterns would be expanded first (specifiers replaced).
			// We test with the expanded patterns and track which original pattern matched.
			version, matched, ok := ExtractVersionMulti(tt.filename, tt.expandedPatterns)

			if ok != tt.expectOK {
				t.Errorf("ExtractVersionMulti() ok = %v, want %v", ok, tt.expectOK)
			}

			if ok {
				if version != tt.expectVersion {
					t.Errorf("ExtractVersionMulti() version = %q, want %q", version, tt.expectVersion)
				}
				// Find which original pattern corresponds to the matched expanded pattern
				var matchedOrig string
				for i, exp := range tt.expandedPatterns {
					if exp == matched {
						matchedOrig = tt.originalPatterns[i]
						break
					}
				}
				if matchedOrig != tt.expectOrigPattern {
					t.Errorf("ExtractVersionMulti() matched original = %q, want %q", matchedOrig, tt.expectOrigPattern)
				}
			}
		})
	}
}

func TestParsePatterns(t *testing.T) {
	t.Run("valid patterns", func(t *testing.T) {
		patterns := ParsePatterns([]string{"app_@v.raw", "app_@v.raw.xz"})
		if len(patterns) != 2 {
			t.Fatalf("expected 2 patterns, got %d", len(patterns))
		}
		if patterns[0].Raw() != "app_@v.raw" {
			t.Errorf("expected raw = %q, got %q", "app_@v.raw", patterns[0].Raw())
		}
	})

	t.Run("skips invalid patterns", func(t *testing.T) {
		patterns := ParsePatterns([]string{"app_@v.raw", "no-version", ""})
		if len(patterns) != 1 {
			t.Fatalf("expected 1 pattern, got %d", len(patterns))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		patterns := ParsePatterns(nil)
		if len(patterns) != 0 {
			t.Fatalf("expected 0 patterns, got %d", len(patterns))
		}
	})
}

func TestExtractVersionParsed(t *testing.T) {
	patterns := ParsePatterns([]string{"app_@v.raw", "app_@v.raw.xz"})

	t.Run("matches first pattern", func(t *testing.T) {
		v, matched, ok := ExtractVersionParsed("app_1.2.3.raw", patterns)
		if !ok {
			t.Fatal("expected match")
		}
		if v != "1.2.3" {
			t.Errorf("version = %q, want %q", v, "1.2.3")
		}
		if matched != "app_@v.raw" {
			t.Errorf("matched = %q, want %q", matched, "app_@v.raw")
		}
	})

	t.Run("matches second pattern", func(t *testing.T) {
		v, matched, ok := ExtractVersionParsed("app_2.0.0.raw.xz", patterns)
		if !ok {
			t.Fatal("expected match")
		}
		if v != "2.0.0" {
			t.Errorf("version = %q, want %q", v, "2.0.0")
		}
		if matched != "app_@v.raw.xz" {
			t.Errorf("matched = %q, want %q", matched, "app_@v.raw.xz")
		}
	})

	t.Run("no match", func(t *testing.T) {
		_, _, ok := ExtractVersionParsed("other_1.0.0.raw", patterns)
		if ok {
			t.Error("expected no match")
		}
	})
}

func TestSort(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		want     []string
	}{
		{
			name:     "semantic versions",
			versions: []string{"1.0.0", "2.0.0", "1.5.0", "1.0.1"},
			want:     []string{"2.0.0", "1.5.0", "1.0.1", "1.0.0"},
		},
		{
			name:     "with prereleases",
			versions: []string{"1.0.0", "1.0.0-rc1", "1.0.0-beta"},
			want:     []string{"1.0.0", "1.0.0-rc1", "1.0.0-beta"},
		},
		{
			name:     "date versions",
			versions: []string{"20240101", "20240115", "20240110"},
			want:     []string{"20240115", "20240110", "20240101"},
		},
		{
			name:     "single version",
			versions: []string{"1.0.0"},
			want:     []string{"1.0.0"},
		},
		{
			name:     "empty slice",
			versions: []string{},
			want:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			versions := make([]string, len(tt.versions))
			copy(versions, tt.versions)

			Sort(versions)

			if len(versions) != len(tt.want) {
				t.Errorf("Sort() len = %v, want %v", len(versions), len(tt.want))
				return
			}

			for i := range versions {
				if versions[i] != tt.want[i] {
					t.Errorf("Sort()[%d] = %v, want %v", i, versions[i], tt.want[i])
				}
			}
		})
	}
}
