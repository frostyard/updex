package testutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// currentUmask reads the process umask by setting it and restoring it at
// once. The umask is process-wide, so this runs from one sequential test.
func currentUmask() int {
	mask := syscall.Umask(0o022)
	syscall.Umask(mask)
	return mask
}

// Umask applies the mask to files the test creates and restores the previous
// value when the test ends.
func TestUmaskAppliesAndIsRestored(t *testing.T) {
	ambient := currentUmask()

	var got os.FileMode
	t.Run("applied", func(t *testing.T) {
		Umask(t, 0o077)
		if mask := currentUmask(); mask != 0o077 {
			t.Fatalf("umask = %04o, want 0077", mask)
		}
		path := filepath.Join(t.TempDir(), "record")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		got = info.Mode().Perm()
	})

	if got != 0o600 {
		t.Errorf("file created under umask 0077 has mode %04o, want 0600", got)
	}
	if mask := currentUmask(); mask != ambient {
		t.Errorf("umask after the test = %04o, want the previous %04o", mask, ambient)
	}
}
