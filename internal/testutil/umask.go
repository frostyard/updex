package testutil

import (
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
