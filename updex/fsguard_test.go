package updex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/updex/v2/internal/testutil"
)

// mkdirAllMode creates every missing directory with the mode it was given,
// whatever the process umask is (issue #524: the documented 0755 of the
// lock, rollback and last-run directories must not depend on it).
func TestMkdirAllModeIgnoresTheProcessUmask(t *testing.T) {
	t.Run("new nested directories under umask 0077", func(t *testing.T) {
		testutil.Umask(t, 0o077)
		base := t.TempDir()
		dir := filepath.Join(base, "run", "updex", "nested")
		if err := mkdirAllMode(dir, 0o755); err != nil {
			t.Fatalf("mkdirAllMode: %v", err)
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
		if err := mkdirAllMode(filepath.Join(existing, "nested"), 0o755); err != nil {
			t.Fatalf("mkdirAllMode: %v", err)
		}
		if info, err := os.Lstat(existing); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("existing directory = %v, %v; want its own mode 0700 unchanged", info, err)
		}
		if info, err := os.Lstat(filepath.Join(existing, "nested")); err != nil || info.Mode().Perm() != 0o755 {
			t.Errorf("created directory = %v, %v; want a 0755 directory", info, err)
		}
	})

	t.Run("a symlink at the target path is not chmodded through", func(t *testing.T) {
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
		if err := mkdirAllMode(link, 0o755); err != nil {
			t.Fatalf("mkdirAllMode: %v", err)
		}
		if info, err := os.Lstat(target); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("symlink target = %v, %v; want its own mode 0700 unchanged", info, err)
		}
	})
}

func TestWriteManagedFileReplacesSymlinkAtManagedPath(t *testing.T) {
	dir := t.TempDir()
	managedPath := filepath.Join(dir, "zoxide.transfer")
	outsidePath := filepath.Join(t.TempDir(), "outside.conf")
	const outsideContent = "operator-owned\n"
	if err := os.WriteFile(outsidePath, []byte(outsideContent), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, managedPath); err != nil {
		t.Fatal(err)
	}

	const generated = "generated definition\n"
	if err := writeManagedFile(managedPath, generated); err != nil {
		t.Fatalf("writeManagedFile: %v", err)
	}

	outside, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(outside) != outsideContent {
		t.Errorf("outside symlink target = %q, want %q", outside, outsideContent)
	}
	assertRegularFile(t, managedPath, generated, 0644)
	assertOnlyEntries(t, dir, "zoxide.transfer")
}

func TestFileSnapshotRestoreReplacesSymlinkAtManagedPath(t *testing.T) {
	dir := t.TempDir()
	managedPath := filepath.Join(dir, "zoxide.feature")
	const original = "captured definition\n"
	if err := os.WriteFile(managedPath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(managedPath, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotFile(managedPath)
	if !snapshot.existed || !snapshot.captured {
		t.Fatalf("snapshot = %+v, want captured existing file", snapshot)
	}

	outsidePath := filepath.Join(t.TempDir(), "outside.conf")
	const outsideContent = "operator-owned\n"
	if err := os.WriteFile(outsidePath, []byte(outsideContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(managedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, managedPath); err != nil {
		t.Fatal(err)
	}

	snapshot.restore()

	outside, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(outside) != outsideContent {
		t.Errorf("outside symlink target = %q, want %q", outside, outsideContent)
	}
	assertRegularFile(t, managedPath, original, 0600)
	assertOnlyEntries(t, dir, "zoxide.feature")
}

func assertRegularFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != mode {
		t.Errorf("%s mode = %v, want regular %04o", path, info.Mode(), mode)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("%s contents = %q, want %q", path, data, content)
	}
}
