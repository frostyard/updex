package updex

import (
	"os"
	"regexp"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestDependabotCommitPrefixIsNotAFeature pins the Dependabot commit prefixes
// that `make bump` depends on. The repository squash-merges with the PR title,
// and svu derives the next version from those titles: a `feat(deps):` title
// made `svu next` propose a minor release for dependency bumps alone (v2.0.4).
// Go modules use `build`, GitHub Actions use `ci`, and `include: scope` adds
// the `(deps)` scope.
func TestDependabotCommitPrefixIsNotAFeature(t *testing.T) {
	data, err := os.ReadFile("../.github/dependabot.yml")
	if err != nil {
		t.Fatalf("read dependabot config: %v", err)
	}

	var config struct {
		Updates []struct {
			Ecosystem     string `yaml:"package-ecosystem"`
			CommitMessage struct {
				Prefix  string `yaml:"prefix"`
				Include string `yaml:"include"`
			} `yaml:"commit-message"`
		} `yaml:"updates"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse dependabot config: %v", err)
	}

	want := map[string]string{"gomod": "build", "github-actions": "ci"}
	seen := map[string]bool{}
	for _, update := range config.Updates {
		seen[update.Ecosystem] = true
		wantPrefix, ok := want[update.Ecosystem]
		if !ok {
			t.Errorf("ecosystem %q has no pinned prefix; add it to this test with a non-feat prefix", update.Ecosystem)
			continue
		}
		if got := update.CommitMessage.Prefix; got != wantPrefix {
			t.Errorf("%s commit-message.prefix = %q, want %q (feat would make svu propose a minor release)", update.Ecosystem, got, wantPrefix)
		}
		if got := update.CommitMessage.Include; got != "scope" {
			t.Errorf("%s commit-message.include = %q, want %q (gives the (deps) scope)", update.Ecosystem, got, "scope")
		}
	}
	for ecosystem := range want {
		if !seen[ecosystem] {
			t.Errorf("dependabot config has no %q update", ecosystem)
		}
	}
}

// TestGoreleaserGroupsDependabotTitles pins the changelog group that collects
// Dependabot commits: it must match the build/ci prefixes above (and the
// legacy feat(deps) titles), and it must come before "New Features", because
// GoReleaser puts a commit in the first group that matches it.
func TestGoreleaserGroupsDependabotTitles(t *testing.T) {
	data, err := os.ReadFile("../.goreleaser.yaml")
	if err != nil {
		t.Fatalf("read goreleaser config: %v", err)
	}

	var config struct {
		Changelog struct {
			Groups []struct {
				Title  string `yaml:"title"`
				Regexp string `yaml:"regexp"`
			} `yaml:"groups"`
		} `yaml:"changelog"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse goreleaser config: %v", err)
	}

	depsIndex, featuresIndex := -1, -1
	var deps *regexp.Regexp
	for i, group := range config.Changelog.Groups {
		switch group.Title {
		case "Dependency updates":
			depsIndex = i
			deps, err = regexp.Compile(group.Regexp)
			if err != nil {
				t.Fatalf("compile Dependency updates regexp %q: %v", group.Regexp, err)
			}
		case "New Features":
			featuresIndex = i
		}
	}
	if depsIndex < 0 || featuresIndex < 0 {
		t.Fatalf("changelog groups missing: Dependency updates at %d, New Features at %d", depsIndex, featuresIndex)
	}
	if depsIndex > featuresIndex {
		t.Errorf("Dependency updates group (%d) must come before New Features (%d)", depsIndex, featuresIndex)
	}

	tests := []struct {
		title string
		match bool
	}{
		{"build(deps): bump github.com/frostyard/std from 0.2.2 to 0.3.0 (#442)", true},
		{"ci(deps): bump jdx/mise-action from 5.1.2 to 5.2.1", true},
		{"build(deps-dev): bump example.com/tool from 1.0.0 to 1.1.0", true},
		{"feat(deps): bump github.com/ulikunitz/xz from 0.5.16 to 0.5.17 (#422)", true},
		{"feat(catalog): add search", false},
		{"fix(sysext): detect merged images (#445)", false},
		{"ci(make): run the docs-integrity gate inside make ci (#403)", false},
		{"build: bump the Go toolchain", false},
	}
	for _, tt := range tests {
		if got := deps.MatchString(tt.title); got != tt.match {
			t.Errorf("Dependency updates regexp matches %q = %v, want %v", tt.title, got, tt.match)
		}
	}
}
