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

// TestReleaseWorkflowGatesPublishOnCI pins the release gate: a tag push is the
// only trigger that publishes, and a tag need not point at a commit the Tests
// workflow ever saw green. The `goreleaser` job must therefore depend on a job
// that runs the project's standard credential-free gate (`make ci`:
// verify-static, unit tests with the coverage floor, e2e, race detector,
// cross-builds) with `permissions: contents: read`, so a failing tag cannot
// reach GoReleaser, the provenance attestation, the R2 publish, or the snosi
// dispatch. The workflow only runs on a tag push, so this test is the pull
// request gate.
func TestReleaseWorkflowGatesPublishOnCI(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}

	var workflow struct {
		Jobs map[string]struct {
			Needs       needsList         `yaml:"needs"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Uses string            `yaml:"uses"`
				Run  string            `yaml:"run"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse release workflow: %v", err)
	}

	publish, ok := workflow.Jobs["goreleaser"]
	if !ok {
		t.Fatal("release workflow is missing goreleaser job")
	}
	if len(publish.Needs) == 0 {
		t.Fatal("goreleaser job declares no `needs`; a tag that fails CI would publish unchecked")
	}

	// Every job the publish job waits on must exist, and at least one of them
	// must run the standard CI gate.
	gated := false
	for _, need := range publish.Needs {
		gate, ok := workflow.Jobs[need]
		if !ok {
			t.Errorf("goreleaser needs %q, which is not a job in this workflow", need)
			continue
		}
		if got := gate.Permissions["contents"]; got != "read" {
			t.Errorf("job %s: permissions.contents = %q, want %q (the gate only reads the tree)", need, got, "read")
		}
		for _, step := range gate.Steps {
			if strings.Contains(step.Run, "make ci") {
				gated = true
			}
		}
		if !gated {
			continue
		}
		var hasCheckout, hasGo bool
		for _, step := range gate.Steps {
			switch {
			case strings.HasPrefix(step.Uses, "actions/checkout@"):
				hasCheckout = true
			case strings.HasPrefix(step.Uses, "actions/setup-go@"):
				hasGo = true
				if step.With["go-version-file"] != "go.mod" {
					t.Errorf("job %s: setup-go with.go-version-file = %q, want %q", need, step.With["go-version-file"], "go.mod")
				}
			}
		}
		if !hasCheckout {
			t.Errorf("job %s runs `make ci` without checking the tree out", need)
		}
		if !hasGo {
			t.Errorf("job %s runs `make ci` without actions/setup-go", need)
		}
	}
	if !gated {
		t.Error("no job the goreleaser job needs runs `make ci`; the publish path is not gated on the standard CI checks")
	}
}

// needsList accepts GitHub Actions' two spellings of `needs:` — a single job
// id or a sequence of them.
type needsList []string

func (n *needsList) UnmarshalYAML(value *yaml.Node) error {
	var one string
	if err := value.Decode(&one); err == nil {
		*n = needsList{one}
		return nil
	}
	var many []string
	if err := value.Decode(&many); err != nil {
		return err
	}
	*n = many
	return nil
}
