package testutil

import (
	"os"
	"syscall"
	"testing"
)

// Umask sets the process umask to mask for the rest of the test and restores
// the previous value with t.Cleanup. It exists for tests that assert a
// documented file or directory mode: a mode set through the umask alone
// changes with it, so such a test must run under a strict umask (0077) and a
// permissive one (0022).
//
// The umask is process-wide, so call it only from a test that is not
// parallel: Go runs parallel tests only after every sequential top-level test
// has finished, so a sequential test that sets and restores the umask in its
// own body never overlaps another test.
func Umask(t testing.TB, mask int) {
	t.Helper()
	old := syscall.Umask(mask)
	t.Cleanup(func() { syscall.Umask(old) })
}

// TrustedTempDir returns a new t.TempDir() chmodded to 0755, a parent that
// dirmode.MkdirAll trusts whatever the umask the test process started under.
//
// t.TempDir makes each directory it returns with mode 0777 under the process
// umask, so under umask 0002 (the default for a user with a private group,
// as on Fedora) it is group-writable. dirmode.MkdirAll deliberately leaves a
// directory it creates in a group-writable parent at the umask mode (the
// trusted-parent rule in internal/dirmode), so a test that pins the 0755 of
// such a directory under umask 0077 would fail there and pass in CI, which
// runs under 0022 and 0077 only (updex T55). In production the parent is a
// root-owned 0755 system directory such as /etc; this gives a test the same.
func TrustedTempDir(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
	return dir
}
