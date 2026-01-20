package version

import (
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
