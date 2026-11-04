package updex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
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
	data, err := os.ReadFile("../.github/risk-paths.json")
	if err != nil {
		t.Fatalf("read risk paths list: %v", err)
	}
	if err := checkDuplicateRiskPathJSONKeys(data); err != nil {
		t.Fatalf("strict JSON check failed: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
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
		if err := validateRiskPathGlob(entry.Glob); err != nil {
			t.Errorf("risk paths glob %q has invalid syntax: %v", entry.Glob, err)
		}
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
				matched, err := path.Match(globSegments[g], pathSegments[p])
				result = err == nil && matched && visit(g+1, p+1)
			}
		}
		memo[key] = result
		return result
	}
	return visit(0, 0)
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
		".github/workflows/test.yml",
		"policies/agent-governance.json",
		".github/policies/ai-governance.json",
		".claude/settings.json",
		".goreleaser.yaml",
		".svu.yml",
		"scripts/units.sh",
		"scripts/check-deb-units.sh",
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
