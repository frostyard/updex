package updex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

var riskPathsCategories = []string{
	"boot chain",
	"signing and keys",
	"update verification",
	"auth",
	"publication",
	"secrets and credentials",
	"rulesets and settings",
	"destructive operations",
	"governance",
}

// TestRiskPathsCallerContract pins the caller against the workflow contract of
// the risk-paths and pr-triage specs (frostyard/core ADR-0068, ADR-0077,
// ADR-0078).
func TestRiskPathsCallerContract(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/risk-paths.yml")
	if err != nil {
		t.Fatalf("read risk paths workflow: %v", err)
	}
	for _, problem := range checkRiskPathsCaller(data) {
		t.Error(problem)
	}
}

// checkRiskPathsCaller gives 1 problem string for each way the workflow
// departs from the caller contract: the labeler stays in this workflow before
// the core job (updex#483) and it is not woken by an edit or a label change,
// the core job runs as pr-triage after the label job, and that job calls core's
// pr-triage workflow pinned to a full commit SHA.
func checkRiskPathsCaller(data []byte) []string {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	var workflow struct {
		On struct {
			PullRequestTarget struct {
				Types []string `yaml:"types"`
			} `yaml:"pull_request_target"`
		} `yaml:"on"`
		Permissions map[string]any `yaml:"permissions"`
		Jobs        map[string]struct {
			If          string         `yaml:"if"`
			Needs       any            `yaml:"needs"`
			Permissions map[string]any `yaml:"permissions"`
			Steps       []struct {
				Uses string         `yaml:"uses"`
				With map[string]any `yaml:"with"`
			} `yaml:"steps"`
			Secrets any `yaml:"secrets"`
			With    any `yaml:"with"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		return append(problems, fmt.Sprintf("parse risk paths workflow: %v", err))
	}

	var generic map[string]any
	if err := yaml.Unmarshal(data, &generic); err != nil {
		return append(problems, fmt.Sprintf("parse risk paths workflow keys: %v", err))
	}
	// One run per pull request at a time, never cancelled in progress: the
	// label job of a later run must not overlap the pr-triage job of an
	// earlier run (updex#483 race).
	wantGroup := "risk-paths-${{ github.event.pull_request.number }}"
	if concurrency, ok := generic["concurrency"].(map[string]any); !ok {
		problem("risk paths workflow has no concurrency map; want group %q with cancel-in-progress false", wantGroup)
	} else {
		if got := concurrency["group"]; got != wantGroup {
			problem("concurrency.group = %v, want %q", got, wantGroup)
		}
		if got, ok := concurrency["cancel-in-progress"].(bool); !ok || got {
			problem("concurrency.cancel-in-progress = %v, want boolean false", concurrency["cancel-in-progress"])
		}
	}
	onValue, ok := generic["on"]
	if !ok {
		problem("risk paths workflow has no on map")
	} else {
		onMap, ok := onValue.(map[string]any)
		if !ok {
			problem("risk paths workflow on = %T, want a map", onValue)
		} else if len(onMap) != 1 {
			problem("risk paths workflow on has %d keys, want exactly 1 (%v)", len(onMap), onMap)
		} else if _, ok := onMap["pull_request_target"]; !ok {
			problem("risk paths workflow on keys = %v, want only pull_request_target", onMap)
		}
	}

	// "labeled": the levels step of pr-triage skips a label event, but the
	// run must still see the new head and remove ready-for-human when needed.
	wantTypes := []string{"opened", "synchronize", "reopened", "edited", "labeled"}
	if !slices.Equal(workflow.On.PullRequestTarget.Types, wantTypes) {
		problem("on.pull_request_target.types = %v, want %v", workflow.On.PullRequestTarget.Types, wantTypes)
	}
	if workflow.Permissions == nil {
		problem("risk paths workflow is missing top-level permissions")
	} else if len(workflow.Permissions) != 0 {
		problem("top-level permissions = %v, want an empty map", workflow.Permissions)
	}
	if len(workflow.Jobs) != 2 {
		problem("risk paths workflow jobs = %v, want exactly two jobs: label and pr-triage", workflow.Jobs)
	}
	label, ok := workflow.Jobs["label"]
	if !ok {
		problem("risk paths workflow is missing the label job")
	}
	// No labeler run for an edit or a label change: a label change is made by
	// pr-triage itself, and the labeler would then set the whole label list.
	wantLabelIf := "github.event.action != 'edited' && github.event.action != 'labeled'"
	if label.If != wantLabelIf {
		problem("label job if = %q, want %q", label.If, wantLabelIf)
	}
	wantPermissions := map[string]any{"contents": "read", "pull-requests": "write"}
	if len(label.Permissions) != len(wantPermissions) {
		problem("label permissions = %v, want %v", label.Permissions, wantPermissions)
	} else {
		for key, want := range wantPermissions {
			if got, ok := label.Permissions[key]; !ok || got != want {
				problem("label permissions[%q] = %v, want %v", key, got, want)
			}
		}
	}
	if len(label.Steps) != 1 {
		problem("label job has %d steps, want exactly 1", len(label.Steps))
	} else {
		pinnedLabeler := regexp.MustCompile(`^actions/labeler@[0-9a-f]{40}$`)
		if !pinnedLabeler.MatchString(label.Steps[0].Uses) {
			problem("label step uses = %q, want actions/labeler pinned to a 40-character SHA", label.Steps[0].Uses)
		}
		if got, ok := label.Steps[0].With["sync-labels"]; !ok || got != false {
			problem("label step sync-labels = %v, want false", got)
		}
	}
	labelMap, ok := genericJobMap(generic, "label")
	if !ok {
		problem("risk paths workflow cannot find label job in generic YAML map")
	}
	if _, exists := labelMap["steps"]; !exists {
		problem("label job must contain a steps list")
	}
	for _, forbidden := range []string{"uses", "secrets"} {
		if _, exists := labelMap[forbidden]; exists {
			problem("label job must not contain %q", forbidden)
		}
	}

	job, ok := workflow.Jobs["pr-triage"]
	if !ok {
		problem("risk paths workflow is missing the pr-triage job; the job id of the caller must be pr-triage")
	}
	if needsLabel(job.Needs) == false {
		problem("pr-triage needs = %v, want label", job.Needs)
	}
	if job.If != "${{ !cancelled() }}" {
		problem("pr-triage if = %q, want %q", job.If, "${{ !cancelled() }}")
	}
	if len(job.Permissions) != len(wantPermissions) {
		problem("pr-triage permissions = %v, want %v", job.Permissions, wantPermissions)
	} else {
		for key, want := range wantPermissions {
			if got, ok := job.Permissions[key]; !ok || got != want {
				problem("pr-triage permissions[%q] = %v, want %v", key, got, want)
			}
		}
	}
	// The pin must sit in the pr-triage job itself: a uses: under another job,
	// or a line inside a block scalar, calls nothing.
	if uses, hasUses := jobUses(string(data), prTriageJob); !hasUses {
		problem("risk paths workflow has no %s job with a uses: key at indent 4", prTriageJob)
	} else if !pinnedRiskUses(uses) {
		problem("pr-triage uses = %q, want %s pinned to a 40-character SHA", uses, prTriageUses)
	}
	if len(job.Steps) != 0 {
		problem("pr-triage job steps = %v, want no steps", job.Steps)
	}
	jobMap, ok := genericJobMap(generic, "pr-triage")
	if !ok {
		problem("risk paths workflow cannot find pr-triage job in generic YAML map")
	}
	for _, forbidden := range []string{"steps", "secrets", "with"} {
		if _, exists := jobMap[forbidden]; exists {
			problem("pr-triage job must not contain %q", forbidden)
		}
	}
	return problems
}

// prTriageJob and prTriageUses name the job and the reusable workflow the
// caller must use: the pin sits in that job, at a full commit SHA (core
// ADR-0021, the risk-paths and PR triage specs).
const (
	prTriageJob  = "pr-triage"
	prTriageUses = "frostyard/core/.github/workflows/pr-triage.yml@"
)

// pinnedRiskUses holds when value is core's PR triage workflow at a full
// 40-character commit SHA.
func pinnedRiskUses(value string) bool {
	if !strings.HasPrefix(value, prTriageUses) {
		return false
	}
	ref := strings.TrimPrefix(value, prTriageUses)
	return len(ref) == 40 && strings.Trim(ref, "0123456789abcdef") == ""
}

// opensBlockScalar holds when a trimmed line starts a block scalar: the text
// after its last colon begins with "|" or ">" (with or without a chomping
// indicator, as in "run: |" or "env: >-").
func opensBlockScalar(trimmed string) bool {
	field := trimmed
	if i := strings.LastIndex(field, ":"); i >= 0 {
		field = strings.TrimSpace(field[i+1:])
	}
	return strings.HasPrefix(field, "|") || strings.HasPrefix(field, ">")
}

// stripComment drops a trailing YAML comment: a "#" starts a comment only at
// the start of the value or after a space or a tab, and a "#" inside a single-
// or double-quoted scalar is part of the value. Backslash escapes in a
// double-quoted scalar are not read; no caller value here needs them.
func stripComment(value string) string {
	quote := byte(0)
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#' && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t'):
			return strings.TrimSpace(value[:i])
		}
	}
	return strings.TrimSpace(value)
}

// unquoteScalar drops the quotes of 1 scalar that is wholly quoted.
func unquoteScalar(value string) string {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}
	return value
}

// jobUses gives the value of the uses: key at indent 4 in the job with id job,
// which is a key at indent 2 under the top-level jobs: map. The jobs: map ends
// at the next indent-0 key, so a job header or a uses: line inside another
// top-level block never counts. The value drops a trailing comment
// (quote-aware) and the quotes of a quoted scalar. A blank line or a
// comment-only line at any indent does not end the job block; only a
// non-comment line at indent 2 or less does. A line inside a block scalar
// (after a key that ends with "|" or ">") is text, not structure.
func jobUses(text, job string) (string, bool) {
	inJobs := false
	inJob := false
	blockIndent := -1
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if blockIndent >= 0 {
			if trimmed == "" || indent > blockIndent {
				continue
			}
			blockIndent = -1
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if opensBlockScalar(trimmed) {
			blockIndent = indent
		}
		if indent == 0 {
			inJobs = strings.HasPrefix(trimmed, "jobs:")
			inJob = false
			continue
		}
		if !inJobs {
			continue
		}
		if indent <= 2 {
			inJob = indent == 2 && strings.HasPrefix(trimmed, job+":")
			continue
		}
		if !inJob || blockIndent == indent {
			continue
		}
		if indent == 4 && strings.HasPrefix(trimmed, "uses:") {
			return unquoteScalar(stripComment(strings.TrimPrefix(trimmed, "uses:"))), true
		}
	}
	return "", false
}

func needsLabel(needs any) bool {
	if got, ok := needs.(string); ok {
		return got == "label"
	}
	if got, ok := needs.([]any); ok {
		return len(got) == 1 && got[0] == "label"
	}
	if got, ok := needs.([]string); ok {
		return len(got) == 1 && got[0] == "label"
	}
	return false
}

func TestNoOtherWorkflowSetsPullRequestLabels(t *testing.T) {
	files, err := filepath.Glob("../.github/workflows/*")
	if err != nil {
		t.Fatalf("glob workflows: %v", err)
	}
	for _, file := range files {
		if filepath.Base(file) == "risk-paths.yml" {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read workflow %s: %v", file, err)
		}
		if strings.Contains(string(data), "actions/labeler@") {
			t.Errorf("workflow %s also uses actions/labeler; label-setting jobs must be consolidated in risk-paths.yml", file)
		}
	}
}

func genericJobMap(workflow map[string]any, name string) (map[string]any, bool) {
	jobs, ok := workflow["jobs"].(map[string]any)
	if !ok {
		return nil, false
	}
	job, ok := jobs[name].(map[string]any)
	return job, ok
}

// riskPathLevels are the 4 levels of a version 2 list (frostyard/core
// ADR-0078), strictest first.
var riskPathLevels = []string{"L1", "L2", "L3", "L4"}

// riskPathsRequiredPaths are the 2 paths rule C of the risk-paths spec requires
// every list to cover. Their globs are L1 or L2 in a version 2 list.
var riskPathsRequiredPaths = []string{".github/risk-paths.json", ".github/workflows/risk-paths.yml"}

type riskPathEntry struct {
	Glob  string `json:"glob"`
	Level string `json:"level"`
	Why   string `json:"why"`
}

type riskPathsList struct {
	SchemaVersion int             `json:"schema_version"`
	DefaultLevel  string          `json:"default_level"`
	Paths         []riskPathEntry `json:"paths"`
}

func validRiskPathLevel(level string) bool {
	return slices.Contains(riskPathLevels, level)
}

// parseRiskPathsList reads a version 2 list with the validator's rules
// (docs/specs/risk-paths.md, sections "Interface" and "Rules A and C"):
// strict JSON with no duplicate key, no unknown key at any level, schema_version
// 2, a default_level and a level on every entry out of L1 to L4, and at least 1
// entry.
func parseRiskPathsList(data []byte) (*riskPathsList, error) {
	if err := checkDuplicateRiskPathJSONKeys(data); err != nil {
		return nil, fmt.Errorf("strict JSON check failed: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var list riskPathsList
	if err := decoder.Decode(&list); err != nil {
		return nil, fmt.Errorf("decode risk paths list: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("risk paths list has trailing JSON data: %v", err)
	}
	if list.SchemaVersion != 2 {
		return nil, fmt.Errorf("schema_version = %d, want 2", list.SchemaVersion)
	}
	if !validRiskPathLevel(list.DefaultLevel) {
		return nil, fmt.Errorf("default_level = %q, want one of %v", list.DefaultLevel, riskPathLevels)
	}
	if len(list.Paths) == 0 {
		return nil, fmt.Errorf("paths must not be empty")
	}
	for _, entry := range list.Paths {
		if !validRiskPathLevel(entry.Level) {
			return nil, fmt.Errorf("risk paths glob %q level = %q, want one of %v", entry.Glob, entry.Level, riskPathLevels)
		}
	}
	return &list, nil
}

// checkRiskPathsRequiredLevels is rule C of the risk-paths spec: the list
// covers each required path, and each glob that covers one of them is L1 or L2.
func checkRiskPathsRequiredLevels(list *riskPathsList, required []string) []string {
	var problems []string
	for _, requiredPath := range required {
		covered := false
		for _, entry := range list.Paths {
			if !matchRiskPathGlob(entry.Glob, requiredPath) {
				continue
			}
			covered = true
			if entry.Level != "L1" && entry.Level != "L2" {
				problems = append(problems, fmt.Sprintf("risk paths glob %q covers %s and has level %s, want L1 or L2", entry.Glob, requiredPath, entry.Level))
			}
		}
		if !covered {
			problems = append(problems, fmt.Sprintf("risk paths list must contain a glob that matches %s", requiredPath))
		}
	}
	return problems
}

func TestRiskPathsListContract(t *testing.T) {
	data, err := os.ReadFile("../.github/risk-paths.json")
	if err != nil {
		t.Fatalf("read risk paths list: %v", err)
	}
	list, err := parseRiskPathsList(data)
	if err != nil {
		t.Fatalf("risk paths list: %v", err)
	}

	ids := readProtectedBoundaryIDs(t)
	seen := map[string]bool{}
	for _, entry := range list.Paths {
		if err := validateRiskPathGlob(entry.Glob); err != nil {
			t.Errorf("risk paths glob %q has invalid syntax: %v", entry.Glob, err)
		}
		if entry.Glob == "" {
			t.Errorf("risk paths entry has an empty glob")
		} else if seen[entry.Glob] {
			t.Errorf("duplicate risk paths glob %q", entry.Glob)
		}
		seen[entry.Glob] = true

		why := strings.TrimSpace(entry.Why)
		if why == "" {
			t.Errorf("risk paths glob %q has an empty why", entry.Glob)
			continue
		}
		if len([]rune(why)) > 200 {
			t.Errorf("risk paths glob %q why is %d characters, want at most 200", entry.Glob, len([]rune(why)))
		}

		matchedID := ""
		for id := range ids {
			prefix := id + ": "
			if strings.HasPrefix(why, prefix) {
				matchedID = id
				break
			}
		}
		if matchedID == "" {
			t.Errorf("risk paths glob %q why %q does not start with a protected-boundary id followed by ': ' (known ids: %v)", entry.Glob, why, sortedMapKeys(ids))
			continue
		}
		text := strings.TrimPrefix(why, matchedID+": ")
		if !categoryPrefix(text) {
			t.Errorf("risk paths glob %q why %q does not start with one of the 9 spec categories after %q", entry.Glob, why, matchedID+":")
		}
	}
	for _, problem := range checkRiskPathsRequiredLevels(list, riskPathsRequiredPaths) {
		t.Error(problem)
	}
}

// TestRiskPathsListRejectsInvalidShape gives 1 failing case for each rule that
// a version 1 list and a hand-edited version 2 list can break.
func TestRiskPathsListRejectsInvalidShape(t *testing.T) {
	tests := []struct {
		name string
		list string
		want string
	}{
		{
			name: "version 1 list",
			list: `{"schema_version":1,"paths":[{"glob":".github/risk-paths.json","why":"governance: the list"}]}`,
			want: "schema_version = 1, want 2",
		},
		{
			name: "missing level",
			list: `{"schema_version":2,"default_level":"L3","paths":[{"glob":".github/risk-paths.json","why":"governance: the list"}]}`,
			want: "level = \"\", want one of",
		},
		{
			name: "level outside L1 to L4",
			list: `{"schema_version":2,"default_level":"L3","paths":[{"glob":".github/risk-paths.json","level":"L5","why":"governance: the list"}]}`,
			want: "level = \"L5\"",
		},
		{
			name: "bad default level",
			list: `{"schema_version":2,"default_level":"high","paths":[{"glob":".github/risk-paths.json","level":"L2","why":"governance: the list"}]}`,
			want: "default_level = \"high\"",
		},
		{
			name: "missing default level",
			list: `{"schema_version":2,"paths":[{"glob":".github/risk-paths.json","level":"L2","why":"governance: the list"}]}`,
			want: "default_level = \"\"",
		},
		{
			name: "unknown top-level key",
			list: `{"schema_version":2,"default_level":"L3","default":"L1","paths":[{"glob":".github/risk-paths.json","level":"L2","why":"governance: the list"}]}`,
			want: "unknown field",
		},
		{
			name: "unknown entry key",
			list: `{"schema_version":2,"default_level":"L3","paths":[{"glob":".github/risk-paths.json","level":"L2","risk":"high","why":"governance: the list"}]}`,
			want: "unknown field",
		},
		{
			name: "no entry",
			list: `{"schema_version":2,"default_level":"L3","paths":[]}`,
			want: "paths must not be empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseRiskPathsList([]byte(tt.list)); err == nil {
				t.Fatalf("risk paths list accepted, want an error naming %q", tt.want)
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to name %q", err, tt.want)
			}
		})
	}
}

func TestRiskPathsRequiredLevels(t *testing.T) {
	entry := func(glob, level string) riskPathEntry {
		return riskPathEntry{Glob: glob, Level: level, Why: "governance: the list"}
	}
	tests := []struct {
		name  string
		paths []riskPathEntry
		want  string
	}{
		{
			name:  "caller glob at L3",
			paths: []riskPathEntry{entry(".github/risk-paths.json", "L2"), entry(".github/workflows/risk-paths.yml", "L3")},
			want:  "covers .github/workflows/risk-paths.yml and has level L3, want L1 or L2",
		},
		{
			name:  "list glob at L3",
			paths: []riskPathEntry{entry(".github/risk-paths.json", "L3"), entry(".github/workflows/risk-paths.yml", "L2")},
			want:  "covers .github/risk-paths.json and has level L3, want L1 or L2",
		},
		{
			name:  "no glob covers the caller",
			paths: []riskPathEntry{entry(".github/risk-paths.json", "L2"), entry(".github/workflows/ci.yml", "L1")},
			want:  "must contain a glob that matches .github/workflows/risk-paths.yml",
		},
		{
			name:  "every glob covering workflows is strict",
			paths: []riskPathEntry{entry(".github/risk-paths.json", "L2"), entry(".github/workflows/risk-paths.yml", "L2"), entry(".github/workflows/**", "L1")},
			want:  "",
		},
		{
			// "?" is 1 character, so this glob matches the caller.
			name:  "caller glob with a wildcard at L3",
			paths: []riskPathEntry{entry(".github/risk-paths.json", "L2"), entry(".github/workflows/risk-paths.yml", "L2"), entry(".github/workflows/risk-paths.ym?", "L3")},
			want:  "covers .github/workflows/risk-paths.yml and has level L3, want L1 or L2",
		},
		{
			// "**/" matches 0 directories, so this glob matches the list.
			name:  "list glob behind a double star at L3",
			paths: []riskPathEntry{entry(".github/risk-paths.json", "L2"), entry(".github/workflows/risk-paths.yml", "L2"), entry("**/.github/risk-paths.json", "L3")},
			want:  "covers .github/risk-paths.json and has level L3, want L1 or L2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := checkRiskPathsRequiredLevels(&riskPathsList{SchemaVersion: 2, DefaultLevel: "L3", Paths: tt.paths}, riskPathsRequiredPaths)
			if tt.want == "" {
				if len(problems) != 0 {
					t.Fatalf("problems = %v, want none", problems)
				}
				return
			}
			if len(problems) == 0 {
				t.Fatalf("no problem found, want one naming %q", tt.want)
			}
			if !slices.ContainsFunc(problems, func(problem string) bool { return strings.Contains(problem, tt.want) }) {
				t.Errorf("problems = %v, want one naming %q", problems, tt.want)
			}
		})
	}
}

// TestRiskPathsCallerRejectsOldForm gives 1 failing case for each way a caller
// can keep the pre-triage form: pinning core's risk-paths workflow, dropping
// the labeled event, or naming the job something other than pr-triage.
func TestRiskPathsCallerRejectsOldForm(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/risk-paths.yml")
	if err != nil {
		t.Fatalf("read risk paths workflow: %v", err)
	}
	pinned := regexp.MustCompile(`frostyard/core/\.github/workflows/pr-triage\.yml@[0-9a-f]{40}`).Find(data)
	if pinned == nil {
		t.Fatal("the caller does not pin frostyard/core/.github/workflows/pr-triage.yml; the cases below need it")
	}
	tests := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{
			name: "pins the old risk-paths workflow",
			old:  string(pinned),
			new:  strings.Replace(string(pinned), "pr-triage.yml", "risk-paths.yml", 1),
			want: "pr-triage.yml@ pinned to a 40-character SHA",
		},
		{
			name: "no labeled event",
			old:  "types: [opened, synchronize, reopened, edited, labeled]",
			new:  "types: [opened, synchronize, reopened, edited]",
			want: "want [opened synchronize reopened edited labeled]",
		},
		{
			// The labeled type under the wrong trigger: pull_request: is not
			// pull_request_target:.
			name: "labeled only under pull_request",
			old:  "    types: [opened, synchronize, reopened, edited, labeled]\n",
			new:  "    types: [opened, synchronize, reopened, edited]\n  pull_request:\n    types: [labeled]\n",
			want: "want [opened synchronize reopened edited labeled]",
		},
		{
			// A "#" inside a quoted scalar is part of the value, not a comment:
			// the 5th type is "labeled # x", not labeled.
			name: "labeled inside a quoted scalar",
			old:  "    types: [opened, synchronize, reopened, edited, labeled]",
			new:  `    types: [opened, synchronize, reopened, edited, "labeled # x"]`,
			want: "want [opened synchronize reopened edited labeled]",
		},
		{
			name: "labeller woken by a label change",
			old:  "if: github.event.action != 'edited' && github.event.action != 'labeled'",
			new:  "if: github.event.action != 'edited'",
			want: "label job if",
		},
		{
			name: "core job not named pr-triage",
			old:  "\n  pr-triage:\n",
			new:  "\n  risk-paths:\n",
			want: "pr-triage",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(string(data), tt.old) {
				t.Fatalf("the caller does not contain %q, so this case cannot run", tt.old)
			}
			mutated := strings.Replace(string(data), tt.old, tt.new, 1)
			problems := checkRiskPathsCaller([]byte(mutated))
			if len(problems) == 0 {
				t.Fatalf("the caller accepted the old form, want a problem naming %q", tt.want)
			}
			if !slices.ContainsFunc(problems, func(problem string) bool { return strings.Contains(problem, tt.want) }) {
				t.Errorf("problems = %v, want one naming %q", problems, tt.want)
			}
		})
	}
}

// TestRiskPathsCallerBindsThePinToTheJob holds the pin to the pr-triage job
// itself. A gate that finds the pin anywhere in the file accepts a uses: under
// another job or a line inside a block scalar, and the run calls nothing.
func TestRiskPathsCallerBindsThePinToTheJob(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/risk-paths.yml")
	if err != nil {
		t.Fatalf("read risk paths workflow: %v", err)
	}
	text := string(data)
	pin := regexp.MustCompile(regexp.QuoteMeta(prTriageUses) + `[0-9a-f]{40}`).FindString(text)
	if pin == "" {
		t.Fatalf("the caller does not pin %s<40 hex>; the cases below need it", prTriageUses)
	}
	usesLine := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "    uses: ") && strings.Contains(line, pin) {
			usesLine = line
			break
		}
	}
	if usesLine == "" {
		t.Fatalf("the caller has no uses: line at indent 4 with the pin %q", pin)
	}
	tests := []struct {
		name   string
		mutate func(string) string
		want   string
	}{
		{
			name: "pin under another job",
			mutate: func(s string) string {
				return strings.Replace(s, usesLine+"\n", "", 1) +
					"\n  other:\n    permissions:\n      contents: read\n" + usesLine + "\n"
			},
			want: "has no pr-triage job with a uses: key",
		},
		{
			name: "pin inside a block scalar of the pr-triage job",
			mutate: func(s string) string {
				return strings.Replace(s, usesLine+"\n", "    steps:\n      - run: |\n          uses: "+pin+"\n", 1)
			},
			want: "has no pr-triage job with a uses: key",
		},
		{
			// Comments at indent 0 and 2 and a blank line are not structure: they
			// do not end the job block, so the pin still counts.
			name: "comments and a blank line before the uses line",
			mutate: func(s string) string {
				return strings.Replace(s, usesLine+"\n", "# a comment at indent 0\n\n  # a comment at indent 2\n"+usesLine+"\n", 1)
			},
			want: "",
		},
		{
			// The jobs: map ends at the next indent-0 key, and a block scalar is
			// text: a job header and a uses: line inside it are not a job.
			name: "job and pin inside a top-level block scalar",
			mutate: func(s string) string {
				s = strings.Replace(s, "\n  pr-triage:\n", "\n  other:\n", 1)
				return s + "\nrun-name: |\n  pr-triage:\n    uses: " + pin + "\n"
			},
			want: "has no pr-triage job with a uses: key",
		},
		{
			// Inline comments in 5 places, including a "[" and "]" in the types
			// comment: the list ends at the first "]" and every comment is
			// dropped, so the caller is valid.
			name: "inline comments in 5 places",
			mutate: func(s string) string {
				s = strings.Replace(s, "\non:\n", "\non: # events\n", 1)
				s = strings.Replace(s, "\n  pull_request_target:\n", "\n  pull_request_target: # base branch\n", 1)
				s = strings.Replace(s, "    types: [opened, synchronize, reopened, edited, labeled]\n",
					"    types: [opened, synchronize, reopened, edited, labeled] # readiness [L1]\n", 1)
				s = strings.Replace(s, "\n  pr-triage:\n", "\n  pr-triage: # core\n", 1)
				return strings.Replace(s, usesLine+"\n", "    uses: "+pin+" # pinned\n", 1)
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := checkRiskPathsCaller([]byte(tt.mutate(text)))
			if tt.want == "" {
				if len(problems) != 0 {
					t.Fatalf("problems = %v, want none", problems)
				}
				return
			}
			if !slices.ContainsFunc(problems, func(problem string) bool { return strings.Contains(problem, tt.want) }) {
				t.Errorf("problems = %v, want one naming %q", problems, tt.want)
			}
		})
	}
}

func readProtectedBoundaryIDs(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile("../policies/agent-governance.json")
	if err != nil {
		t.Fatalf("read agent governance policy: %v", err)
	}
	var policy struct {
		ProtectedBoundaries []struct {
			ID string `json:"id"`
		} `json:"protected_boundaries"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatalf("parse agent governance policy: %v", err)
	}
	ids := map[string]bool{}
	for _, boundary := range policy.ProtectedBoundaries {
		if boundary.ID != "" {
			ids[boundary.ID] = true
		}
	}
	return ids
}

func sortedMapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func categoryPrefix(text string) bool {
	for _, category := range riskPathsCategories {
		if strings.HasPrefix(text, category+",") || text == category {
			return true
		}
	}
	return false
}

func checkDuplicateRiskPathJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, isDelimiter := token.(json.Delim)
		if !isDelimiter {
			return nil
		}
		if delimiter == '{' {
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("JSON object key is not a string")
				}
				if seen[key] {
					return fmt.Errorf("duplicate JSON key %q", key)
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
			if _, err := decoder.Token(); err != nil {
				return err
			}
			return nil
		}
		if delimiter == '[' {
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			if _, err := decoder.Token(); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing data after the JSON document")
		}
		return fmt.Errorf("trailing data after the JSON document: %w", err)
	}
	return nil
}

