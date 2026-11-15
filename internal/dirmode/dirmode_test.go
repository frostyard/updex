package dirmode

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/frostyard/updex/v2/internal/testutil"
)

// MkdirAll creates every missing directory with the mode it was given,
// whatever the process umask is (issue #524: the documented 0755 of the
// lock, rollback and last-run directories must not depend on it).
func TestMkdirAllIgnoresTheProcessUmask(t *testing.T) {
	t.Run("new nested directories under umask 0077", func(t *testing.T) {
		testutil.Umask(t, 0o077)
		base := t.TempDir()
		dir := filepath.Join(base, "run", "updex", "nested")
		if err := MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		for _, p := range []string{
			filepath.Join(base, "run"),
			filepath.Join(base, "run", "updex"),
			dir,
		} {
			info, err := os.Lstat(p)
			if err != nil || !info.IsDir() || info.Mode().Perm() != 0o755 {
				t.Errorf("%s = %v, %v; want a 0755 directory", p, info, err)
			}
		}
	})

	t.Run("an existing directory keeps its mode", func(t *testing.T) {
		testutil.Umask(t, 0o077)
		base := t.TempDir()
		existing := filepath.Join(base, "rollback")
		if err := os.Mkdir(existing, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := MkdirAll(filepath.Join(existing, "nested"), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if info, err := os.Lstat(existing); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("existing directory = %v, %v; want its own mode 0700 unchanged", info, err)
		}
		if info, err := os.Lstat(filepath.Join(existing, "nested")); err != nil || info.Mode().Perm() != 0o755 {
			t.Errorf("created directory = %v, %v; want a 0755 directory", info, err)
		}
	})

	t.Run("an existing symlink at the target path is left alone (nothing is missing)", func(t *testing.T) {
		testutil.Umask(t, 0o077)
		base := t.TempDir()
		target := filepath.Join(base, "elsewhere")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(base, "rollback")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := MkdirAll(link, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		// Nothing is missing, so the walk creates and chmods nothing and the
		// directory the link points at keeps its own mode.
		if info, err := os.Lstat(target); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("symlink target = %v, %v; want its own mode 0700 unchanged", info, err)
		}
	})

	// MkdirAll binds every step to the descriptor of the directory it
	// created, so replacing the ancestor path after it was created cannot
	// redirect the next mkdir or chmod at an unrelated directory (the
	// O_NOFOLLOW in an open by path would protect only the last component).
	t.Run("a replaced ancestor does not redirect the chmod", func(t *testing.T) {
		testutil.Umask(t, 0o077)
		base := t.TempDir()
		other := filepath.Join(base, "other")
		private := filepath.Join(other, "private")
		if err := os.MkdirAll(private, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(private, 0o700); err != nil {
			t.Fatal(err)
		}
		state := filepath.Join(base, "state")
		moved := filepath.Join(base, "moved")

		// After base/state is created and chmodded, it is renamed away and a
		// symlink to other/ takes its place.
		afterStep = func(path string) {
			if path != state {
				return
			}
			if err := os.Rename(state, moved); err != nil {
				t.Fatalf("rename %s: %v", state, err)
			}
			if err := os.Symlink(other, state); err != nil {
				t.Fatalf("symlink %s: %v", state, err)
			}
		}
		t.Cleanup(func() { afterStep = nil })

		if err := MkdirAll(filepath.Join(state, "private"), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if info, err := os.Lstat(private); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("unrelated directory %s = %v, %v; want its own mode 0700 unchanged", private, info, err)
		}
		if info, err := os.Lstat(filepath.Join(moved, "private")); err != nil || !info.IsDir() || info.Mode().Perm() != 0o755 {
			t.Errorf("directory inside the moved ancestor %s = %v, %v; want a 0755 directory", filepath.Join(moved, "private"), info, err)
		}
	})

	// Unix has no atomic mkdir-and-open: in a parent other users can write,
	// the entry can be swapped between mkdirat and openat, so the chmod is
	// skipped and the directory keeps the umask-applied mode.
	t.Run("a swapped directory in a parent other users can write is not chmodded", func(t *testing.T) {
		testutil.Umask(t, 0o077)
		base := t.TempDir()
		shared := filepath.Join(base, "shared")
		if err := os.Mkdir(shared, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(shared, 0o777); err != nil {
			t.Fatal(err)
		}
		private := filepath.Join(base, "private")
		if err := os.Mkdir(private, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(private, 0o700); err != nil {
			t.Fatal(err)
		}
		gone := filepath.Join(base, "gone")
		swapped := filepath.Join(shared, "new")

		beforeOpen = func(path string) {
			if path != swapped {
				return
			}
			if err := os.Rename(swapped, gone); err != nil {
				t.Fatalf("rename %s: %v", swapped, err)
			}
			if err := os.Rename(private, swapped); err != nil {
				t.Fatalf("rename %s: %v", private, err)
			}
		}
		t.Cleanup(func() { beforeOpen = nil })

		if err := MkdirAll(swapped, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if info, err := os.Lstat(swapped); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Errorf("swapped-in directory %s = %v, %v; want its own mode 0700 unchanged", swapped, info, err)
		}
	})

	// The sticky bit makes the parent trusted again: other users cannot
	// rename an entry they do not own there.
	t.Run("a parent with the sticky bit is trusted", func(t *testing.T) {
		testutil.Umask(t, 0o077)
		base := t.TempDir()
		shared := filepath.Join(base, "shared")
		if err := os.Mkdir(shared, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(shared, os.ModeSticky|0o777); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(shared, "new")
		if err := MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode().Perm() != 0o755 {
			t.Errorf("%s = %v, %v; want a 0755 directory", dir, info, err)
		}
	})
}

// TestMkdirAllKeepsInheritedSetgid pins that a directory MkdirAll creates
// inside a setgid parent keeps the setgid bit the kernel gives it, as
// os.MkdirAll does: the chmod that makes the mode explicit must not clear it,
// or a shared group would stop inheriting the directories created below it
// (cross-review of PR #528).
func TestMkdirAllKeepsInheritedSetgid(t *testing.T) {
	for _, tc := range []struct {
		name string
		mask int
	}{
		{name: "umask 0022", mask: 0o022},
		{name: "umask 0077", mask: 0o077},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Umask(t, tc.mask)
			parent := t.TempDir()
			if err := os.Chmod(parent, os.ModeSetgid|0o755); err != nil {
				t.Fatalf("chmod %s: %v", parent, err)
			}
			info, err := os.Lstat(parent)
			if err != nil {
				t.Fatalf("Lstat %s: %v", parent, err)
			}
			if info.Mode()&os.ModeSetgid == 0 {
				t.Skipf("the platform dropped the setgid bit: %s mode = %v", parent, info.Mode())
			}

			dirA := filepath.Join(parent, "a")
			dirB := filepath.Join(dirA, "b")
			if err := MkdirAll(dirB, 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			for _, p := range []string{dirA, dirB} {
				got, err := os.Lstat(p)
				if err != nil {
					t.Fatalf("Lstat %s: %v", p, err)
				}
				if got.Mode().Perm() != 0o755 {
					t.Errorf("%s perm = %04o, want 0755", p, got.Mode().Perm())
				}
				if got.Mode()&os.ModeSetgid == 0 {
					t.Errorf("%s mode = %v, want the inherited setgid bit kept", p, got.Mode())
				}
			}
		})
	}
}

// trustedParent trusts a directory owned by the test's user only when group
// and others cannot write it, or when the sticky bit keeps them from renaming
// an entry they do not own.
func TestTrustedParent(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode os.FileMode
		want bool
	}{
		{name: "0755", mode: 0o755, want: true},
		{name: "0700", mode: 0o700, want: true},
		{name: "0777", mode: 0o777, want: false},
		{name: "1777 sticky", mode: os.ModeSticky | 0o777, want: true},
		{name: "0775", mode: 0o775, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "parent")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, tc.mode); err != nil {
				t.Fatal(err)
			}
			fd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = syscall.Close(fd) }()
			got, err := trustedParent(fd)
			if err != nil {
				t.Fatalf("trustedParent: %v", err)
			}
			if got != tc.want {
				t.Errorf("trustedParent(%v) = %t, want %t", tc.mode, got, tc.want)
			}
		})
	}
}
