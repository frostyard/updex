package updex

import (
	"strings"
	"testing"
)

func TestUpdateExecStartIsQuiet(t *testing.T) {
	if !strings.Contains(updateExecStart, "--quiet") {
		t.Errorf("updateExecStart = %q, want it to include --quiet", updateExecStart)
	}
	if !strings.Contains(updateExecStart, "--no-refresh") {
		t.Errorf("updateExecStart = %q, want it to include --no-refresh", updateExecStart)
	}
}
