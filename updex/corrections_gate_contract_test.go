package updex

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// correctionsGateScript is this repository's owned copy of frostyard/core's
// corrections validator (core ADR-0018, ADR-0081). The local copy is what
// `make ci` runs, so it is never edited here: change it in core, then copy it
// again unchanged.
const correctionsGateScript = "../scripts/check-corrections.mjs"

// correctionsGateBaseEnv is the value the workflow gives BASE when it runs
// `make ci`: the base commit of the pull request, so `make corrections-base`
// can compare the records in .memory/corrections/ with the base tree.
const correctionsGateBaseEnv = "${{ github.event.pull_request.base.sha }}"

// correctionsGateInvocations are the 2 targets the `ci` recipe must run: the
// form check of every record, and the append-only comparison with BASE.
var correctionsGateInvocations = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{
		name:    "make corrections-check",
		pattern: regexp.MustCompile(`(?m)^\t@?(?:\$\(MAKE\)|make)(?:\s+--no-print-directory)?\s+corrections-check\s*$`),
	},
	{
		name:    "make corrections-base",
		pattern: regexp.MustCompile(`(?m)^\t@?(?:\$\(MAKE\)|make)(?:\s+--no-print-directory)?\s+corrections-base\s*$`),
	},
}

// TestCorrectionsGateScriptIsPresent fails when the local copy of the
// corrections validator is missing or is not the validator `make ci` needs.
// Without the script `make corrections-check` and `make corrections-base` both
// fail on a missing file, so the gate would be dropped from CI silently.
func TestCorrectionsGateScriptIsPresent(t *testing.T) {
	data, err := os.ReadFile(correctionsGateScript)
	if err != nil {
		t.Fatalf("read %s: %v (copy frostyard/core's scripts/check-corrections.mjs here unchanged)", correctionsGateScript, err)
	}
	if len(data) == 0 {
		t.Fatalf("%s is empty; copy frostyard/core's script here unchanged", correctionsGateScript)
	}

	script := string(data)
	for _, want := range []string{".memory/corrections", "--base"} {
		if !strings.Contains(script, want) {
			t.Errorf("%s does not mention %q; it must be the unchanged copy of frostyard/core's corrections validator (core ADR-0081)", correctionsGateScript, want)
		}
	}
}

// TestMakefileCIRecipeRunsCorrectionsGate pins the corrections gate into
// `make ci`. The records in .memory/corrections/ are only protected when the
// local gate checks their form and compares them with BASE, so a record that
// is edited, renamed or removed fails before a pull request exists. The test
// fails if either invocation is dropped from the recipe (or its prerequisites),
// is placed after the final success line, or has its failure swallowed.
func TestMakefileCIRecipeRunsCorrectionsGate(t *testing.T) {
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(data)

	recipe := makefileTargetWithPrerequisites(t, makefile, "ci:", 0)

	passIdx := strings.Index(recipe, "CI gate passed")
	if passIdx < 0 {
		t.Fatalf("Makefile ci recipe no longer prints \"CI gate passed\"; got:\n%s", recipe)
	}

	for _, invocation := range correctionsGateInvocations {
		loc := invocation.pattern.FindStringIndex(recipe)
		if loc == nil {
			t.Errorf("Makefile ci recipe must run `%s` so a changed, renamed or removed correction record fails the gate (core ADR-0081); got:\n%s", invocation.name, recipe)
			continue
		}
		if loc[0] > passIdx {
			t.Errorf("Makefile ci recipe runs `%s` after printing \"CI gate passed\"; move it before the success line", invocation.name)
		}
	}

	for _, line := range strings.Split(recipe, "\n") {
		if !strings.HasPrefix(line, "\t") {
			continue
		}
		trimmed := strings.TrimPrefix(line, "\t")
		if !strings.Contains(trimmed, "corrections-check") && !strings.Contains(trimmed, "corrections-base") {
			continue
		}
		if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "@-") {
			t.Errorf("Makefile ci recipe ignores the exit status of a corrections invocation: %q", line)
		}
		if strings.Contains(trimmed, "||") {
			t.Errorf("Makefile ci recipe swallows a corrections failure with a `||` fallback: %q", line)
		}
	}
}

// TestMakefileCorrectionsCheckRecipeRunsValidator pins the corrections-check
// recipe itself. A `ci` recipe that calls the target proves nothing when the
// target's own recipe is empty: on a push or merge-group run `BASE` is empty,
// so the append-only comparison never runs, and the form check is then the only
// validation left. The recipe must run the validator, and it must not ignore
// its exit status.
func TestMakefileCorrectionsCheckRecipeRunsValidator(t *testing.T) {
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	recipe := extractRecipe(t, string(data), "corrections-check:")

	pattern := regexp.MustCompile(`(?m)^\t@?node\s+scripts/check-corrections\.mjs(\s.*)?$`)
	if !pattern.MatchString(recipe) {
		t.Fatalf("Makefile corrections-check recipe must run `node scripts/check-corrections.mjs` (an empty recipe validates nothing on a push or merge-group run, where BASE is empty); got:\n%s", recipe)
	}

	for _, line := range strings.Split(recipe, "\n") {
		trimmed := strings.TrimPrefix(line, "\t")
		if !strings.Contains(trimmed, "check-corrections.mjs") {
			continue
		}
		if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "@-") {
			t.Errorf("Makefile corrections-check recipe ignores the validator's exit status: %q", line)
		}
		if strings.Contains(trimmed, "||") {
			t.Errorf("Makefile corrections-check recipe swallows a validator failure with a `||` fallback: %q", line)
		}
	}
}

