package common

import (
	"os"
	"testing"
)

func TestRequireRoot(t *testing.T) {
	// This test runs as non-root in CI
	err := RequireRoot()
	if os.Geteuid() == 0 {
		// Running as root
		if err != nil {
			t.Errorf("RequireRoot() returned error when running as root: %v", err)
		}
	} else {
		// Not running as root
		if err == nil {
			t.Error("RequireRoot() should return error when not running as root")
		}
		expectedMsg := "this operation requires root privileges"
		if err.Error() != expectedMsg {
			t.Errorf("RequireRoot() error = %v, want %v", err.Error(), expectedMsg)
		}
	}
}
