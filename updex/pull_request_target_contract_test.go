package updex

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// pullRequestTargetAllowed lists the only workflow that may run on
// pull_request_target: the frostyard Actions policy allows the event only for
// the PR triage caller (frostyard/core
// docs/design/pull-request-target-policy.md).
var pullRequestTargetAllowed = []string{"risk-paths.yml"}

var topLevelEvent = regexp.MustCompile(`^  ([A-Za-z_-]+):`)

// topLevelEvents returns the event names of the block-style top-level on: key.
func topLevelEvents(text string) []string {
	lines := strings.Split(text, "\n")
	start := slices.Index(lines, "on:")
	if start < 0 {
		return nil
	}
	var events []string
	for _, line := range lines[start+1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			break
		}
		if match := topLevelEvent.FindStringSubmatch(line); match != nil {
			events = append(events, match[1])
		}
	}
	return events
}

func TestOnlyTheCallerRunsOnPullRequestTarget(t *testing.T) {
	files, err := filepath.Glob("../.github/workflows/*.yml")
	if err != nil {
		t.Fatalf("list workflows: %v", err)
	}
	yamlFiles, err := filepath.Glob("../.github/workflows/*.yaml")
	if err != nil {
		t.Fatalf("list workflows: %v", err)
	}
	files = append(files, yamlFiles...)
	if len(files) < 5 {
		t.Fatalf("found %d workflows, want at least 5", len(files))
	}
	var callers []string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(file), err)
		}
		events := topLevelEvents(string(data))
		if len(events) == 0 {
			t.Errorf("%s: no event found under on:", filepath.Base(file))
		}
		if slices.Contains(events, "pull_request_target") {
			callers = append(callers, filepath.Base(file))
		}
	}
	if !slices.Equal(callers, pullRequestTargetAllowed) {
		t.Errorf("workflows on pull_request_target = %v, want %v", callers, pullRequestTargetAllowed)
	}
}

func TestCopilotReviewRunsOnPullRequest(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/claude-code-review.yml")
	if err != nil {
		t.Fatalf("read review workflow: %v", err)
	}
	text := string(data)
	if events := topLevelEvents(text); !slices.Equal(events, []string{"pull_request"}) {
		t.Errorf("review workflow events = %v, want [pull_request]", events)
	}
	if !strings.Contains(text, "github.event.pull_request.head.repo.full_name == github.repository") {
		t.Error("review workflow does not skip fork pull requests")
	}
}

func TestTopLevelEventsFindsPullRequestTarget(t *testing.T) {
	text := "name: x\n\non:\n  pull_request_target:\n    types: [labeled]\n  push:\n\njobs:\n  x:\n"
	if got, want := topLevelEvents(text), []string{"pull_request_target", "push"}; !slices.Equal(got, want) {
		t.Errorf("topLevelEvents = %v, want %v", got, want)
	}
}
