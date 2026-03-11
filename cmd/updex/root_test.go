package updex

import (
	"os"
	"testing"
)

func TestRequireRoot(t *testing.T) {
	err := requireRoot()
	if os.Geteuid() == 0 {
		if err != nil {
			t.Errorf("requireRoot() returned error when running as root: %v", err)
		}
	} else {
		if err == nil {
			t.Error("requireRoot() should return error when not running as root")
		}
		expectedMsg := "this operation requires root privileges"
		if err.Error() != expectedMsg {
			t.Errorf("requireRoot() error = %v, want %v", err.Error(), expectedMsg)
		}
	}
}
