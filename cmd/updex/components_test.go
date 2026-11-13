package updex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunComponents_RefusesDefinitionsOverride covers #508 at the CLI:
// `updex -C <dir> components` must fail rather than print the host-wide
// component list, because a definitions override is 1 flat directory with no
// components (the SDK reports it in updex/domain.go).
func TestRunComponents_RefusesDefinitionsOverride(t *testing.T) {
	oldDefinitions := definitions
	t.Cleanup(func() { definitions = oldDefinitions })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "local.feature"), []byte("[Feature]\nEnabled=false\n"), 0o644); err != nil {
		t.Fatalf("write feature file: %v", err)
	}
	definitions = dir

	out := &failingWriter{}
	cmd := cmdWithOut(t, out)

	err := runComponents(cmd, nil)
	if err == nil {
		t.Fatal("runComponents with -C set returned no error, want the definitions-override refusal")
	}
	want := "components cannot be combined with a definitions override (--definitions): a definitions directory has no components"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("runComponents error = %q, want it to contain %q", err.Error(), want)
	}
	if out.calls != 0 {
		t.Errorf("a failed components run wrote %d chunk(s) to stdout, want none (no table, no 'No components discovered.')", out.calls)
	}
}
