package updex

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"go.yaml.in/yaml/v3"
)

// pullRequestTargetAllowed names the only workflow file that may run on
// pull_request_target: the PR triage caller. GitHub blocks that event by
// default in a public repository from 2026-12-02, and the organization's
// Actions policy allows it for the caller path only
// (frostyard/core docs/design/pull-request-target-policy.md). Any other
// workflow on that event makes a blocked run with no job and no check run.
var pullRequestTargetAllowed = map[string]bool{"risk-paths.yml": true}

// resolveAlias follows a YAML alias (`*name`) to the node its anchor names.
func resolveAlias(node *yaml.Node) *yaml.Node {
	for node != nil && node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	return node
}

// mappingKeys returns the resolved keys of a mapping, with the keys that a
// merge key (`<<: *anchor` or `<<: [*a, *b]`) brings in.
func mappingKeys(t *testing.T, mapping *yaml.Node) []*yaml.Node {
	t.Helper()
	var keys []*yaml.Node
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := resolveAlias(mapping.Content[i])
		if key.Kind == yaml.ScalarNode && key.Tag == "!!merge" {
			value := resolveAlias(mapping.Content[i+1])
			sources := []*yaml.Node{value}
			if value.Kind == yaml.SequenceNode {
				sources = value.Content
			}
			for _, source := range sources {
				source = resolveAlias(source)
				if source.Kind != yaml.MappingNode {
					t.Fatalf("merge key holds a YAML kind %d, not a mapping", source.Kind)
				}
				keys = append(keys, mappingKeys(t, source)...)
			}
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

// mappingValue returns the resolved value of the key name, or nil.
func mappingValue(t *testing.T, mapping *yaml.Node, name string) []*yaml.Node {
	t.Helper()
	var values []*yaml.Node
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := resolveAlias(mapping.Content[i])
		value := resolveAlias(mapping.Content[i+1])
		if key.Kind == yaml.ScalarNode && key.Tag == "!!merge" {
			sources := []*yaml.Node{value}
			if value.Kind == yaml.SequenceNode {
				sources = value.Content
			}
			for _, source := range sources {
				if source = resolveAlias(source); source.Kind == yaml.MappingNode {
					values = append(values, mappingValue(t, source, name)...)
				}
			}
			continue
		}
		if key.Value == name {
			values = append(values, value)
		}
	}
	return values
}

// workflowEvents returns the event names of a workflow's top-level `on:` key,
// in the 3 shapes GitHub accepts: a scalar, a sequence and a mapping. It reads
// the parsed document, so a comment or a block scalar that holds the word is
// not an event. It resolves aliases and merge keys at each level, so an
// anchor cannot hide an event name.
func workflowEvents(t *testing.T, data []byte) []string {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse workflow: %v", err)
	}
	if len(doc.Content) == 0 {
		return nil
	}
	root := resolveAlias(doc.Content[0])
	if root.Kind != yaml.MappingNode {
		return nil
	}
	var events []string
	for _, on := range mappingValue(t, root, "on") {
		switch on.Kind {
		case yaml.ScalarNode:
			events = append(events, on.Value)
		case yaml.SequenceNode:
			for _, item := range on.Content {
				events = append(events, resolveAlias(item).Value)
			}
		case yaml.MappingNode:
			for _, key := range mappingKeys(t, on) {
				events = append(events, key.Value)
			}
		default:
			t.Fatalf("workflow `on:` has an unexpected YAML kind %d", on.Kind)
		}
	}
	return events
}

// TestWorkflowEventsShapes is the negative control of the guard below: each
// shape of `on:` that names pull_request_target is found, and a comment or a
// run script that holds the word is not.
func TestWorkflowEventsShapes(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want bool
	}{
		{"scalar", "on: pull_request_target\n", true},
		{"flow sequence", "on: [push, pull_request_target]\n", true},
		{"block sequence", "on:\n  - push\n  - pull_request_target\n", true},
		{"mapping", "on:\n  pull_request_target:\n    types: [labeled]\n", true},
		{"quoted key", "\"on\":\n  'pull_request_target': {}\n", true},
		{"alias in a sequence", "name: &event pull_request_target\non: [*event]\n", true},
		{"alias as a mapping key", "name: &event pull_request_target\non:\n  *event : {}\n", true},
		{"alias as the on value", "x: &events\n  pull_request_target: {}\non: *events\n", true},
		{"alias as the on key", "name: &key on\n*key : pull_request_target\n", true},
		{"merge key", "x: &base\n  pull_request_target: {}\non:\n  <<: *base\n  push: {}\n", true},
		{"merge key list", "x: &base\n  pull_request_target: {}\ny: &other\n  push: {}\non:\n  <<: [*other, *base]\n", true},
		{"hash in a quoted value", "on: {push: {branches: [\"release#1\"]}, pull_request_target: {}}\n", true},
		{"comment only", "# pull_request_target is not used\non:\n  pull_request:\n", false},
		{"run script", "on: push\njobs:\n  a:\n    steps:\n      - run: |\n          echo pull_request_target\n", false},
		{"other event", "on:\n  pull_request:\n    types: [labeled]\n", false},
		{"alias to another event", "name: &event pull_request\non: [*event]\n", false},
		{"empty file", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := slices.Contains(workflowEvents(t, []byte(c.yaml)), "pull_request_target")
			if got != c.want {
				t.Errorf("pull_request_target found = %v, want %v", got, c.want)
			}
		})
	}
}

// TestOnlyTheTriageCallerUsesPullRequestTarget fails when a workflow other
// than the PR triage caller runs on pull_request_target. Move such a
// workflow to pull_request, or give it its own Actions policy with the same
// review as the caller's.
func TestOnlyTheTriageCallerUsesPullRequestTarget(t *testing.T) {
	var files []string
	for _, pattern := range []string{"../.github/workflows/*.yml", "../.github/workflows/*.yaml"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no workflows found under ../.github/workflows")
	}
	sort.Strings(files)
	callerFound := false
	for _, file := range files {
		name := filepath.Base(file)
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !slices.Contains(workflowEvents(t, data), "pull_request_target") {
			continue
		}
		if !pullRequestTargetAllowed[name] {
			t.Errorf("%s runs on pull_request_target; only risk-paths.yml may (frostyard/core docs/design/pull-request-target-policy.md)", name)
			continue
		}
		callerFound = true
	}
	if !callerFound {
		t.Error("risk-paths.yml no longer runs on pull_request_target: update pullRequestTargetAllowed and the organization policy together")
	}
}
