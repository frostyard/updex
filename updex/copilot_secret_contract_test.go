package updex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCopilotAssignmentSecretIsCanonical pins the fleet-wide secret name and
// loud missing-secret failure required by frostyard/core ADR-0020.
func TestCopilotAssignmentSecretIsCanonical(t *testing.T) {
	const workflowPath = "../.github/workflows/ai-fix-requested.yml"
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read AI fix workflow: %v", err)
	}
	workflow := string(data)
	for _, required := range []string{
		"secrets.COPILOT_ASSIGNMENT_TOKEN",
		"COPILOT_ASSIGNMENT_TOKEN is not configured",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("AI fix workflow does not contain %q (core ADR-0020)", required)
		}
	}

	files, err := filepath.Glob("../.github/workflows/*.yml")
	if err != nil {
		t.Fatalf("list workflows: %v", err)
	}
	yamlFiles, err := filepath.Glob("../.github/workflows/*.yaml")
	if err != nil {
		t.Fatalf("list workflows: %v", err)
	}
	files = append(files, yamlFiles...)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(file), err)
		}
		for _, forbidden := range []string{"COPILOT_ASSIGN" + "_PAT", "COPILOT_AGENT" + "_TOKEN"} {
			if strings.Contains(string(data), forbidden) {
				t.Errorf("%s contains forbidden Copilot secret alias %q (core ADR-0020)", filepath.Base(file), forbidden)
			}
		}
	}
}
