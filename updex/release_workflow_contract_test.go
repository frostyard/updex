package updex

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestReleaseWorkflowRequestsAptPublicationForTags pins the publication
// contract of frostyard/core ADR-0055 and ADR-0056: a tag release asks
// frostyard/apt-publisher to publish its .deb files with an unguarded
// `publish-deb` repository_dispatch that may not continue on error (if it
// fails, nothing was published). The publisher, not this workflow, dispatches
// `build` to frostyard/snosi once the packages are installable, so a direct
// snosi dispatch or a repogen publish step here would race the publish queue.
func TestReleaseWorkflowRequestsAptPublicationForTags(t *testing.T) {
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
				If              string            `yaml:"if"`
				Uses            string            `yaml:"uses"`
				With            map[string]string `yaml:"with"`
				ContinueOnError yaml.Node         `yaml:"continue-on-error"`
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
		t.Fatalf("goreleaser job has guard %q; tag releases must reach the publication request", job.If)
	}

	requests := 0
	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, "frostyard/repogen/") {
			t.Fatalf("release workflow uses %s; .deb files are published by frostyard/apt-publisher", step.Uses)
		}
		if !strings.HasPrefix(step.Uses, "peter-evans/repository-dispatch@") {
			continue
		}
		switch step.With["repository"] {
		case "frostyard/snosi":
			t.Fatal("release workflow dispatches to frostyard/snosi; frostyard/apt-publisher does after publishing")
		case "frostyard/apt-publisher":
			if step.With["event-type"] != "publish-deb" {
				t.Fatalf("apt-publisher dispatch event-type = %q, want publish-deb", step.With["event-type"])
			}
			if step.If != "" {
				t.Fatalf("publication request has guard %q; tag releases must reach it", step.If)
			}
			if !step.ContinueOnError.IsZero() {
				t.Fatal("publication request sets continue-on-error; a failed request must fail the release")
			}
			payload := step.With["client-payload"]
			for _, want := range []string{`"repo": "${{ github.repository }}"`, `"tag": "${{ github.ref_name }}"`} {
				if !strings.Contains(payload, want) {
					t.Fatalf("publication request client-payload %q lacks %s", payload, want)
				}
			}
			requests++
		}
	}
	if requests != 1 {
		t.Fatalf("release workflow has %d publish-deb requests to frostyard/apt-publisher, want 1", requests)
	}
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
