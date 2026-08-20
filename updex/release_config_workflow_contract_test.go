package updex

import (
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type goreleaserWorkflow struct {
	On          map[string]any `yaml:"on"`
	Permissions any            `yaml:"permissions"`
	Jobs        map[string]struct {
		Permissions map[string]string `yaml:"permissions"`
		Env         map[string]string `yaml:"env"`
		Steps       []struct {
			Name string            `yaml:"name"`
			If   string            `yaml:"if"`
			Uses string            `yaml:"uses"`
			With map[string]string `yaml:"with"`
			Run  string            `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func readGoReleaserWorkflow(t *testing.T, path string) goreleaserWorkflow {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var workflow goreleaserWorkflow
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return workflow
}

func TestReleaseConfigWorkflowValidatesBeforeMerge(t *testing.T) {
	workflow := readGoReleaserWorkflow(t, "../.github/workflows/test.yml")
	for _, event := range []string{"push", "pull_request", "merge_group"} {
		if _, ok := workflow.On[event]; !ok {
			t.Errorf("Tests workflow is missing %s trigger", event)
		}
	}

	job, ok := workflow.Jobs["release-config"]
	if !ok {
		t.Fatal("Tests workflow is missing release-config job")
	}
	if got := job.Permissions["contents"]; got != "read" {
		t.Errorf("release-config permissions.contents = %q, want read", got)
	}
	if got := job.Env["GORELEASER_KEY"]; got != "${{ secrets.GORELEASER_KEY }}" {
		t.Errorf("release-config GORELEASER_KEY = %q, want repository secret", got)
	}

	var checkout, validate, trustedMissingKey, forkWithoutKey bool
	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, "actions/checkout@") {
			checkout = true
			if got := step.With["persist-credentials"]; got != "false" {
				t.Errorf("release-config checkout persist-credentials = %q, want false", got)
			}
		}
		if strings.HasPrefix(step.Uses, "goreleaser/goreleaser-action@") {
			validate = true
			if step.If != "env.GORELEASER_KEY != ''" {
				t.Errorf("GoReleaser validation guard = %q, want non-empty key guard", step.If)
			}
			if step.With["distribution"] != "goreleaser-pro" ||
				step.With["version"] != "~> v2" ||
				step.With["args"] != "check" {
				t.Errorf("GoReleaser validation inputs = %v, want goreleaser-pro v2 args check", step.With)
			}
		}
		if strings.Contains(step.If, "github.event.pull_request.head.repo.full_name == github.repository") &&
			strings.Contains(step.Run, "not validated") && strings.Contains(step.Run, "exit 1") {
			trustedMissingKey = true
		}
		if strings.Contains(step.If, "github.event.pull_request.head.repo.full_name != github.repository") &&
			strings.Contains(step.Run, "not validated") && strings.Contains(step.Run, "GITHUB_STEP_SUMMARY") {
			forkWithoutKey = true
		}
	}
	if !checkout {
		t.Error("release-config job must check out the repository")
	}
	if !validate {
		t.Error("release-config job must run goreleaser/goreleaser-action")
	}
	if !trustedMissingKey {
		t.Error("release-config job must fail trusted runs when GORELEASER_KEY is unavailable")
	}
	if !forkWithoutKey {
		t.Error("release-config job must explicitly report that fork PRs were not validated")
	}
}

func TestReleaseConfigWorkflowMatchesReleaseAction(t *testing.T) {
	testWorkflow := readGoReleaserWorkflow(t, "../.github/workflows/test.yml")
	releaseWorkflow := readGoReleaserWorkflow(t, "../.github/workflows/release.yml")
	snapshotWorkflow := readGoReleaserWorkflow(t, "../.github/workflows/snapshot.yml")

	findAction := func(t *testing.T, workflow goreleaserWorkflow) (string, map[string]string) {
		t.Helper()
		for _, job := range workflow.Jobs {
			for _, step := range job.Steps {
				if strings.HasPrefix(step.Uses, "goreleaser/goreleaser-action@") {
					return step.Uses, step.With
				}
			}
		}
		t.Fatal("workflow is missing goreleaser/goreleaser-action")
		return "", nil
	}

	testUses, testWith := findAction(t, testWorkflow)
	for name, workflow := range map[string]goreleaserWorkflow{
		"release":  releaseWorkflow,
		"snapshot": snapshotWorkflow,
	} {
		uses, with := findAction(t, workflow)
		if uses != testUses {
			t.Errorf("%s GoReleaser action = %q, want %q", name, uses, testUses)
		}
		for _, input := range []string{"distribution", "version"} {
			if with[input] != testWith[input] {
				t.Errorf("%s GoReleaser %s = %q, want %q", name, input, with[input], testWith[input])
			}
		}
	}
}
