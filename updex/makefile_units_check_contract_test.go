package updex

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestMakefileCIRecipeRunsUnitsCheck pins baseline row B19: local and hosted
// CI use the same rootless systemd verification gate (updex#536).
func TestMakefileCIRecipeRunsUnitsCheck(t *testing.T) {
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	recipe := extractRecipe(t, string(data), "ci:")
	for _, target := range []string{"test-units-check", "units-check"} {
		pattern := regexp.MustCompile(`(?m)^\t@?\$\(MAKE\)\s+` + target + `\s*$`)
		loc := pattern.FindStringIndex(recipe)
		if loc == nil {
			t.Errorf("Makefile ci recipe must invoke `$(MAKE) %s`", target)
		} else if pass := strings.Index(recipe, "CI gate passed"); pass >= 0 && loc[0] > pass {
			t.Errorf("Makefile ci recipe must invoke %s before reporting success", target)
		}
	}
	units := extractRecipe(t, string(data), "units-check:")
	if !regexp.MustCompile(`(?m)^\t@?\./scripts/units-check\.sh build/updex\s*$`).MatchString(units) {
		t.Error("Makefile units-check target must run scripts/units-check.sh build/updex")
	}

	workflow := readGoReleaserWorkflow(t, "../.github/workflows/ci.yml")
	job, ok := workflow.Jobs["units"]
	if !ok {
		t.Fatal("Tests workflow is missing the units job")
	}
	found := false
	for _, step := range job.Steps {
		if strings.TrimSpace(step.Run) == "make units-check" {
			found = true
		}
		if strings.Contains(step.Run, "sudo") || strings.Contains(step.Run, "systemd-analyze") {
			t.Errorf("units job must use the shared rootless target, not sudo or a direct systemd-analyze call: %s", step.Run)
		}
	}
	if !found {
		t.Error("units job must run make units-check")
	}
}
