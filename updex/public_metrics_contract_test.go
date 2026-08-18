package updex

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestPublicMetricsIndexIsSubstantive pins the ACMM public-metrics tree:
// docs/metrics/ must be a real directory (a symlink would read as a blob to
// the evaluator) whose README indexes the public signals and points at the
// canonical acceptance-metric spec (ADR-0012).
func TestPublicMetricsIndexIsSubstantive(t *testing.T) {
	info, err := os.Lstat("../docs/metrics")
	if err != nil {
		t.Fatalf("stat public metrics directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("docs/metrics must be a real directory, not a symlink")
	}

	data, err := os.ReadFile("../docs/metrics/README.md")
	if err != nil {
		t.Fatalf("read public metrics index: %v", err)
	}
	contents := string(data)
	for _, required := range []string{
		"# Public metrics",
		"## Signal index",
		"## Pull-request acceptance",
		"## Publication and privacy contract",
		"https://github.com/frostyard/updex/actions/workflows/test.yml",
		"https://codecov.io/gh/frostyard/updex",
		"../specs/pr-acceptance-metric.md",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("public metrics index is missing %q", required)
		}
	}
}

// TestPRAcceptanceMetricSpecIsCanonical pins the acceptance-metric spec and
// its docs/metrics.md conformance alias (ADR-0012). scripts/check-docs.mjs
// pins the same headings; this test keeps the formula from drifting.
func TestPRAcceptanceMetricSpecIsCanonical(t *testing.T) {
	data, err := os.ReadFile("../docs/specs/pr-acceptance-metric.md")
	if err != nil {
		t.Fatalf("read acceptance metric spec: %v", err)
	}
	contents := string(data)
	for _, required := range []string{
		"## Definition",
		"## Rules",
		"accepted PRs / (accepted PRs + closed, unmerged PRs) × 100",
		"gh pr list --repo frostyard/updex",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("acceptance metric spec is missing %q", required)
		}
	}

	target, err := os.Readlink("../docs/metrics.md")
	if err != nil {
		t.Fatalf("docs/metrics.md must be a symlink alias: %v", err)
	}
	if want := "specs/pr-acceptance-metric.md"; target != want {
		t.Errorf("docs/metrics.md -> %q, want %q", target, want)
	}
}

func TestAutoQATuningTargetsCanonicalAcceptanceMetric(t *testing.T) {
	data, err := os.ReadFile("../.github/auto-qa-tuning.json")
	if err != nil {
		t.Fatalf("read Auto-QA tuning policy: %v", err)
	}
	var policy struct {
		Signals map[string]string `json:"signals"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatalf("parse Auto-QA tuning policy: %v", err)
	}
	if got, want := policy.Signals["pr_acceptance_rate"], "docs/specs/pr-acceptance-metric.md#definition"; got != want {
		t.Errorf("pr_acceptance_rate signal = %q, want %q", got, want)
	}
}
