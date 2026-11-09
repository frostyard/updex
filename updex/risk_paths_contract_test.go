package updex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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

// riskCallerUses is the 1 body line of the caller that may vary: the core pin,
// with 40 lowercase hex characters and an optional trailing " # comment".
const riskCallerUses = "    uses: " + prTriageUses + "<SHA>[ # comment]"

// riskCallerLabeler is the labeler pin: Dependabot bumps it, so any full
// 40-character SHA with an optional trailing " # comment" passes (ADR-0021).
const riskCallerLabeler = "        uses: actions/labeler@<SHA>[ # comment]"

// labelerPinned holds when line pins actions/labeler to a full 40-character
// lowercase commit SHA, with an optional " # comment" after it.
func labelerPinned(line string) bool {
	rest, ok := strings.CutPrefix(line, "        uses: actions/labeler@")
	if !ok {
		return false
	}
	sha, _, _ := strings.Cut(rest, " #")
	return len(sha) == 40 && strings.Trim(sha, "0123456789abcdef") == ""
}

// riskCallerBody is the caller after its leading comment block: every line must
// equal this body exactly (core ADR-0068, ADR-0077, ADR-0078; the risk-paths
// and PR triage specs). The labeler stays in this workflow before the core job
// with its concurrency group (updex#483). An exact text check cannot be fooled
// by a comment that holds a key, by a quoted scalar or by a second types: list.
var riskCallerBody = []string{
	"name: PR triage",
	"",
	"on:",
	"  pull_request_target:",
	"    types: [opened, synchronize, reopened, edited, labeled]",
	"",
	"permissions: {}",
	"",
	"concurrency:",
	"  group: risk-paths-${{ github.event.pull_request.number }}",
	"  cancel-in-progress: false",
	"",
	"jobs:",
	"  label:",
	"    name: Apply path labels",
	"    if: github.event.action != 'edited' && github.event.action != 'labeled'",
	"    runs-on: ubuntu-latest",
	"    timeout-minutes: 5",
	"    permissions:",
	"      contents: read",
	"      pull-requests: write",
	"    steps:",
	"      # pull_request_target can label fork PRs. Never check out or execute",
	"      # pull-request-controlled code in this privileged workflow.",
	"      - name: Apply labels",
	riskCallerLabeler,
	"        with:",
	"          sync-labels: false",
	"",
	"  pr-triage:",
	"    needs: label",
	"    if: ${{ !cancelled() }}",
	"    permissions:",
	"      contents: read",
	"      pull-requests: write",
	riskCallerUses,
}

// checkRiskPathsCaller compares the caller with its canonical body and reports
// the first difference.
func checkRiskPathsCaller(data []byte) []string {
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	start := 0
	for start < len(lines) && (strings.TrimSpace(lines[start]) == "" || strings.HasPrefix(strings.TrimSpace(lines[start]), "#")) {
		start++
	}
	body := lines[start:]
	for i, want := range riskCallerBody {
		if i >= len(body) {
			return []string{fmt.Sprintf("caller ends at line %d, want line %d: %q", len(body), i+1, want)}
		}
		if want == riskCallerLabeler {
			if !labelerPinned(body[i]) {
				return []string{fmt.Sprintf("caller line %d is %q, want actions/labeler pinned to a 40-character commit SHA", i+1, body[i])}
			}
			continue
		}
		if want == riskCallerUses {
			if problem := checkRiskCallerUses(body[i], i+1); problem != "" {
				return []string{problem}
			}
			continue
		}
		if body[i] != want {
			return []string{fmt.Sprintf("caller line %d is %q, want %q", i+1, body[i], want)}
		}
	}
	if len(body) > len(riskCallerBody) {
		return []string{fmt.Sprintf("caller has an extra line %d: %q", len(riskCallerBody)+1, body[len(riskCallerBody)])}
	}
	return nil
}

// checkRiskCallerUses holds when line calls core's PR triage workflow at a full
// 40-character lowercase commit SHA, with an optional " # comment" after it.
func checkRiskCallerUses(line string, number int) string {
	rest, ok := strings.CutPrefix(line, "    uses: "+prTriageUses)
	if !ok {
		return fmt.Sprintf("caller line %d is %q, want %q", number, line, riskCallerUses)
	}
	sha, _, _ := strings.Cut(rest, " #")
	if len(sha) != 40 || strings.Trim(sha, "0123456789abcdef") != "" {
		return fmt.Sprintf("caller line %d holds the SHA %q, want 40 lowercase hex characters", number, sha)
	}
	return ""
}

// riskCaller is the caller workflow file: its name stays risk-paths.yml.
const riskCaller = ".github/workflows/risk-paths.yml"

// prTriageJob and prTriageUses name the job and the reusable workflow the
// caller must use: the pin sits in that job, at a full commit SHA (core
// ADR-0021, the risk-paths and PR triage specs).
const (
	prTriageJob  = "pr-triage"
	prTriageUses = "frostyard/core/.github/workflows/pr-triage.yml@"
)

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

