package updex

import (
	"os"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/imagecompat"
	"go.yaml.in/yaml/v3"
)

// TestPackagesRequireErofsUtils pins the runtime dependency of the packaged
// CLI on erofs-utils, which the minimum image version check needs to read a
// compressed EROFS image with dump.erofs (ADR-0016). Each format writes a
// dependency in its own syntax, so the formats are pinned one by one, and the
// version must be the one imagecompat requires: a bump there fails this test
// until .goreleaser.yaml follows.
func TestPackagesRequireErofsUtils(t *testing.T) {
	data, err := os.ReadFile("../.goreleaser.yaml")
	if err != nil {
		t.Fatalf("read goreleaser config: %v", err)
	}

	var config struct {
		Nfpms []struct {
			Formats []string `yaml:"formats"`
			Depends []string `yaml:"dependencies"`
			// Overrides is keyed by package format; only depends is pinned.
			Overrides map[string]struct {
				Depends []string `yaml:"dependencies"`
			} `yaml:"overrides"`
		} `yaml:"nfpms"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse goreleaser config: %v", err)
	}
	if len(config.Nfpms) != 1 {
		t.Fatalf("nfpms = %d entries, want 1", len(config.Nfpms))
	}
	pkg := config.Nfpms[0]

	wantDepends := map[string]string{
		"deb": "erofs-utils (>= " + imagecompat.ErofsUtilsMinVersion + ")",
		"rpm": "erofs-utils >= " + imagecompat.ErofsUtilsMinVersion,
		"apk": "erofs-utils>=" + imagecompat.ErofsUtilsMinVersion,
	}

	formats := map[string]bool{}
	for _, f := range pkg.Formats {
		formats[f] = true
	}
	for format, want := range wantDepends {
		if !formats[format] {
			t.Errorf("nfpms.formats has no %q package, so its dependency is never written", format)
			continue
		}
		override, ok := pkg.Overrides[format]
		if !ok {
			t.Errorf("nfpms.overrides has no %q entry; want dependencies %q", format, want)
			continue
		}
		if got := dependsOf(override.Depends, "erofs-utils"); got != want {
			t.Errorf("%s dependencies = %q, want %q (nfpm syntax per format)", format, got, want)
		}
	}

	// The base list applies to a format without an override; keep it valid deb
	// syntax so a new format never ships without the dependency.
	if got := dependsOf(pkg.Depends, "erofs-utils"); !strings.Contains(got, "erofs-utils") {
		t.Errorf("nfpms.dependencies = %q, want the erofs-utils dependency as a fallback", got)
	}
}

// dependsOf returns the entry of depends that names pkg, or "".
func dependsOf(depends []string, pkg string) string {
	for _, d := range depends {
		if strings.HasPrefix(d, pkg) {
			return d
		}
	}
	return ""
}
