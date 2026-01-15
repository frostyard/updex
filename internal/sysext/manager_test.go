package sysext

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/updex/internal/config"
)

func TestGetInstalledVersions(t *testing.T) {
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

	transfer := &config.Transfer{
		Target: config.TargetSection{
			Path:         tmpDir,
			MatchPattern: "myext_@v.raw",
		},
	}

	versions, current, err := GetInstalledVersions(transfer)
	if err != nil {
		t.Fatalf("GetInstalledVersions() error = %v", err)
	}

	if len(versions) != 3 {
		t.Errorf("got %d versions, want 3", len(versions))
	}

	// Current should be newest (2.0.0)
	if current != "2.0.0" {
		t.Errorf("current = %q, want %q", current, "2.0.0")
	}

	// Check all versions are present
	expected := map[string]bool{"1.0.0": true, "1.1.0": true, "2.0.0": true}
	for _, v := range versions {
		if !expected[v] {
			t.Errorf("unexpected version %q", v)
		}
		delete(expected, v)
	}
	if len(expected) > 0 {
		t.Errorf("missing versions: %v", expected)
	}
}

func TestGetInstalledVersionsWithSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	// Create extension files
	files := []string{
		"myext_1.0.0.raw",
		"myext_2.0.0.raw",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	// Create symlink pointing to older version
	symlinkPath := filepath.Join(tmpDir, "myext.raw")
	if err := os.Symlink("myext_1.0.0.raw", symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	transfer := &config.Transfer{
		Target: config.TargetSection{
			Path:           tmpDir,
			MatchPattern:   "myext_@v.raw",
			CurrentSymlink: "myext.raw",
		},
	}

	versions, current, err := GetInstalledVersions(transfer)
	if err != nil {
		t.Fatalf("GetInstalledVersions() error = %v", err)
	}

	// Should not count symlink as a version
	if len(versions) != 2 {
		t.Errorf("got %d versions, want 2", len(versions))
	}

	// Current should follow symlink, not newest
	if current != "1.0.0" {
		t.Errorf("current = %q, want %q", current, "1.0.0")
	}
}

func TestGetInstalledVersionsEmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	transfer := &config.Transfer{
		Target: config.TargetSection{
			Path:         tmpDir,
			MatchPattern: "myext_@v.raw",
		},
	}

	versions, current, err := GetInstalledVersions(transfer)
	if err != nil {
		t.Fatalf("GetInstalledVersions() error = %v", err)
	}

	if len(versions) != 0 {
		t.Errorf("got %d versions, want 0", len(versions))
	}

	if current != "" {
		t.Errorf("current = %q, want empty", current)
	}
}

func TestGetInstalledVersionsNonexistentDir(t *testing.T) {
	transfer := &config.Transfer{
		Target: config.TargetSection{
			Path:         "/nonexistent/path/that/should/not/exist",
			MatchPattern: "myext_@v.raw",
		},
	}

	versions, current, err := GetInstalledVersions(transfer)
	if err != nil {
		t.Fatalf("GetInstalledVersions() error = %v", err)
	}

	if len(versions) != 0 {
		t.Errorf("got %d versions, want nil or empty", len(versions))
	}

	if current != "" {
		t.Errorf("current = %q, want empty", current)
	}
}

func TestUpdateSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	// Create target file
	targetFile := "myext_1.0.0.raw"
	if err := os.WriteFile(filepath.Join(tmpDir, targetFile), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create symlink
	symlinkName := "myext.raw"
	if err := UpdateSymlink(tmpDir, symlinkName, targetFile); err != nil {
		t.Fatalf("UpdateSymlink() error = %v", err)
	}

	// Verify symlink
	symlinkPath := filepath.Join(tmpDir, symlinkName)
	target, err := os.Readlink(symlinkPath)
	if err != nil {
		t.Fatalf("failed to read symlink: %v", err)
	}

	if target != targetFile {
		t.Errorf("symlink target = %q, want %q", target, targetFile)
	}
}

