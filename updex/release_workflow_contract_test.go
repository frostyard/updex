package updex

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestReleaseWorkflowDispatchesSnosiBuildForTags(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}

	var workflow struct {
		On struct {
			Push struct {
				Tags     []string `yaml:"tags"`
				Branches []string `yaml:"branches"`
			} `yaml:"push"`
		} `yaml:"on"`
		Jobs map[string]struct {
			If    string `yaml:"if"`
			Steps []struct {
				If   string            `yaml:"if"`
				Uses string            `yaml:"uses"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse release workflow: %v", err)
	}

	if len(workflow.On.Push.Tags) == 0 {
		t.Fatal("release workflow must run on tag pushes")
	}
	if len(workflow.On.Push.Branches) != 0 {
		t.Fatalf("release workflow branch filters = %v, want tag-only push trigger", workflow.On.Push.Branches)
	}

	job, ok := workflow.Jobs["goreleaser"]
	if !ok {
		t.Fatal("release workflow is missing goreleaser job")
	}
	if job.If != "" {
		t.Fatalf("goreleaser job has guard %q; tag releases must reach the snosi dispatch", job.If)
	}

	for _, step := range job.Steps {
		if !strings.HasPrefix(step.Uses, "peter-evans/repository-dispatch@") {
			continue
		}
		if step.With["repository"] != "frostyard/snosi" ||
			step.With["event-type"] != "build" {
			continue
		}
		if step.If != "" {
			t.Fatalf("snosi dispatch has guard %q; tag releases must reach it", step.If)
		}
		return
	}

	t.Fatal("release workflow must dispatch event-type build to frostyard/snosi")
}

// TestReleaseWorkflowAttestsBuildProvenance pins the provenance contract:
// the tag release workflow grants id-token: write and attestations: write and
// runs actions/attest-build-provenance (pinned to a full commit SHA) over a
// subject-path that includes checksums.txt, so a published artifact carries an
// authenticity signal beyond the same-origin checksums.txt (README
// "Installation": `gh attestation verify <artifact> --repo frostyard/updex`).
// The workflow only runs on a tag push, so this test is the pull-request gate.
func TestReleaseWorkflowAttestsBuildProvenance(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}

	var workflow struct {
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Steps []struct {
				Uses string            `yaml:"uses"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse release workflow: %v", err)
	}

	for _, scope := range []string{"id-token", "attestations"} {
		if got := workflow.Permissions[scope]; got != "write" {
			t.Errorf("permissions.%s = %q, want %q (actions/attest-build-provenance needs it)", scope, got, "write")
		}
	}

	job, ok := workflow.Jobs["goreleaser"]
	if !ok {
		t.Fatal("release workflow is missing goreleaser job")
	}
	pinned := regexp.MustCompile(`^actions/attest-build-provenance@[0-9a-f]{40}$`)
	for _, step := range job.Steps {
		if !strings.HasPrefix(step.Uses, "actions/attest-build-provenance@") {
			continue
		}
		if !pinned.MatchString(step.Uses) {
			t.Errorf("attest step uses = %q, want actions/attest-build-provenance pinned to a 40-character commit SHA", step.Uses)
		}
		if !strings.Contains(step.With["subject-path"], "checksums.txt") {
			t.Errorf("attest step with.subject-path = %q, want it to include checksums.txt", step.With["subject-path"])
		}
		return
	}
	t.Fatal("release workflow must run actions/attest-build-provenance over the release artifacts")
}
