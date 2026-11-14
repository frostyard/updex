package updex

import (
	"os"
	"strings"
	"testing"
)

// TestMakefileVerifyRunsTestsUnderUmask0077 pins the issue #524 guard: a mode
// that a test asserts is documented, not whatever the process umask leaves,
// so `make verify` runs the unit tests under umask 0077 while `make ci`'s
// coverage run uses the caller's umask (0022 in GitHub CI). Both umasks
// therefore run in CI, and a mode that depends on the umask fails one of them.
func TestMakefileVerifyRunsTestsUnderUmask0077(t *testing.T) {
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	recipe := extractRecipe(t, string(data), "verify:")

	found := false
	for _, line := range strings.Split(recipe, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "$(GO) test") {
			continue
		}
		found = true
		if !strings.HasPrefix(line, "umask 0077 && $(GO) test") {
			t.Errorf("verify runs the unit tests under the caller's umask; the line must start with `umask 0077 && `:\n%s", line)
		}
		// The test cache does not key on the umask, so a cached result from
		// a run under another umask would pass without running the tests.
		if !strings.Contains(line, " -count=1 ") {
			t.Errorf("verify's umask 0077 test run must bypass the test cache with -count=1:\n%s", line)
		}
	}
	if !found {
		t.Fatalf("verify recipe has no go test line:\n%s", recipe)
	}
}