// TestMakefileCorrectionsBaseRecipeIsAppendOnly pins the corrections-base
// recipe: it compares the records with BASE when BASE holds the script, skips
// the comparison with a notice when BASE predates the gate, and refuses a BASE
// that does not resolve to a commit. That last refusal is the important one: a
// typo or an unfetched ref must fail the gate instead of passing it as an old
// base that had no rule to compare.
func TestMakefileCorrectionsBaseRecipeIsAppendOnly(t *testing.T) {
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	recipe := extractRecipe(t, string(data), "corrections-base:")

	for _, want := range []struct {
		text string
		why  string
	}{
		{text: `[ -z "$(BASE)" ]`, why: "it must exit 0 at once when BASE is empty (a push or merge-group run has no base commit)"},
		{text: "rev-parse --verify --quiet", why: "it must verify that BASE resolves"},
		{text: "^{commit}", why: "it must verify that BASE resolves to a commit"},
		{text: "does not resolve to a commit", why: "it must refuse a BASE that does not resolve, instead of comparing with nothing"},
		{text: "exit 1", why: "a BASE that does not resolve must fail the gate"},
		{text: "git cat-file -e", why: "it must ask whether BASE holds scripts/check-corrections.mjs"},
		{text: "predates the corrections gate", why: "it must skip the comparison, with a notice, when BASE had no corrections rule"},
		{text: `--base "$(BASE)"`, why: "it must run the append-only comparison against BASE"},
	} {
		if !strings.Contains(recipe, want.text) {
			t.Errorf("Makefile corrections-base recipe lost %q: %s; got:\n%s", want.text, want.why, recipe)
		}
	}

	// The refusal must come before the comparison: a BASE that does not
	// resolve must never reach the script (which is fatal, but the recipe
	// states the rule itself, in a message that names the fix).
	refuseIdx := strings.Index(recipe, "does not resolve to a commit")
	compareIdx := strings.Index(recipe, "check-corrections.mjs")
	if refuseIdx >= 0 && compareIdx >= 0 && refuseIdx > compareIdx {
		t.Errorf("Makefile corrections-base recipe compares with BASE before it refuses a BASE that does not resolve; got:\n%s", recipe)
	}
}

// TestVerifyWorkflowPassesCorrectionsBase pins the workflow contract of the
// corrections gate: the job that runs `make ci` gives the step BASE (the pull
// request's base commit) and checks out the full history (`fetch-depth: 0`),
// because `make corrections-base` reads the base tree with git. Without them
// the comparison never runs in CI, and a pull request could change or remove a
// record and still pass.
func TestVerifyWorkflowPassesCorrectionsBase(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("read workflows/ci.yml: %v", err)
	}

	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string         `yaml:"name"`
				Uses string         `yaml:"uses"`
				Run  string         `yaml:"run"`
				Env  map[string]any `yaml:"env"`
				With map[string]any `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse workflows/ci.yml: %v", err)
	}

	checked := 0
	jobNames := make([]string, 0, len(workflow.Jobs))
	for name := range workflow.Jobs {
		jobNames = append(jobNames, name)
	}
	slices.Sort(jobNames)
	for _, name := range jobNames {
		job := workflow.Jobs[name]
		runsMakeCI := false
		for _, step := range job.Steps {
			if strings.TrimSpace(step.Run) == "make ci" {
				runsMakeCI = true
			}
		}
		if !runsMakeCI {
			continue
		}
		checked++

		for _, step := range job.Steps {
			if strings.TrimSpace(step.Run) != "make ci" {
				continue
			}
			if got := envValue(step.Env["BASE"]); got != correctionsGateBaseEnv {
				t.Errorf("job %q: the `make ci` step sets BASE to %q, want %q (make corrections-base compares the records with the pull request's base commit)", name, got, correctionsGateBaseEnv)
			}
		}

		fetchDepth := ""
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "actions/checkout@") {
				continue
			}
			fetchDepth = fmt.Sprint(step.With["fetch-depth"])
			if fetchDepth == "0" {
				if got, ok := step.With["persist-credentials"]; ok && got != false {
					t.Errorf("job %q: the checkout step must keep persist-credentials: false, got %v", name, got)
				}
				break
			}
		}
		if fetchDepth != "0" {
			t.Errorf("job %q: the checkout step sets fetch-depth to %q, want 0 (make corrections-base reads the base tree with git)", name, fetchDepth)
		}
	}

	if checked == 0 {
		t.Fatal("no workflow job runs `make ci`; the Verify job must run the whole local gate")
	}
}

// envValue renders a workflow env value, which may be a string or a number.
func envValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

// makefileTargetWithPrerequisites returns the recipe of target plus the
// recipes of the prerequisites named on its target line, recursively, so a
// check that moved into a prerequisite of `ci` still counts.
func makefileTargetWithPrerequisites(t *testing.T, makefile, target string, depth int) string {
	t.Helper()
	recipe := extractRecipe(t, makefile, target)
	if depth > 2 {
		return recipe
	}

	var text strings.Builder
	text.WriteString(recipe)
	text.WriteString("\n")

	targetLine, _, _ := strings.Cut(recipe, "\n")
	_, prerequisites, ok := strings.Cut(targetLine, ":")
	if !ok {
		return text.String()
	}
	for _, prerequisite := range strings.Fields(prerequisites) {
		if prerequisite == "" || strings.ContainsAny(prerequisite, "$()") {
			continue
		}
		if !strings.Contains(makefile, "\n"+prerequisite+":") && !strings.HasPrefix(makefile, prerequisite+":") {
			// A file prerequisite (for example "coverage.out"), not a target.
			continue
		}
		text.WriteString(makefileTargetWithPrerequisites(t, makefile, prerequisite+":", depth+1))
	}
	return text.String()
}
