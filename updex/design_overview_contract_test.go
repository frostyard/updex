package updex

import (
	"os"
	"strings"
	"testing"
)

// TestDesignOverviewListsInternalPackages pins docs/design/overview.md to the
// live module layout: every directory under internal/ must be named in the
// design overview as `internal/<name>` so a new module-internal package
// cannot land undocumented (ADR-0008 made internal/retry the shared retry
// policy without the overview catching up).
func TestDesignOverviewListsInternalPackages(t *testing.T) {
	entries, err := os.ReadDir("../internal")
	if err != nil {
		t.Fatalf("read internal packages: %v", err)
	}

	data, err := os.ReadFile("../docs/design/overview.md")
	if err != nil {
		t.Fatalf("read design overview: %v", err)
	}
	contents := string(data)

	found := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		found++
		if required := "internal/" + entry.Name(); !strings.Contains(contents, required) {
			t.Errorf("docs/design/overview.md does not mention %q", required)
		}
	}
	if found == 0 {
		t.Fatal("no directories found under internal/; the test would prove nothing")
	}
}
