package updex

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestWorkflowsPinActionsAndLeastPrivilege pins the core ADR-0021 shape of
// every workflow under .github/workflows/ (bound to this repository by
// docs/org-adrs.md):
//   - every external `uses:` is a full 40-character commit SHA followed by a
//     `# <label>` comment naming the version it was pinned from;
//   - the same SHA carries the same label everywhere (a bump bot updates the
//     SHA and the comment together; a stale label misleads reviewers);
//   - every workflow declares top-level `permissions:` (`{}` or an explicit
//     minimal map);
//   - every actions/checkout step sets `persist-credentials: false` unless its
//     job is in pushingJobs — none today: release.yml and snapshot.yml publish
//     through the GitHub API with GITHUB_TOKEN and never `git push`.
//
// It reads the raw lines for the comment (YAML drops it) and the parsed
// document for structure. Not skipped under -short: it is the gate.
func TestWorkflowsPinActionsAndLeastPrivilege(t *testing.T) {
	// "<workflow file>/<job id>" of jobs that push to the repository and may
	// keep checkout credentials. Empty on purpose; add an entry only with the
	// job that needs it and say why.
	pushingJobs := map[string]bool{}

	files, err := filepath.Glob("../.github/workflows/*.yml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no workflows found under ../.github/workflows")
	}
	sort.Strings(files)

	usesLine := regexp.MustCompile(`^\s*-?\s*uses:\s*(\S+)(?:\s+#\s*(\S.*))?\s*$`)
	pinned := regexp.MustCompile(`^[^@]+@([0-9a-f]{40})$`)
	labels := map[string]map[string]string{} // sha -> label -> first file:line

	for _, file := range files {
		name := filepath.Base(file)
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		// Raw pass: SHA pins and their labels.
		for index, line := range strings.Split(string(data), "\n") {
			m := usesLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			ref, label := m[1], strings.TrimSpace(m[2])
			if strings.HasPrefix(ref, "./") {
				continue // local action, exempt per ADR-0021
			}
			where := name + ":" + strconv.Itoa(index+1)
			sha := pinned.FindStringSubmatch(ref)
			if sha == nil {
				t.Errorf("%s: uses %q is not pinned to a full 40-character commit SHA", where, ref)
				continue
			}
			if label == "" {
				t.Errorf("%s: uses %q has no trailing `# <version>` comment", where, ref)
				continue
			}
			if labels[sha[1]] == nil {
				labels[sha[1]] = map[string]string{}
			}
			if _, seen := labels[sha[1]][label]; !seen {
				labels[sha[1]][label] = where
			}
		}

		// Parsed pass: top-level permissions and checkout credentials.
		var workflow struct {
			Permissions any `yaml:"permissions"`
			Jobs        map[string]struct {
				Steps []struct {
					Uses string         `yaml:"uses"`
					With map[string]any `yaml:"with"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(data, &workflow); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if workflow.Permissions == nil {
			t.Errorf("%s: no top-level permissions: block (ADR-0021: start from permissions: {} or an explicit minimal grant)", name)
		}
		for jobID, job := range workflow.Jobs {
			for _, step := range job.Steps {
				if !strings.HasPrefix(step.Uses, "actions/checkout@") || pushingJobs[name+"/"+jobID] {
					continue
				}
				if v, ok := step.With["persist-credentials"]; !ok || v != false {
					t.Errorf("%s job %s: actions/checkout without `persist-credentials: false` (the job does not push)", name, jobID)
				}
			}
		}
	}

	for sha, byLabel := range labels {
		if len(byLabel) > 1 {
			var seen []string
			for label, where := range byLabel {
				seen = append(seen, label+" ("+where+")")
			}
			sort.Strings(seen)
			t.Errorf("commit %s is labelled inconsistently: %s", sha[:12], strings.Join(seen, ", "))
		}
	}
}
