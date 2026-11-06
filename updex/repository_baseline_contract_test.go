package updex

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// This file pins the repository baseline of frostyard/core ADR-0071
// (docs/specs/repository-baseline.md, rows B1 to B16) for updex. There is one
// test per row, each naming the row it pins, so a pull request that drops a
// baseline artifact fails the unit tests instead of a fleet audit.

// repositoryFile returns the absolute path of a repository file, given a
// slash-separated path relative to the repository root. The package's tests
// run in updex/, so the root is "..".
func repositoryFile(t *testing.T, slashPath string) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return filepath.Join(root, filepath.FromSlash(slashPath))
}

// requireRegularFile fails when path is missing, is a symlink, or is not a
// regular file.
func requireRegularFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Errorf("%s is missing: %v", path, err)
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("%s is a symlink, want a regular file", path)
		return
	}
	if !info.Mode().IsRegular() {
		t.Errorf("%s has mode %v, want a regular file", path, info.Mode())
	}
}

// trackedFiles returns the file list of the committed tree: the paths git
// tracks, relative to the repository root, slash-separated. The baseline spec
// judges the committed tree, not a working tree, so an untracked local file
// (a developer's `.claude/settings.local.json`, say) never fails the gate.
// It splits `git ls-files -z` on NUL, because a path may contain a space.
func trackedFiles(t *testing.T) []string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; cannot list the committed tree")
	}
	out, err := exec.Command("git", "-C", "..", "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files -z: %v", err)
	}
	var files []string
	for _, tracked := range strings.Split(string(out), "\x00") {
		if tracked != "" {
			files = append(files, tracked)
		}
	}
	if len(files) == 0 {
		t.Fatal("git ls-files listed no tracked files")
	}
	return files
}

// TestRepositoryBaselineB1CanonicalInstructions pins row B1 of frostyard/core
// ADR-0071: AGENTS.md is the one canonical instruction file and every
// conformance alias is a committed relative symlink that resolves to it.
func TestRepositoryBaselineB1CanonicalInstructions(t *testing.T) {
	canonical := repositoryFile(t, "AGENTS.md")
	requireRegularFile(t, canonical)

	aliases := []string{
		"CLAUDE.md",
		"GEMINI.md",
		".cursorrules",
		".github/copilot-instructions.md",
	}
	for _, alias := range aliases {
		link := repositoryFile(t, alias)
		info, err := os.Lstat(link)
		if err != nil {
			t.Errorf("row B1: alias %s is missing: %v", alias, err)
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("row B1: alias %s is not a symlink; it must point at AGENTS.md, never hold a copy", alias)
			continue
		}
		target, err := os.Readlink(link)
		if err != nil {
			t.Errorf("row B1: read link %s: %v", alias, err)
			continue
		}
		if filepath.IsAbs(target) {
			t.Errorf("row B1: alias %s has absolute target %q, want a relative one that survives a checkout anywhere", alias, target)
			continue
		}
		resolved := filepath.Clean(filepath.Join(filepath.Dir(link), target))
		if resolved != canonical {
			t.Errorf("row B1: alias %s resolves to %q, want %q", alias, resolved, canonical)
		}
	}
}

// TestRepositoryBaselineB3DocsShape pins row B3 of frostyard/core ADR-0071:
// the four-category docs/ tree with its index, the org ADR list, and a
// TEMPLATE.md in every category.
func TestRepositoryBaselineB3DocsShape(t *testing.T) {
	for _, slashPath := range []string{
		"docs/README.md",
		"docs/org-adrs.md",
		"docs/adr/TEMPLATE.md",
		"docs/design/TEMPLATE.md",
		"docs/specs/TEMPLATE.md",
		"docs/plans/TEMPLATE.md",
	} {
		requireRegularFile(t, repositoryFile(t, slashPath))
	}

	index, err := os.ReadFile(repositoryFile(t, "docs/README.md"))
	if err != nil {
		t.Fatalf("read docs/README.md: %v", err)
	}
	if !strings.Contains(string(index), "org-adrs.md") {
		t.Errorf("row B3: docs/README.md does not index org-adrs.md")
	}

	orgADRs, err := os.ReadFile(repositoryFile(t, "docs/org-adrs.md"))
	if err != nil {
		t.Fatalf("read docs/org-adrs.md: %v", err)
	}
	if !strings.Contains(string(orgADRs), "0071-hold-every-code-repository-to-one-baseline") {
		t.Errorf("row B3: docs/org-adrs.md does not list ADR-0071")
	}
}