func expandRiskPathGroups(glob string) ([]string, error) {
	parts := [][]string{}
	literal := ""
	for i := 0; i < len(glob); {
		if glob[i] == '}' {
			return nil, fmt.Errorf("unmatched '}'")
		}
		if glob[i] != '{' {
			literal += glob[i : i+1]
			i++
			continue
		}
		close := strings.IndexByte(glob[i+1:], '}')
		if close < 0 {
			return nil, fmt.Errorf("unmatched '{'")
		}
		close += i + 1
		inside := glob[i+1 : close]
		if strings.Contains(inside, "{") {
			return nil, fmt.Errorf("nested braces are not supported")
		}
		alternatives := strings.Split(inside, ",")
		if len(alternatives) < 2 {
			return nil, fmt.Errorf("a brace group needs at least 2 alternatives")
		}
		if slices.Contains(alternatives, "") {
			return nil, fmt.Errorf("a brace group has an empty alternative")
		}
		parts = append(parts, []string{literal})
		parts = append(parts, alternatives)
		literal = ""
		i = close + 1
	}
	parts = append(parts, []string{literal})

	expansions := []string{""}
	for _, alternatives := range parts {
		next := make([]string, 0, len(expansions)*len(alternatives))
		for _, prefix := range expansions {
			for _, alternative := range alternatives {
				next = append(next, prefix+alternative)
			}
			if len(next) > 256 {
				return nil, fmt.Errorf("brace expansion gives more than 256 globs")
			}
		}
		expansions = next
	}
	return expansions, nil
}

