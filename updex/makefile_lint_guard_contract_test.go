package updex

import (
	"os"
	"strings"
	"testing"
)

// TestMakefileLintRecipeDoesNotSwallowFailures guards against a regression of
// the `make lint` recipe that used `golangci-lint run || echo …`. That form
// treated *every* non-zero exit — including real lint findings — as tolerable,
// so `make lint` (and therefore `make check`) always exited 0. The guarded
// recipe must run golangci-lint unconditionally when it is installed and only
// print the "not installed" message when the binary is genuinely absent.
func TestMakefileLintRecipeDoesNotSwallowFailures(t *testing.T) {
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(data)

	if strings.Contains(makefile, "golangci-lint run || echo") {
		t.Fatalf("Makefile lint recipe uses `golangci-lint run || echo` which swallows real lint failures; guard it behind `command -v golangci-lint` instead")
	}

	lintRecipe := extractRecipe(t, makefile, "lint:")
	if !strings.Contains(lintRecipe, "command -v golangci-lint") {
		t.Fatalf("Makefile lint recipe must gate on `command -v golangci-lint` so a missing binary is distinguished from a lint failure; got:\n%s", lintRecipe)
	}
	if strings.Contains(lintRecipe, "golangci-lint run || ") {
		t.Fatalf("Makefile lint recipe must not `|| <fallback>` a `golangci-lint run` invocation; got:\n%s", lintRecipe)
	}
}

func TestMakefileCIRunsE2EAfterCoverageAndBeforeRace(t *testing.T) {
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	recipe := extractRecipe(t, string(data), "ci:")

	stages := []string{
		"$(MAKE) coverage-check",
		"$(GO) test -v ./tests/e2e/...",
		"$(GO) test -race",
	}
	previous := -1
	for _, stage := range stages {
		index := strings.Index(recipe, stage)
		if index == -1 {
			t.Fatalf("Makefile ci recipe must contain %q; got:\n%s", stage, recipe)
		}
		if index <= previous {
			t.Fatalf("Makefile ci recipe stages are out of order at %q; got:\n%s", stage, recipe)
		}
		previous = index
	}
}

// extractRecipe returns the lines of the recipe whose target line begins with
// target (e.g. "lint:"), i.e. the target line plus the following tab-indented
// command lines, stopping at the next non-indented, non-blank line.
func extractRecipe(t *testing.T, makefile, target string) string {
	t.Helper()
	lines := strings.Split(makefile, "\n")
	var recipe []string
	inRecipe := false
	for _, line := range lines {
		if !inRecipe {
			if strings.HasPrefix(line, target) {
				inRecipe = true
				recipe = append(recipe, line)
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "\t") {
			recipe = append(recipe, line)
			continue
		}
		break
	}
	if !inRecipe {
		t.Fatalf("Makefile has no %q target", target)
	}
	return strings.Join(recipe, "\n")
}
