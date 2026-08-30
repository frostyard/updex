package updex

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// docsGateInvocations are the two commands the `docs-gate` job in
// .github/workflows/test.yml runs. `make ci` claims to be the credential-free
// local equivalent of that workflow, so it must run both as well.
var docsGateInvocations = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{
		name:    "make test-docs-check",
		pattern: regexp.MustCompile(`(?m)^\t@?(?:\$\(MAKE\)|make)(?:\s+--no-print-directory)?\s+test-docs-check\s*$`),
	},
	{
		name:    "node scripts/check-docs.mjs",
		pattern: regexp.MustCompile(`(?m)^\t@?node\s+scripts/check-docs\.mjs\s*$`),
	},
}

// TestMakefileCIRecipeRunsDocsIntegrityGate pins the docs-integrity checks
// into `make ci`. GitHub CI runs `make test-docs-check` and
// `node scripts/check-docs.mjs` in its `docs-gate` job; before this guard the
// local `ci` recipe ran neither, so a broken docs index, a dead relative link,
// or a broken conformance alias passed the local gate and only failed after a
// pull request was already open. The test fails if either invocation is
// dropped from the recipe, is moved after the final "CI gate passed" line, or
// has its failure swallowed.
func TestMakefileCIRecipeRunsDocsIntegrityGate(t *testing.T) {
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}

	recipe := extractRecipe(t, string(data), "ci:")

	passIdx := strings.Index(recipe, "CI gate passed")
	if passIdx < 0 {
		t.Fatalf("Makefile ci recipe no longer prints \"CI gate passed\"; got:\n%s", recipe)
	}

	for _, invocation := range docsGateInvocations {
		loc := invocation.pattern.FindStringIndex(recipe)
		if loc == nil {
			t.Errorf("Makefile ci recipe must invoke `%s` (the docs-gate CI job runs it); got:\n%s", invocation.name, recipe)
			continue
		}
		if loc[0] > passIdx {
			t.Errorf("Makefile ci recipe invokes `%s` after printing \"CI gate passed\"; move it before the success line", invocation.name)
		}
	}

	for _, line := range strings.Split(recipe, "\n") {
		trimmed := strings.TrimPrefix(line, "\t")
		if !strings.Contains(trimmed, "test-docs-check") && !strings.Contains(trimmed, "check-docs.mjs") {
			continue
		}
		if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "@-") {
			t.Errorf("Makefile ci recipe ignores the exit status of a docs-integrity invocation: %q", line)
		}
		if strings.Contains(trimmed, "||") {
			t.Errorf("Makefile ci recipe swallows a docs-integrity failure with a `||` fallback: %q", line)
		}
	}
}