// TestRepositoryBaselineB4MakeGateTriad pins row B4 of frostyard/core ADR-0071
// (and ADR-0044): the Makefile exposes verify, check and ci, and ci calls
// verify rather than verify-static.
func TestRepositoryBaselineB4MakeGateTriad(t *testing.T) {
	data, err := os.ReadFile(repositoryFile(t, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}

	rules := map[string][]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ".") {
			continue // recipe line, comment, or .PHONY
		}
		name, rest, ok := strings.Cut(line, ":")
		if !ok || strings.Contains(name, "=") || strings.ContainsAny(name, " \t") {
			continue
		}
		fields := strings.Fields(rest)
		// A "rule: prereq" line may carry no recipe; split the header off a
		// target that continues on it (Make's `target: prereq` form).
		rules[name] = fields
	}

	for _, rule := range []string{"verify", "check", "ci"} {
		if _, ok := rules[rule]; !ok {
			t.Errorf("row B4: the Makefile has no %q rule", rule)
		}
	}
	prereqs, ok := rules["ci"]
	if !ok {
		return
	}
	if !slices.Contains(prereqs, "verify") {
		t.Errorf("row B4: the ci rule prerequisites are %v, want `verify` (core ADR-0044: ci calls verify and adds what CI alone runs)", prereqs)
	}

	for _, slashPath := range []string{"mise.toml", "mise.lock"} {
		requireRegularFile(t, repositoryFile(t, slashPath))
	}
}

// TestRepositoryBaselineB7PullRequestTemplate pins row B7 of frostyard/core
// ADR-0071: the pull-request template carries the risk-tier heading.
func TestRepositoryBaselineB7PullRequestTemplate(t *testing.T) {
	data, err := os.ReadFile(repositoryFile(t, ".github/pull_request_template.md"))
	if err != nil {
		t.Fatalf("read pull request template: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "#") && strings.Contains(line, "Risk tier") {
			return
		}
	}
	t.Error("row B7: .github/pull_request_template.md has no heading line that contains \"Risk tier\"")
}

// TestRepositoryBaselineB12CIEntryWorkflow pins row B12 of frostyard/core
// ADR-0071: CI runs from .github/workflows/ci.yml, keeps the workflow name
// "Tests" that snapshot.yml triggers on, and its gate is `make ci`.
func TestRepositoryBaselineB12CIEntryWorkflow(t *testing.T) {
	data, err := os.ReadFile(repositoryFile(t, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatalf("read ci workflow: %v", err)
	}

	var workflow struct {
		Name string         `yaml:"name"`
		On   map[string]any `yaml:"on"`
		Jobs map[string]struct {
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse ci workflow: %v", err)
	}

	if workflow.Name != "Tests" {
		t.Errorf("row B12: ci workflow name = %q, want %q (snapshot.yml triggers on the workflow named Tests)", workflow.Name, "Tests")
	}
	if _, ok := workflow.On["pull_request"]; !ok {
		t.Errorf("row B12: ci workflow does not trigger on pull_request")
	}

	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.TrimSpace(step.Run) == "make ci" {
				return
			}
		}
	}
	t.Error("row B12: no ci workflow step runs `make ci`")
}

// dependencyEcosystems maps a repository file to the Dependabot package
// ecosystem that must watch it.
var dependencyEcosystems = map[string]string{
	"go.mod":        "gomod",
	"package.json":  "npm",
	"Cargo.toml":    "cargo",
	"Dockerfile":    "docker",
	"Containerfile": "docker",
}

// missingEcosystems reports every package ecosystem a repository needs but
// `.github/dependabot.yml` does not configure: the ecosystems the given files
// imply (by basename), plus github-actions, which every repository needs, minus
// the configured ones. The result is sorted, and nil when nothing is missing.
func missingEcosystems(files []string, ecosystems []string) []string {
	required := map[string]bool{"github-actions": true}
	for _, file := range files {
		if ecosystem, ok := dependencyEcosystems[filepath.Base(file)]; ok {
			required[ecosystem] = true
		}
	}
	configured := make(map[string]bool, len(ecosystems))
	for _, ecosystem := range ecosystems {
		configured[ecosystem] = true
	}

	var missing []string
	for ecosystem := range required {
		if !configured[ecosystem] {
			missing = append(missing, ecosystem)
		}
	}
	slices.Sort(missing)
	return missing
}

