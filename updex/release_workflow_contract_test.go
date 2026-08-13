package updex

import (
	"os"
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
