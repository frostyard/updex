package updex

import (
	"os"
	"path/filepath"
	"testing"
)

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
