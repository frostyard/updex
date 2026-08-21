package updex

import (
	"os"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestSnapshotWorkflowCancelsStaleRollingReleases pins the rolling-dev-release
// contract from frostyard/core ADR-0034 (and AGENTS.md "Release Automation"):
// snapshot.yml runs after a successful Tests run on main, and every run shares
// the goreleaser-nightly concurrency group with cancel-in-progress enabled so
// concurrent GoReleaser uploads to the singleton dev release cannot collide.
func TestSnapshotWorkflowCancelsStaleRollingReleases(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/snapshot.yml")
	if err != nil {
		t.Fatalf("read snapshot workflow: %v", err)
	}

	var workflow struct {
		On struct {
			WorkflowRun struct {
				Workflows []string `yaml:"workflows"`
				Types     []string `yaml:"types"`
				Branches  []string `yaml:"branches"`
			} `yaml:"workflow_run"`
		} `yaml:"on"`
		Concurrency struct {
			Group            string `yaml:"group"`
			CancelInProgress any    `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Jobs map[string]struct {
			If    string `yaml:"if"`
			Steps []struct {
				Uses string         `yaml:"uses"`
				With map[string]any `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse snapshot workflow: %v", err)
	}

	if got := workflow.Concurrency.Group; got != "goreleaser-nightly" {
		t.Errorf("concurrency.group = %q, want %q (core ADR-0034: nightlies share one group)", got, "goreleaser-nightly")
	}
	// Parse the boolean, not the string: `cancel-in-progress: "true"` is a
	// string YAML scalar and GitHub treats it differently from the boolean.
	if got, ok := workflow.Concurrency.CancelInProgress.(bool); !ok || !got {
		t.Errorf("concurrency.cancel-in-progress = %v (%T), want boolean true (core ADR-0034: cancel stale rolling dev releases)",
			workflow.Concurrency.CancelInProgress, workflow.Concurrency.CancelInProgress)
	}

	trigger := workflow.On.WorkflowRun
	if !slices.Contains(trigger.Workflows, "Tests") {
		t.Errorf("on.workflow_run.workflows = %v, want it to include %q (core ADR-0034: snapshot follows the Tests workflow)", trigger.Workflows, "Tests")
	}
	if !slices.Contains(trigger.Types, "completed") {
		t.Errorf("on.workflow_run.types = %v, want it to include %q (core ADR-0034)", trigger.Types, "completed")
	}
	if !slices.Contains(trigger.Branches, "main") {
		t.Errorf("on.workflow_run.branches = %v, want it to include %q (core ADR-0034: snapshot follows main)", trigger.Branches, "main")
	}

	job, ok := workflow.Jobs["snapshot"]
	if !ok {
		t.Fatal("snapshot workflow is missing the snapshot job (core ADR-0034)")
	}
	if !strings.Contains(job.If, "github.event.workflow_run.conclusion == 'success'") {
		t.Errorf("snapshot job guard = %q, want it to require github.event.workflow_run.conclusion == 'success' (core ADR-0034: only a successful Tests run publishes the dev release)", job.If)
	}

	const wantRef = "${{ github.event.workflow_run.head_sha }}"
	var checkoutSteps []struct {
		Uses string
		With map[string]any
	}
	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, "actions/checkout@") {
			checkoutSteps = append(checkoutSteps, struct {
				Uses string
				With map[string]any
			}{Uses: step.Uses, With: step.With})
		}
	}
	if len(checkoutSteps) != 1 {
		t.Fatalf("snapshot job has %d actions/checkout steps, want exactly 1 so the tested commit is unambiguous", len(checkoutSteps))
	}
	if got, ok := checkoutSteps[0].With["ref"].(string); !ok || got != wantRef {
		t.Errorf("snapshot checkout ref = %v (%T), want exactly %q so publication uses the commit whose Tests run succeeded",
			checkoutSteps[0].With["ref"], checkoutSteps[0].With["ref"], wantRef)
	}
}