// TestRepositoryBaselineB13DependabotEcosystems pins row B13 of frostyard/core
// ADR-0071: `.github/dependabot.yml` configures Dependabot for every package
// ecosystem the repository holds, and always for github-actions.
func TestRepositoryBaselineB13DependabotEcosystems(t *testing.T) {
	data, err := os.ReadFile(repositoryFile(t, ".github/dependabot.yml"))
	if err != nil {
		t.Fatalf("read dependabot config: %v", err)
	}
	var config struct {
		Updates []struct {
			PackageEcosystem string `yaml:"package-ecosystem"`
		} `yaml:"updates"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse dependabot config: %v", err)
	}
	configured := make([]string, 0, len(config.Updates))
	for _, update := range config.Updates {
		if update.PackageEcosystem != "" {
			configured = append(configured, update.PackageEcosystem)
		}
	}
	for _, ecosystem := range []string{"github-actions", "gomod"} {
		if !slices.Contains(configured, ecosystem) {
			t.Errorf("row B13: .github/dependabot.yml does not configure the %q ecosystem (configured: %v)", ecosystem, configured)
		}
	}

	tests := []struct {
		name       string
		files      []string
		ecosystems []string
		want       []string
	}{
		{
			name:       "everything configured",
			files:      []string{"go.mod", "package.json", "Cargo.toml", "Dockerfile"},
			ecosystems: []string{"gomod", "npm", "cargo", "docker", "github-actions"},
			want:       nil,
		},
		{
			name:       "github-actions is always required",
			files:      []string{"README.md"},
			ecosystems: []string{"gomod"},
			want:       []string{"github-actions"},
		},
		{
			name:       "a go.mod without a gomod entry fails",
			files:      []string{"go.mod"},
			ecosystems: []string{"github-actions"},
			want:       []string{"gomod"},
		},
		{
			name:       "a nested manifest counts by basename",
			files:      []string{"internal/tools/package.json"},
			ecosystems: []string{"gomod", "github-actions"},
			want:       []string{"npm"},
		},
		{
			name:       "a container file needs docker",
			files:      []string{"Containerfile"},
			ecosystems: []string{"gomod", "github-actions"},
			want:       []string{"docker"},
		},
		{
			name:       "an unrelated file names no ecosystem",
			files:      []string{"docs/README.md", "Makefile"},
			ecosystems: []string{"gomod", "github-actions"},
			want:       nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := missingEcosystems(tt.files, tt.ecosystems)
			if !slices.Equal(got, tt.want) {
				t.Errorf("missingEcosystems(%v, %v) = %v, want %v", tt.files, tt.ecosystems, got, tt.want)
			}
		})
	}

	files := trackedFiles(t)
	if missing := missingEcosystems(files, configured); len(missing) > 0 {
		t.Errorf("row B13: .github/dependabot.yml does not configure the ecosystem(s) %v that the repository files need", missing)
	}
}

// TestRepositoryBaselineB14SecurityPolicy pins row B14 of frostyard/core
// ADR-0071: the repository publishes a security policy that names GitHub
// private vulnerability reporting.
func TestRepositoryBaselineB14SecurityPolicy(t *testing.T) {
	var (
		path string
		data []byte
		err  error
	)
	for _, candidate := range []string{"SECURITY.md", ".github/SECURITY.md"} {
		path = repositoryFile(t, candidate)
		data, err = os.ReadFile(path)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("row B14: neither SECURITY.md nor .github/SECURITY.md is readable: %v", err)
	}
	const want = "https://github.com/frostyard/updex/security"
	if !strings.Contains(string(data), want) {
		t.Errorf("row B14: %s does not name GitHub private vulnerability reporting (%s)", path, want)
	}
}

// codeOwnersPaths are the locations a CODEOWNERS file could sit in.
var codeOwnersPaths = []string{"CODEOWNERS", ".github/CODEOWNERS", "docs/CODEOWNERS"}

// findCodeOwners returns every tracked path that is a CODEOWNERS file.
func findCodeOwners(files []string) []string {
	var found []string
	for _, file := range files {
		if slices.Contains(codeOwnersPaths, file) {
			found = append(found, file)
		}
	}
	return found
}

// retiredAgentTrees are the directory prefixes (each with its trailing slash,
// so `docs/superpowers-notes.md` is not a match) that must not come back.
var retiredAgentTrees = []string{
	"docs/superpowers/",
	".planning/",
	".cursor/rules/",
	".gaai/",
	"yeti/",
	"cairn/",
}

// findRetiredAgentTrees returns every tracked path that lives in a retired
// agent-tooling tree.
func findRetiredAgentTrees(files []string) []string {
	var found []string
	for _, file := range files {
		for _, retired := range retiredAgentTrees {
			if strings.HasPrefix(file, retired) {
				found = append(found, file)
				break
			}
		}
	}
	return found
}

// claudeAllowedEntries are the only names allowed directly under `.claude/`.
var claudeAllowedEntries = []string{"settings.json", "skills", "session-summary.md"}

// claudeEntriesNotAllowed returns, sorted and de-duplicated, the first path
// element below `.claude/` of every tracked path, keeping only the ones the
// baseline does not allow.
func claudeEntriesNotAllowed(files []string) []string {
	var notAllowed []string
	seen := map[string]bool{}
	for _, file := range files {
		rest, ok := strings.CutPrefix(file, ".claude/")
		if !ok {
			continue
		}
		entry, _, _ := strings.Cut(rest, "/")
		if entry == "" || seen[entry] {
			continue
		}
		seen[entry] = true
		if !slices.Contains(claudeAllowedEntries, entry) {
			notAllowed = append(notAllowed, entry)
		}
	}
	slices.Sort(notAllowed)
	return notAllowed
}

// TestRepositoryBaselineB15NoCodeOwners pins row B15 of frostyard/core
// ADR-0071: the committed tree holds no CODEOWNERS file, because reviews follow
// the rubric, not a per-path owner list.
func TestRepositoryBaselineB15NoCodeOwners(t *testing.T) {
	files := trackedFiles(t)
	if found := findCodeOwners(files); len(found) > 0 {
		t.Errorf("row B15: the committed tree tracks %v; the baseline keeps no CODEOWNERS file", found)
	}
}

// TestRepositoryBaselineB16NoLegacyAgentTrees pins row B16 of frostyard/core
// ADR-0071: no retired agent-tooling tree is committed, and `.claude/` holds
// only its settings, the synced skills, and the session summary.
func TestRepositoryBaselineB16NoLegacyAgentTrees(t *testing.T) {
	files := trackedFiles(t)
	if found := findRetiredAgentTrees(files); len(found) > 0 {
		t.Errorf("row B16: the committed tree still holds the retired agent tree(s) %v", found)
	}
	if notAllowed := claudeEntriesNotAllowed(files); len(notAllowed) > 0 {
		t.Errorf("row B16: the committed tree holds .claude/%v, want only %v", notAllowed, claudeAllowedEntries)
	}
}

// TestRepositoryBaselineB16Checks pins the pure checks of row B16 of
// frostyard/core ADR-0071: a retired tree is matched by its directory prefix
// only, and `.claude/` may hold nothing but its settings, the synced skills
// and the session summary.
func TestRepositoryBaselineB16Checks(t *testing.T) {
	retiredTests := []struct {
		name  string
		files []string
		want  []string
	}{
		{
			name:  "the committed tree has no retired tree",
			files: []string{"AGENTS.md", "docs/README.md", ".claude/skills"},
			want:  nil,
		},
		{
			name:  "a retired yeti tree fails",
			files: []string{"AGENTS.md", "yeti/OVERVIEW.md"},
			want:  []string{"yeti/OVERVIEW.md"},
		},
		{
			name:  "a retired docs/superpowers tree fails",
			files: []string{"docs/superpowers/x.md"},
			want:  []string{"docs/superpowers/x.md"},
		},
		{
			name:  "a lookalike file outside the retired tree passes",
			files: []string{"docs/superpowers-notes.md"},
			want:  nil,
		},
	}
	for _, tt := range retiredTests {
		t.Run("retired trees: "+tt.name, func(t *testing.T) {
			got := findRetiredAgentTrees(tt.files)
			if !slices.Equal(got, tt.want) {
				t.Errorf("findRetiredAgentTrees(%v) = %v, want %v", tt.files, got, tt.want)
			}
		})
	}

	claudeTests := []struct {
		name  string
		files []string
		want  []string
	}{
		{
			name:  "the allowed entries pass",
			files: []string{".claude/settings.json", ".claude/skills/go/SKILL.md", ".claude/session-summary.md"},
			want:  nil,
		},
		{
			name:  "a local settings file fails",
			files: []string{".claude/settings.json", ".claude/settings.local.json"},
			want:  []string{"settings.local.json"},
		},
		{
			name:  "every unexpected entry is named once, sorted",
			files: []string{".claude/notes/a.md", ".claude/settings.local.json", ".claude/notes/b.md"},
			want:  []string{"notes", "settings.local.json"},
		},
	}
	for _, tt := range claudeTests {
		t.Run(".claude entries: "+tt.name, func(t *testing.T) {
			got := claudeEntriesNotAllowed(tt.files)
			if !slices.Equal(got, tt.want) {
				t.Errorf("claudeEntriesNotAllowed(%v) = %v, want %v", tt.files, got, tt.want)
			}
		})
	}
}
