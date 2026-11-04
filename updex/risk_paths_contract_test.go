package updex

import (
	"encoding/json"
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

func TestRiskPathsCallerContract(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/risk-paths.yml")
	if err != nil {
		t.Fatalf("read risk paths workflow: %v", err)
	}

	var workflow struct {
		On struct {
			PullRequestTarget struct {
				Types []string `yaml:"types"`
			} `yaml:"pull_request_target"`
		} `yaml:"on"`
		Permissions map[string]any `yaml:"permissions"`
		Jobs        map[string]struct {
			Permissions map[string]any `yaml:"permissions"`
			Uses        string         `yaml:"uses"`
			Steps       []any          `yaml:"steps"`
			Secrets     any            `yaml:"secrets"`
			With        any            `yaml:"with"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse risk paths workflow: %v", err)
	}

	var generic map[string]any
	if err := yaml.Unmarshal(data, &generic); err != nil {
		t.Fatalf("parse risk paths workflow keys: %v", err)
	}
	onValue, ok := generic["on"]
	if !ok {
		t.Errorf("risk paths workflow has no on map")
	} else {
		onMap, ok := onValue.(map[string]any)
		if !ok {
			t.Errorf("risk paths workflow on = %T, want a map", onValue)
		} else if len(onMap) != 1 {
			t.Errorf("risk paths workflow on has %d keys, want exactly 1 (%v)", len(onMap), onMap)
		} else if _, ok := onMap["pull_request_target"]; !ok {
			t.Errorf("risk paths workflow on keys = %v, want only pull_request_target", onMap)
		}
	}

	wantTypes := []string{"opened", "synchronize", "reopened", "edited"}
	if !slices.Equal(workflow.On.PullRequestTarget.Types, wantTypes) {
		t.Errorf("on.pull_request_target.types = %v, want %v", workflow.On.PullRequestTarget.Types, wantTypes)
	}
	if workflow.Permissions == nil {
		t.Errorf("risk paths workflow is missing top-level permissions")
	} else if len(workflow.Permissions) != 0 {
		t.Errorf("top-level permissions = %v, want an empty map", workflow.Permissions)
	}
	if len(workflow.Jobs) != 1 {
		t.Errorf("risk paths workflow jobs = %v, want exactly one job: risk-paths", workflow.Jobs)
	}
	job, ok := workflow.Jobs["risk-paths"]
	if !ok {
		t.Fatal("risk paths workflow is missing the risk-paths job")
	}
	wantPermissions := map[string]any{"contents": "read", "pull-requests": "write"}
	if len(job.Permissions) != len(wantPermissions) {
		t.Errorf("risk-paths permissions = %v, want %v", job.Permissions, wantPermissions)
	} else {
		for key, want := range wantPermissions {
			if got, ok := job.Permissions[key]; !ok || got != want {
				t.Errorf("risk-paths permissions[%q] = %v, want %v", key, got, want)
			}
		}
	}
	pinned := regexp.MustCompile(`^frostyard/core/\.github/workflows/risk-paths\.yml@[0-9a-f]{40}$`)
	if !pinned.MatchString(job.Uses) {
		t.Errorf("risk-paths uses = %q, want frostyard/core reusable workflow pinned to a 40-character SHA", job.Uses)
	}
	if len(job.Steps) != 0 {
		t.Errorf("risk-paths job steps = %v, want no steps", job.Steps)
	}
	jobMap, ok := genericJobMap(generic, "risk-paths")
	if !ok {
		t.Fatal("risk paths workflow cannot find risk-paths job in generic YAML map")
	}
	for _, forbidden := range []string{"steps", "secrets", "with"} {
		if _, exists := jobMap[forbidden]; exists {
			t.Errorf("risk-paths job must not contain %q", forbidden)
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

func TestRiskPathsListContract(t *testing.T) {
	var list struct {
		SchemaVersion int `json:"schema_version"`
		Paths         []struct {
			Glob string `json:"glob"`
			Why  string `json:"why"`
		} `json:"paths"`
	}
	file, err := os.Open("../.github/risk-paths.json")
	if err != nil {
		t.Fatalf("open risk paths list: %v", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&list); err != nil {
		t.Fatalf("decode risk paths list: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Errorf("risk paths list has trailing JSON data: %v", err)
	}
	if list.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", list.SchemaVersion)
	}
	if len(list.Paths) == 0 {
		t.Errorf("paths must not be empty")
	}

	ids := readProtectedBoundaryIDs(t)
	seen := map[string]bool{}
	hasSelfList := false
	hasCaller := false
	for _, entry := range list.Paths {
		if entry.Glob == "" {
			t.Errorf("risk paths entry has an empty glob")
		} else if seen[entry.Glob] {
			t.Errorf("duplicate risk paths glob %q", entry.Glob)
		}
		seen[entry.Glob] = true
		if entry.Glob == ".github/risk-paths.json" {
			hasSelfList = true
		}
		if entry.Glob == ".github/workflows/risk-paths.yml" {
			hasCaller = true
		}

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
	if !hasSelfList {
		t.Errorf("risk paths list must contain glob .github/risk-paths.json")
	}
	if !hasCaller {
		t.Errorf("risk paths list must contain glob .github/workflows/risk-paths.yml")
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

	for _, entry := range list.Paths {
		for _, glob := range expandRiskPathGroups(entry.Glob) {
			if glob == "" {
				t.Errorf("risk paths glob %q expanded to an empty glob", entry.Glob)
				continue
			}
			checkRiskPathGlob(t, entry.Glob, glob)
		}
	}
}

func expandRiskPathGroups(glob string) []string {
	open := strings.Index(glob, "{")
	if open < 0 {
		return []string{glob}
	}
	end := strings.Index(glob[open+1:], "}")
	if end < 0 {
		return []string{glob}
	}
	end += open + 1
	choices := strings.Split(glob[open+1:end], ",")
	prefix, suffix := glob[:open], glob[end+1:]
	var expanded []string
	for _, choice := range choices {
		expanded = append(expanded, expandRiskPathGroups(prefix+choice+suffix)...)
	}
	return expanded
}

func checkRiskPathGlob(t *testing.T, original, glob string) {
	t.Helper()
	path := filepath.Join("..", filepath.FromSlash(glob))
	hasWildcard := strings.ContainsAny(glob, "*?")
	if !hasWildcard {
		info, err := os.Lstat(path)
		if err != nil {
			t.Errorf("risk paths glob %q (expanded %q) names missing file %q: %v", original, glob, glob, err)
			return
		}
		if !info.Mode().IsRegular() {
			t.Errorf("risk paths glob %q (expanded %q) names %q, which is not a regular file", original, glob, glob)
		}
		return
	}

	segments := strings.Split(glob, "/")
	prefix := []string{}
	for _, segment := range segments {
		if strings.ContainsAny(segment, "*?") {
			break
		}
		prefix = append(prefix, segment)
	}
	if len(prefix) == 0 {
		t.Errorf("risk paths glob %q (expanded %q) has a wildcard before a usable base directory", original, glob)
		return
	}
	base := filepath.Join("..", filepath.Join(prefix...))
	info, err := os.Stat(base)
	if err != nil {
		t.Errorf("risk paths glob %q (expanded %q) base directory %q is missing: %v", original, glob, base, err)
		return
	}
	if !info.IsDir() {
		t.Errorf("risk paths glob %q (expanded %q) base %q exists but is not a directory", original, glob, base)
		return
	}
	found := false
	err = filepath.WalkDir(base, func(_ string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			info, infoErr := entry.Info()
			if infoErr == nil && info.Mode().IsRegular() {
				found = true
			}
		}
		return nil
	})
	if err != nil {
		t.Errorf("risk paths glob %q (expanded %q) cannot inspect base directory %q: %v", original, glob, base, err)
		return
	}
	if !found {
		t.Errorf("risk paths glob %q (expanded %q) base directory %q contains no regular file at any depth", original, glob, base)
	}
}