func TestCheckRiskPathsCaller(t *testing.T) {
	data, err := os.ReadFile("../" + riskCaller)
	if err != nil {
		t.Fatalf("read %s: %v", riskCaller, err)
	}
	text := string(data)
	usesPrefix := "    uses: " + prTriageUses
	usesLine := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, usesPrefix) && len(strings.Split(line, "@")) > 1 {
			if strings.Contains(line, "actions/labeler@") {
				continue
			}
			usesLine = line
			break
		}
	}
	if usesLine == "" {
		t.Fatalf("%s has no uses: line for the core pin", riskCaller)
	}
	sha, _, _ := strings.Cut(strings.TrimPrefix(usesLine, usesPrefix), " #")
	if len(sha) != 40 {
		t.Fatalf("%s pins %q, want a 40-character SHA", riskCaller, sha)
	}
	// The labeler line is read from the file: Dependabot bumps its SHA, and a
	// fixed SHA here would break the test on every bump.
	labelerLine := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "        uses: actions/labeler@") {
			labelerLine = line
			break
		}
	}
	if labelerLine == "" {
		t.Fatalf("%s has no actions/labeler line", riskCaller)
	}
	types := "    types: [opened, synchronize, reopened, edited, labeled]"
	tests := []struct {
		name string
		text string
		want string
	}{
		{"the committed caller", text, ""},
		{"another SHA and another trailing comment",
			strings.Replace(text, usesLine, usesPrefix+strings.Repeat("a", 40)+" # pinned", 1), ""},
		{"another labeler SHA (a Dependabot bump)",
			strings.Replace(text, labelerLine, "        uses: actions/labeler@"+strings.Repeat("b", 40)+" # v7.1", 1), ""},
		{"labeler on a tag",
			strings.Replace(text, labelerLine, "        uses: actions/labeler@v7", 1), "actions/labeler pinned"},
		{"pin under another job name",
			strings.Replace(text, "\n  "+prTriageJob+":\n", "\n  other:\n", 1), `want "  ` + prTriageJob + `:"`},
		{"caller that pins risk-paths.yml",
			strings.Replace(text, prTriageUses, "frostyard/core/.github/workflows/risk-paths.yml@", 1), prTriageUses + "<SHA>"},
		{"short SHA", strings.Replace(text, usesPrefix+sha, usesPrefix+sha[:7], 1), "40 lowercase hex"},
		{"no labeled type", strings.Replace(text, types, "    types: [opened, synchronize, reopened, edited]", 1), types},
		{"commented-out pull-requests scope",
			strings.Replace(text, "      pull-requests: write", "      # pull-requests: write", 1), "pull-requests: write"},
		{"an extra permission",
			strings.Replace(text, "      pull-requests: write\n", "      pull-requests: write\n      actions: read\n", 1), "actions: read"},
		{"a scalar job header with a comment",
			strings.Replace(text, "\n  "+prTriageJob+":\n", "\n  "+prTriageJob+": | # note: text\n", 1), prTriageJob + ": | # note: text"},
		{"the types list inside a comment",
			strings.Replace(text, types, "    types: # [opened, synchronize, reopened, edited, labeled]", 1), "types: #"},
		{"an added merge_group trigger",
			strings.Replace(text, "\n  "+prTriageJob+":\n", "\n  merge_group:\n  "+prTriageJob+":\n", 1), "merge_group"},
		{"an extra line inside the job",
			strings.Replace(text, "      contents: read\n", "      contents: read\n    timeout-minutes: 5\n", 1), "timeout-minutes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(checkRiskPathsCaller([]byte(tt.text)), "; ")
			if tt.want == "" {
				if got != "" {
					t.Fatalf("checkRiskPathsCaller(%s) = %q, want no finding", tt.name, got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("checkRiskPathsCaller(%s) = %q, want it to name %q", tt.name, got, tt.want)
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
// characters, and every other character is literal. "*" and "?" are recognized
// before literal equality, so "*a" matches "*ba" and "*" matches "**", as in
// core. A character class or a backslash is literal, as core reads it, not
// syntax.
func matchRiskPathSegment(glob, segment string) bool {
	pattern, value := []rune(glob), []rune(segment)
	i, j := 0, 0
	star, mark := -1, 0
	for j < len(value) {
		switch {
		case i < len(pattern) && pattern[i] == '*':
			star = i
			mark = j
			i++
		case i < len(pattern) && pattern[i] == '?':
			i++
			j++
		case i < len(pattern) && pattern[i] == value[j]:
			i++
			j++
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
		// "*" and "?" come before literal equality, as in core's segmentPattern.
		{name: "star before literal equality", glob: "*a", path: "*ba", match: true},
		{name: "star matches a star", glob: "*", path: "**", match: true},
		{name: "2 stars in 1 segment match 2 stars", glob: "a*b", path: "a**b", match: true},
		{name: "question mark matches a star", glob: "?", path: "*", match: true},
		{name: "question mark matches itself", glob: "?", path: "?", match: true},
		{name: "question mark matches a star in the middle", glob: "a?c", path: "a*c", match: true},
		{name: "star file pattern matches a star file", glob: "*.go", path: "*.go", match: true},
		{name: "star then question mark", glob: "*?", path: "**", match: true},
		{name: "trailing star does not match a leading star", glob: "*b", path: "b*", match: false},
		{name: "literal question marks match", glob: "a?c", path: "a?c", match: true},
		{name: "star segment matches a star segment", glob: "a/*/c", path: "a/*/c", match: true},
		{name: "double star then star", glob: "**/*a", path: "x/*ba", match: true},
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
