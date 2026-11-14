package sysext

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/version"
)

// switchTransfer is the transfer "foo" with the target pattern foo_@v_%w.raw
// as config loads it on a host whose VERSION_ID is 14.
func switchTransfer(dir string) *config.Transfer {
	return &config.Transfer{
		Component: "foo",
		Target: config.TargetSection{
			Path:             dir,
			MatchPattern:     "foo_@v_14.raw",
			MatchPatterns:    []string{"foo_@v_14.raw"},
			AnySuitePatterns: []string{"foo_@v_" + version.SuiteWildcard + ".raw"},
		},
	}
}

func TestLinkedOtherSuiteAt(t *testing.T) {
	tests := []struct {
		name      string
		noSuite   bool   // the transfer has no %w pattern
		link      string // the link target's base name; "" = no link
		regular   bool   // the link path is a regular file
		wantImage string
		wantSuite string
	}{
		{name: "other suite", link: "foo_1.0_13.raw", wantImage: "foo_1.0_13.raw", wantSuite: "13"},
		{name: "other suite, image missing", link: "foo_9.0_12.raw", wantImage: "foo_9.0_12.raw", wantSuite: "12"},
		{name: "running suite", link: "foo_1.0_14.raw"},
		{name: "no link"},
		{name: "not an image of the transfer", link: "bar_1.0_13.raw"},
		{name: "regular file at the link path", link: "foo_1.0_13.raw", regular: true},
		{name: "transfer without %w", noSuite: true, link: "foo_1.0_13.raw"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage := t.TempDir()
			links := t.TempDir()
			tr := switchTransfer(stage)
			if tt.noSuite {
				tr.Target.AnySuitePatterns = nil
			}
			if err := os.WriteFile(filepath.Join(stage, "foo_1.0_13.raw"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			linkPath := filepath.Join(links, "foo.raw")
			switch {
			case tt.regular:
				if err := os.WriteFile(linkPath, nil, 0o644); err != nil {
					t.Fatal(err)
				}
			case tt.link != "":
				if err := os.Symlink(filepath.Join(stage, tt.link), linkPath); err != nil {
					t.Fatal(err)
				}
			}

			image, suite, err := LinkedOtherSuiteAt(tr, links)
			if err != nil {
				t.Fatalf("LinkedOtherSuiteAt() error = %v", err)
			}
			if image != tt.wantImage || suite != tt.wantSuite {
				t.Errorf("LinkedOtherSuiteAt() = %q, %q; want %q, %q", image, suite, tt.wantImage, tt.wantSuite)
			}
		})
	}
}
