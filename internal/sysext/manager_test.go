package sysext

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/updex/internal/config"
)

func TestIsExtensionActive_WithSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	// Create extension file and symlink
	extFile := "myext_1.0.0.raw"
	if err := os.WriteFile(filepath.Join(tmpDir, extFile), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if err := os.Symlink(extFile, filepath.Join(tmpDir, "myext.raw")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	transfer := &config.Transfer{
		Target: config.TargetSection{
			Path:           tmpDir,
			CurrentSymlink: "myext.raw",
		},
	}

	active, target := IsExtensionActive(transfer)
	if !active {
		t.Error("expected extension to be active")
	}
	if target != extFile {
		t.Errorf("target = %q, want %q", target, extFile)
	}
}

func TestIsExtensionActive_NoSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	transfer := &config.Transfer{
		Target: config.TargetSection{
			Path:           tmpDir,
			CurrentSymlink: "myext.raw",
		},
	}

	active, target := IsExtensionActive(transfer)
	if active {
		t.Error("expected extension to not be active")
	}
	if target != "" {
		t.Errorf("target = %q, want empty", target)
	}
}

func TestIsExtensionActive_NoCurrentSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	transfer := &config.Transfer{
		Target: config.TargetSection{
			Path: tmpDir,
		},
	}

	active, _ := IsExtensionActive(transfer)
	if active {
		t.Error("expected extension to not be active when no CurrentSymlink configured")
	}
}

func TestRemoveMatchingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some test extension files
	files := []string{
		"myext_1.0.0.raw",
		"myext_1.1.0.raw",
		"myext_2.0.0.raw",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	// Create a symlink too
	if err := os.Symlink("myext_2.0.0.raw", filepath.Join(tmpDir, "myext.raw")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	transfer := &config.Transfer{
		Target: config.TargetSection{
			Path:           tmpDir,
			MatchPattern:   "myext_@v.raw",
			CurrentSymlink: "myext.raw",
		},
	}

	removed, err := RemoveMatchingFiles(transfer)
	if err != nil {
		t.Fatalf("RemoveMatchingFiles() error = %v", err)
	}

	// Should remove symlink + 3 files = 4 total
	if len(removed) != 4 {
		t.Errorf("removed %d files, want 4; removed: %v", len(removed), removed)
	}

	// Verify all files are gone
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(tmpDir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted", f)
		}
	}

	// Verify symlink is gone
	if _, err := os.Lstat(filepath.Join(tmpDir, "myext.raw")); !os.IsNotExist(err) {
		t.Error("expected symlink myext.raw to be deleted")
	}
}

func TestRemoveMatchingFiles_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	transfer := &config.Transfer{
		Target: config.TargetSection{
			Path:         tmpDir,
			MatchPattern: "myext_@v.raw",
		},
	}

	removed, err := RemoveMatchingFiles(transfer)
	if err != nil {
		t.Fatalf("RemoveMatchingFiles() error = %v", err)
	}

	if len(removed) != 0 {
		t.Errorf("removed %d files from empty dir, want 0", len(removed))
	}
}

func TestRemoveMatchingFiles_OnlyMatchingFilesRemoved(t *testing.T) {
	tmpDir := t.TempDir()

	// Create matching and non-matching files
	if err := os.WriteFile(filepath.Join(tmpDir, "myext_1.0.0.raw"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "other_file.txt"), []byte("keep"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	transfer := &config.Transfer{
		Target: config.TargetSection{
			Path:         tmpDir,
			MatchPattern: "myext_@v.raw",
		},
	}

	removed, err := RemoveMatchingFiles(transfer)
	if err != nil {
		t.Fatalf("RemoveMatchingFiles() error = %v", err)
	}

	if len(removed) != 1 {
		t.Errorf("removed %d files, want 1", len(removed))
	}

	// Non-matching file should still exist
	if _, err := os.Stat(filepath.Join(tmpDir, "other_file.txt")); os.IsNotExist(err) {
		t.Error("expected other_file.txt to still exist")
	}
}

func TestUnlinkFromSysext_NoSymlink(t *testing.T) {
	transfer := &config.Transfer{
		Target: config.TargetSection{
			CurrentSymlink: "nonexistent.raw",
		},
	}

	// Should not error when symlink doesn't exist
	// (This will look in /var/lib/extensions which we can't control in tests,
	// but the function handles ENOENT gracefully)
	err := UnlinkFromSysext(transfer)
	if err != nil {
		t.Errorf("UnlinkFromSysext() error = %v, expected nil for missing symlink", err)
	}
}

func TestUnlinkFromSysext_NoCurrentSymlink(t *testing.T) {
	transfer := &config.Transfer{
		Target: config.TargetSection{},
	}

	err := UnlinkFromSysext(transfer)
	if err == nil {
		t.Error("expected error when no CurrentSymlink defined")
	}
}