func validateRiskPathGlob(glob string) error {
	if glob == "" {
		return fmt.Errorf("glob is empty")
	}
	var errors []string
	for _, c := range []string{"[", "]", "!", "\\"} {
		if strings.Contains(glob, c) {
			errors = append(errors, fmt.Sprintf("%q is not supported", c))
		}
	}

	expansions, err := expandRiskPathGroups(glob)
	if err != nil {
		errors = append(errors, err.Error())
	} else {
		for _, expansion := range expansions {
			if strings.HasPrefix(expansion, "/") {
				errors = append(errors, "a glob must not start with '/'")
			}
			if strings.HasPrefix(expansion, "./") {
				errors = append(errors, "a glob must not start with './'")
			}
			for _, segment := range strings.Split(expansion, "/") {
				if segment == "" {
					errors = append(errors, "a glob must not have an empty segment")
				} else if segment == ".." {
					errors = append(errors, "a glob must not have a '..' segment")
				} else if strings.Contains(segment, "**") && segment != "**" {
					errors = append(errors, "'**' must be a whole segment")
				}
			}
		}
	}
	if len(errors) != 0 {
		return fmt.Errorf("%s", strings.Join(uniqueStrings(errors), "; "))
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func expandMatchGroups(glob string) []string {
	expansions, err := expandRiskPathGroups(glob)
	if err != nil {
		return nil
	}
	return expansions
}

func matchRiskPathGlob(glob, filePath string) bool {
	if glob == "" || filePath == "" {
		return false
	}
	for _, expansion := range expandMatchGroups(glob) {
		if matchRiskPathSegments(strings.Split(expansion, "/"), strings.Split(filePath, "/")) {
			return true
		}
	}
	return false
}

func matchRiskPathSegments(globSegments, pathSegments []string) bool {
	memo := map[[2]int]bool{}
	seen := map[[2]int]bool{}
	var visit func(int, int) bool
	visit = func(g, p int) bool {
		key := [2]int{g, p}
		if seen[key] {
			return memo[key]
		}
		seen[key] = true
		var result bool
		if g == len(globSegments) {
			result = p == len(pathSegments)
		} else if globSegments[g] == "**" {
			result = visit(g+1, p) || (p < len(pathSegments) && visit(g, p+1))
		} else {
			if p < len(pathSegments) {
				result = matchRiskPathSegment(globSegments[g], pathSegments[p]) && visit(g+1, p+1)
			}
		}
		memo[key] = result
		return result
	}
	return visit(0, 0)
}

// matchRiskPathSegment matches 1 path segment against 1 glob segment with the
// rules of core's segmentPattern: "?" is exactly 1 character, "*" is 0 or more
// characters, and every other character is literal. A character class or a
// backslash is literal, as core reads it, not syntax.
func matchRiskPathSegment(glob, segment string) bool {
	pattern, value := []rune(glob), []rune(segment)
	i, j := 0, 0
	star, mark := -1, 0
	for j < len(value) {
		switch {
		case i < len(pattern) && (pattern[i] == '?' || pattern[i] == value[j]):
			i++
			j++
		case i < len(pattern) && pattern[i] == '*':
			star = i
			mark = j
			i++
		case star >= 0:
			i = star + 1
			mark++
			j = mark
		default:
			return false
		}
	}
	for i < len(pattern) && pattern[i] == '*' {
		i++
	}
	return i == len(pattern)
}

func collectRepositoryFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir("..", func(filePath string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("..", filePath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel == ".git" || rel == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("collect repository files: %v", err)
	}
	return files
}

func TestRiskPathsGlobsNameExistingFiles(t *testing.T) {
	data, err := os.ReadFile("../.github/risk-paths.json")
	if err != nil {
		t.Fatalf("read risk paths list: %v", err)
	}
	var list struct {
		Paths []struct {
			Glob string `json:"glob"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("parse risk paths list: %v", err)
	}
	files := collectRepositoryFiles(t)

	for _, entry := range list.Paths {
		matched := false
		for _, file := range files {
			if matchRiskPathGlob(entry.Glob, file) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("risk paths glob %q matches no regular file in the repository", entry.Glob)
		}
	}
}

func TestRiskPathGlobValidation(t *testing.T) {
	tests := []struct {
		name  string
		glob  string
		valid bool
	}{
		{name: "github workflows", glob: ".github/workflows/**", valid: true},
		{name: "brace alternatives", glob: "updex/{a,b}.go", valid: true},
		{name: "double star prefix", glob: "**/x", valid: true},
		{name: "double star alone", glob: "**", valid: true},
		{name: "markdown", glob: "docs/*.md", valid: true},
		{name: "question mark", glob: "x?y", valid: true},
		{name: "three stars", glob: "manifest/***", valid: false},
		{name: "mixed double stars", glob: "a**", valid: false},
		{name: "leading slash", glob: "/a", valid: false},
		{name: "dot slash", glob: "./a", valid: false},
		{name: "empty segment", glob: "a//b", valid: false},
		{name: "dot dot segment", glob: "a/../b", valid: false},
		{name: "character class", glob: "a/[ab]", valid: false},
		{name: "nested braces", glob: "{a,{b}}", valid: false},
		{name: "empty brace alternative", glob: "{a,}", valid: false},
		{name: "negation", glob: "!a", valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRiskPathGlob(tt.glob)
			if tt.valid && err != nil {
				t.Errorf("validateRiskPathGlob(%q) = %v, want valid", tt.glob, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("validateRiskPathGlob(%q) = nil, want invalid", tt.glob)
			}
		})
	}
}

func TestRiskPathDuplicateJSONKeys(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{name: "duplicate nested key", json: `{"paths":[{"glob":"a","glob":"b","why":"governance"}]}`},
		{name: "duplicate top-level key", json: `{"schema_version":1,"schema_version":2,"paths":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := checkDuplicateRiskPathJSONKeys([]byte(tt.json)); err == nil {
				t.Errorf("duplicate JSON keys accepted in %s", tt.json)
			} else if !strings.Contains(err.Error(), "duplicate JSON key") {
				t.Errorf("duplicate JSON key error = %q, want clear duplicate-key message", err)
			}
		})
	}
}

func TestMatchRiskPathGlob(t *testing.T) {
	tests := []struct {
		name  string
		glob  string
		path  string
		match bool
	}{
		{name: "double star root", glob: "**/x", path: "x", match: true},
		{name: "double star one folder", glob: "**/x", path: "a/x", match: true},
		{name: "double star two folders", glob: "**/x", path: "a/b/x", match: true},
		{name: "double star does not match child", glob: "**/x", path: "a/x/y", match: false},
		{name: "star exact root", glob: "x*", path: "x", match: true},
		{name: "star xy root", glob: "x*", path: "xy", match: true},
		{name: "star no nested match", glob: "x*", path: "a/x", match: false},
		{name: "markdown file", glob: "docs/*.md", path: "docs/a.md", match: true},
		{name: "dot markdown file", glob: "docs/*.md", path: "docs/.md", match: true},
		{name: "markdown no nested", glob: "docs/*.md", path: "docs/a/b.md", match: false},
		{name: "shared nested", glob: "shared/**", path: "shared/a/b", match: true},
		{name: "shared no other root", glob: "shared/**", path: "other/a", match: false},
		{name: "brace first", glob: ".github/{risk-paths.json,settings.yml}", path: ".github/risk-paths.json", match: true},
		{name: "brace second", glob: ".github/{risk-paths.json,settings.yml}", path: ".github/settings.yml", match: true},
		{name: "brace no unrelated", glob: ".github/{risk-paths.json,settings.yml}", path: ".github/workflows/ci.yml", match: false},
		{name: "manifest nonexistent", glob: "manifest/does-not-exist-*.go", path: "manifest/does-not-exist-a.go", match: true},
		{name: "non-ASCII literal in a brace glob", glob: "café/{a,b}.md", path: "café/a.md", match: true},
		{name: "non-ASCII literal is not mangled", glob: "café/{a,b}.md", path: "cafÃ©/a.md", match: false},
		// Core's segmentPattern reads a backslash and a character class as
		// literal text, never as syntax.
		{name: "backslash is literal", glob: `a\b`, path: `a\b`, match: true},
		{name: "backslash does not escape", glob: `a\b`, path: "ab", match: false},
		{name: "character class is literal", glob: "[ab]", path: "[ab]", match: true},
		{name: "character class does not match its member", glob: "[ab]", path: "a", match: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchRiskPathGlob(tt.glob, tt.path)
			if got != tt.match {
				t.Errorf("matchRiskPathGlob(%q, %q) = %v, want %v", tt.glob, tt.path, got, tt.match)
			}
		})
	}
}

func TestMatchRiskPathGlobNoRepositoryFile(t *testing.T) {
	files := collectRepositoryFiles(t)
	glob := "manifest/does-not-exist-*.go"
	for _, file := range files {
		if matchRiskPathGlob(glob, file) {
			t.Fatalf("manifest/does-not-exist-*.go unexpectedly matched repository file %q", file)
		}
	}
}

// TestRiskPathsListCoversHighRiskFiles pins that the list still covers the
// files whose changes must get risk:high: removing or narrowing a glob that
// covers one of them fails here. Add a path when a new high-risk file appears.
func TestRiskPathsListCoversHighRiskFiles(t *testing.T) {
	data, err := os.ReadFile("../.github/risk-paths.json")
	if err != nil {
		t.Fatalf("read risk paths list: %v", err)
	}
	var list struct {
		Paths []struct {
			Glob string `json:"glob"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("parse risk paths list: %v", err)
	}
	required := []string{
		".github/risk-paths.json",
		".github/workflows/risk-paths.yml",
		".github/workflows/release.yml",
		".github/workflows/snapshot.yml",
		".github/workflows/ci.yml",
		"policies/agent-governance.json",
		".github/policies/ai-governance.json",
		".claude/settings.json",
		".goreleaser.yaml",
		".svu.yml",
		"scripts/units.sh",
		"scripts/check-deb-units.sh",
		"scripts/completions.sh",
		"scripts/manpages.sh",
		"Makefile",
		"mise.toml",
		"mise.lock",
		"download/download.go",
		"download/decompress.go",
		"manifest/gpg.go",
		"manifest/manifest.go",
		"internal/httpclient/httpclient.go",
		"updex/list.go",
		"config/transfer.go",
		"catalog/catalog.go",
		"catalog/repo.go",
		"catalog/cache.go",
		"imagecompat/erofs.go",
		"sysext/manager.go",
		"sysext/runner.go",
		"systemd/manager.go",
		"updex/install.go",
		"updex/imagegate.go",
		"updex/rollback.go",
		"updex/activation.go",
		"updex/features.go",
		"updex/catalog.go",
		"updex/daemon.go",
		"updex/fsguard.go",
	}
	for _, file := range required {
		if _, err := os.Lstat(filepath.Join("..", filepath.FromSlash(file))); err != nil {
			t.Errorf("required high-risk file %q does not exist; update this test and the list: %v", file, err)
			continue
		}
		covered := slices.ContainsFunc(list.Paths, func(entry struct {
			Glob string `json:"glob"`
		}) bool {
			return matchRiskPathGlob(entry.Glob, file)
		})
		if !covered {
			t.Errorf("no glob in .github/risk-paths.json matches the high-risk file %q", file)
		}
	}
}
