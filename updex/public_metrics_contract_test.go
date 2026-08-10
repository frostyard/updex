package updex

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPublicMetricsIndexIsSubstantive(t *testing.T) {
	info, err := os.Stat("../docs/metrics")
	if err != nil {
		t.Fatalf("stat public metrics directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("docs/metrics must be a directory")
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
		"accepted PRs / (accepted PRs + closed, unmerged PRs) × 100",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("public metrics index is missing %q", required)
		}
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
	if got, want := policy.Signals["pr_acceptance_rate"], "docs/metrics/README.md#pull-request-acceptance"; got != want {
		t.Errorf("pr_acceptance_rate signal = %q, want %q", got, want)
	}
}
