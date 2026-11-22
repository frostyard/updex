package updex

import (
	"os"
	"strings"
	"testing"
)

// TestErofsCIInstallsRequiredTools keeps the real compressed-image test in
// every test gate, with its tools installed before tests run (updex#535).
func TestErofsCIInstallsRequiredTools(t *testing.T) {
	for _, workflow := range []struct {
		path string
		jobs []string
	}{
		{"../.github/workflows/ci.yml", []string{"unit-test", "race-test", "verify"}},
		{"../.github/workflows/nightly-compliance.yml", []string{"compliance"}},
	} {
		config := readGoReleaserWorkflow(t, workflow.path)
		for _, name := range workflow.jobs {
			t.Run(name, func(t *testing.T) {
				job, ok := config.Jobs[name]
				if !ok {
					t.Fatalf("%s is missing job %s", workflow.path, name)
				}
				install, test := -1, -1
				for i, step := range job.Steps {
					if install < 0 && strings.Contains(step.Run, "apt-get install") && strings.Contains(step.Run, "erofs-utils") {
						install = i
					}
					if test < 0 && (strings.Contains(step.Run, "go test") || strings.Contains(step.Run, "make ci")) {
						test = i
					}
				}
				if install < 0 || test < 0 || install >= test {
					t.Errorf("%s job %s must install erofs-utils before its first test step: install=%d test=%d", workflow.path, name, install, test)
				}
			})
		}
	}
}

// TestRealCompressedErofsNeverSkips enforces the required-tool policy:
// absent or outdated erofs-utils must fail, not silently remove coverage.
func TestRealCompressedErofsNeverSkips(t *testing.T) {
	data, err := os.ReadFile("../imagecompat/erofs_test.go")
	if err != nil {
		t.Fatalf("read erofs_test.go: %v", err)
	}
	_, body, ok := strings.Cut(string(data), "\nfunc TestReadExtensionReleaseRealCompressed(t *testing.T) {")
	if !ok {
		t.Fatal("erofs_test.go is missing TestReadExtensionReleaseRealCompressed")
	}
	body, _, _ = strings.Cut(body, "\nfunc ")
	if strings.Contains(body, "t.Skip") {
		t.Error("TestReadExtensionReleaseRealCompressed must fail with installation guidance, not call t.Skip (core ADR-0043)")
	}
}