func TestUpdateSymlinkReplace(t *testing.T) {
	tmpDir := t.TempDir()

	// Create target files
	for _, f := range []string{"myext_1.0.0.raw", "myext_2.0.0.raw"} {
		if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	symlinkName := "myext.raw"

	// Create initial symlink
	if err := UpdateSymlink(tmpDir, symlinkName, "myext_1.0.0.raw"); err != nil {
		t.Fatalf("UpdateSymlink() first call error = %v", err)
	}

	// Update symlink to new target
	if err := UpdateSymlink(tmpDir, symlinkName, "myext_2.0.0.raw"); err != nil {
		t.Fatalf("UpdateSymlink() second call error = %v", err)
	}

	// Verify updated symlink
	symlinkPath := filepath.Join(tmpDir, symlinkName)
	target, err := os.Readlink(symlinkPath)
	if err != nil {
		t.Fatalf("failed to read symlink: %v", err)
	}

	if target != "myext_2.0.0.raw" {
		t.Errorf("symlink target = %q, want %q", target, "myext_2.0.0.raw")
	}
}

func TestVacuumWithDetails(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test extension files (versions sorted: 3.0.0, 2.0.0, 1.0.0)
	files := []string{
		"myext_1.0.0.raw",
		"myext_2.0.0.raw",
		"myext_3.0.0.raw",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	transfer := &config.Transfer{
		Transfer: config.TransferSection{
			InstancesMax: 2, // Keep only 2
		},
		Target: config.TargetSection{
			Path:         tmpDir,
			MatchPattern: "myext_@v.raw",
		},
	}

	removed, kept, err := VacuumWithDetails(transfer)
	if err != nil {
		t.Fatalf("VacuumWithDetails() error = %v", err)
	}

	// Should keep 2 newest (3.0.0, 2.0.0) and remove 1 (1.0.0)
	if len(kept) != 2 {
		t.Errorf("kept %d versions, want 2", len(kept))
	}
	if len(removed) != 1 {
		t.Errorf("removed %d versions, want 1", len(removed))
	}

	// Verify the oldest was removed
	if len(removed) > 0 && removed[0] != "1.0.0" {
		t.Errorf("removed[0] = %q, want %q", removed[0], "1.0.0")
	}

	// Verify file was actually deleted
	if _, err := os.Stat(filepath.Join(tmpDir, "myext_1.0.0.raw")); !os.IsNotExist(err) {
		t.Error("expected myext_1.0.0.raw to be deleted")
	}

	// Verify kept files still exist
	for _, v := range []string{"myext_2.0.0.raw", "myext_3.0.0.raw"} {
		if _, err := os.Stat(filepath.Join(tmpDir, v)); err != nil {
			t.Errorf("expected %s to still exist", v)
		}
	}
}

func TestVacuumWithDetailsProtectedVersion(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test extension files
	files := []string{
		"myext_1.0.0.raw",
		"myext_2.0.0.raw",
		"myext_3.0.0.raw",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	transfer := &config.Transfer{
		Transfer: config.TransferSection{
			InstancesMax:   1,       // Keep only 1
			ProtectVersion: "1.0.0", // But protect 1.0.0
		},
		Target: config.TargetSection{
			Path:         tmpDir,
			MatchPattern: "myext_@v.raw",
		},
	}

	removed, kept, err := VacuumWithDetails(transfer)
	if err != nil {
		t.Fatalf("VacuumWithDetails() error = %v", err)
	}

	// Protected version should be kept even if InstancesMax=1
	keptMap := make(map[string]bool)
	for _, v := range kept {
		keptMap[v] = true
	}

	if !keptMap["1.0.0"] {
		t.Error("protected version 1.0.0 should be kept")
	}
	if !keptMap["3.0.0"] {
		t.Error("newest version 3.0.0 should be kept")
	}

	// 2.0.0 should be removed
	removedMap := make(map[string]bool)
	for _, v := range removed {
		removedMap[v] = true
	}

	if !removedMap["2.0.0"] {
		t.Error("version 2.0.0 should be removed")
	}
}

func TestVacuumWithDetailsEmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	transfer := &config.Transfer{
		Transfer: config.TransferSection{
			InstancesMax: 2,
		},
		Target: config.TargetSection{
			Path:         tmpDir,
			MatchPattern: "myext_@v.raw",
		},
	}

	removed, kept, err := VacuumWithDetails(transfer)
	if err != nil {
		t.Fatalf("VacuumWithDetails() error = %v", err)
	}

	if len(removed) != 0 {
		t.Errorf("removed %d versions from empty dir, want 0", len(removed))
	}
	if len(kept) != 0 {
		t.Errorf("kept %d versions from empty dir, want 0", len(kept))
	}
}

func TestVacuumWithDetailsNothingToRemove(t *testing.T) {
	tmpDir := t.TempDir()

	// Create exactly InstancesMax files
	files := []string{
		"myext_1.0.0.raw",
		"myext_2.0.0.raw",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	transfer := &config.Transfer{
		Transfer: config.TransferSection{
			InstancesMax: 2,
		},
		Target: config.TargetSection{
			Path:         tmpDir,
			MatchPattern: "myext_@v.raw",
		},
	}

	removed, kept, err := VacuumWithDetails(transfer)
	if err != nil {
		t.Fatalf("VacuumWithDetails() error = %v", err)
	}

	if len(removed) != 0 {
		t.Errorf("removed %d versions, want 0", len(removed))
	}
	if len(kept) != 2 {
		t.Errorf("kept %d versions, want 2", len(kept))
	}
}

func TestGetExtensionName(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"myext_1.2.3.raw", "myext"},
		{"myext_1.2.3.raw.xz", "myext"},
		{"myext_1.2.3.raw.gz", "myext"},
		{"myext_1.2.3.raw.zst", "myext"},
		{"my_ext_1.2.3.raw", "my_ext"},
		{"noversion.raw", "noversion"},
		{"ext", "ext"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := GetExtensionName(tt.filename)
			if result != tt.expected {
				t.Errorf("GetExtensionName(%q) = %q, want %q", tt.filename, result, tt.expected)
			}
		})
	}
}
