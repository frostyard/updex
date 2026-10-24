package updex

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/updex/v2/sysext"
)

// TestMain points every default-constructed client at a temporary lock file
// and rollback directory, so no test touches /run/updex/lock or
// /var/lib/updex (the production defaults), and at empty
// temporary merged-state hierarchies, so the vacuum that follows an install
// never reads the host's /usr/.systemd-sysext and /opt/.systemd-sysext. Tests run
// one after another in a shared process, so one lock file is enough; a test
// that runs clients concurrently or that tests the lock itself gives each
// client its own RuntimePaths.LockPath. There is no *testing.T here, so this
// uses os.MkdirTemp.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "updex-sdk-lock-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temporary lock directory: %v\n", err)
		os.Exit(1)
	}
	lockPathDefault = filepath.Join(dir, "lock")
	rollbackDirDefault = filepath.Join(dir, "rollback")
	sysext.MergedHierarchies = []string{filepath.Join(dir, "usr-hierarchy"), filepath.Join(dir, "opt-hierarchy")}

	code := m.Run()

	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintf(os.Stderr, "remove temporary lock directory: %v\n", err)
	}
	os.Exit(code)
}
