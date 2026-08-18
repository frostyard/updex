package sysext

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/frostyard/updex/config"
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

func TestRemoveAllVersions(t *testing.T) {
	stagingDir := t.TempDir()
	versionedFiles := []string{
		"myext_1.0.0.raw",
		"myext_2.0.0.raw",
	}
	for _, name := range append(versionedFiles, "other_1.0.0.raw") {
		if err := os.WriteFile(filepath.Join(stagingDir, name), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	currentLink := filepath.Join(stagingDir, "myext.raw")
	if err := os.Symlink(versionedFiles[1], currentLink); err != nil {
		t.Fatalf("failed to create current symlink: %v", err)
	}
	versionedLink := filepath.Join(stagingDir, "myext_3.0.0.raw")
	if err := os.Symlink(versionedFiles[1], versionedLink); err != nil {
		t.Fatalf("failed to create versioned symlink: %v", err)
	}

	transfer := &config.Transfer{
		Target: config.TargetSection{
			Path:           stagingDir,
			MatchPattern:   "myext_@v.raw",
			CurrentSymlink: "myext.raw",
		},
	}

	removed, err := RemoveAllVersions(transfer)
	if err != nil {
		t.Fatalf("RemoveAllVersions() error = %v", err)
	}

	wantRemoved := []string{
		currentLink,
		filepath.Join(stagingDir, versionedFiles[0]),
		filepath.Join(stagingDir, versionedFiles[1]),
	}
	if !slices.Equal(removed, wantRemoved) {
		t.Errorf("RemoveAllVersions() removed = %v, want %v", removed, wantRemoved)
	}
	for _, path := range wantRemoved {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed, stat err = %v", path, err)
		}
	}
	for _, name := range []string{"other_1.0.0.raw", filepath.Base(versionedLink)} {
		path := filepath.Join(stagingDir, name)
		if _, err := os.Lstat(path); err != nil {
			t.Errorf("expected %s to remain: %v", path, err)
		}
	}
}

func TestRemoveAllVersionsAbsentDirectory(t *testing.T) {
	transfer := &config.Transfer{
		Target: config.TargetSection{
			Path:         filepath.Join(t.TempDir(), "absent"),
			MatchPattern: "myext_@v.raw",
		},
	}

	removed, err := RemoveAllVersions(transfer)
	if err != nil {
		t.Fatalf("RemoveAllVersions() error = %v", err)
	}
	if removed != nil {
		t.Errorf("RemoveAllVersions() removed = %v, want nil", removed)
	}
}

func TestRemoveAllVersionsPartialFailure(t *testing.T) {
	stagingDir := t.TempDir()
	firstVersion := filepath.Join(stagingDir, "myext_1.0.0.raw")
	if err := os.WriteFile(firstVersion, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	failingVersion := filepath.Join(stagingDir, "myext_2.0.0.raw")
	if err := os.Mkdir(failingVersion, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(failingVersion, "keep"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to populate test directory: %v", err)
	}
	currentLink := filepath.Join(stagingDir, "myext.raw")
	if err := os.Symlink(filepath.Base(firstVersion), currentLink); err != nil {
		t.Fatalf("failed to create current symlink: %v", err)
	}

	transfer := &config.Transfer{
		Target: config.TargetSection{
			Path:           stagingDir,
			MatchPattern:   "myext_@v.raw",
			CurrentSymlink: filepath.Base(currentLink),
		},
	}

	removed, err := RemoveAllVersions(transfer)
	if err == nil {
		t.Fatal("RemoveAllVersions() error = nil, want removal error")
	}
	if !strings.Contains(err.Error(), failingVersion) {
		t.Errorf("RemoveAllVersions() error = %q, want path %q", err, failingVersion)
	}
	wantRemoved := []string{currentLink, firstVersion}
	if !slices.Equal(removed, wantRemoved) {
		t.Errorf("RemoveAllVersions() removed = %v, want %v", removed, wantRemoved)
	}
	for _, path := range wantRemoved {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed before failure, stat err = %v", path, err)
		}
	}
	if _, err := os.Stat(failingVersion); err != nil {
		t.Errorf("expected failed removal target to remain: %v", err)
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

func TestSysextLinkName(t *testing.T) {
	tests := []struct {
		name      string
		component string
		pattern   string
		want      string
	}{
		{
			name:      "raw",
			component: "myext",
			pattern:   "different_@v.raw",
			want:      "myext.raw",
		},
		{
			name:      "compressed raw",
			component: "myext",
			pattern:   "different_@v.raw.xz",
			want:      "myext.raw",
		},
		{
			name:      "zstd long suffix",
			component: "myext",
			pattern:   "different_@v.raw.zstd",
			want:      "myext.raw",
		},
		{
			name:      "empty component",
			component: "",
			pattern:   "different_@v.raw",
			want:      "",
		},
		{
			name:      "empty pattern",
			component: "myext",
			pattern:   "",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transfer := &config.Transfer{
				Component: tt.component,
				Target: config.TargetSection{
					MatchPattern: tt.pattern,
				},
			}
			if got := SysextLinkName(transfer); got != tt.want {
				t.Errorf("SysextLinkName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRemoveLegacyCurrentSymlink(t *testing.T) {
	t.Run("no current symlink configured", func(t *testing.T) {
		transfer := &config.Transfer{
			Target: config.TargetSection{
				Path: t.TempDir(),
			},
		}
		if err := RemoveLegacyCurrentSymlink(transfer); err != nil {
			t.Fatalf("RemoveLegacyCurrentSymlink() error = %v", err)
		}
	})

	t.Run("symlink present", func(t *testing.T) {
		tmpDir := t.TempDir()
		targetFile := filepath.Join(tmpDir, "myext_1.0.0.raw")
		if err := os.WriteFile(targetFile, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		symlinkPath := filepath.Join(tmpDir, "myext.raw")
		if err := os.Symlink("myext_1.0.0.raw", symlinkPath); err != nil {
			t.Fatalf("failed to create legacy symlink: %v", err)
		}

		transfer := &config.Transfer{
			Target: config.TargetSection{
				Path:           tmpDir,
				CurrentSymlink: "myext.raw",
			},
		}
		if err := RemoveLegacyCurrentSymlink(transfer); err != nil {
			t.Fatalf("RemoveLegacyCurrentSymlink() error = %v", err)
		}
		if _, err := os.Lstat(symlinkPath); !os.IsNotExist(err) {
			t.Errorf("expected legacy symlink to be removed, stat err = %v", err)
		}
	})

	t.Run("symlink absent", func(t *testing.T) {
		transfer := &config.Transfer{
			Target: config.TargetSection{
				Path:           t.TempDir(),
				CurrentSymlink: "myext.raw",
			},
		}
		if err := RemoveLegacyCurrentSymlink(transfer); err != nil {
			t.Fatalf("RemoveLegacyCurrentSymlink() error = %v", err)
		}
	})
}

func TestLinkToSysextWithoutCurrentSymlink(t *testing.T) {
	stagingDir := t.TempDir()
	sysextDir := t.TempDir()
	oldSysextDir := SysextDir
	SysextDir = sysextDir
	t.Cleanup(func() { SysextDir = oldSysextDir })

	for _, f := range []string{"myext_1.0.0.raw", "myext_2.0.0.raw"} {
		if err := os.WriteFile(filepath.Join(stagingDir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	transfer := &config.Transfer{
		Component: "myext",
		Target: config.TargetSection{
			Path:         stagingDir,
			MatchPattern: "myext_@v.raw",
		},
	}

	if err := LinkToSysext(transfer); err != nil {
		t.Fatalf("LinkToSysext() error = %v", err)
	}

	linkPath := filepath.Join(sysextDir, "myext.raw")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("failed to read sysext link: %v", err)
	}
	want := filepath.Join(stagingDir, "myext_2.0.0.raw")
	if target != want {
		t.Errorf("sysext link target = %q, want %q", target, want)
	}
}

func TestUnlinkFromSysextWithoutCurrentSymlink(t *testing.T) {
	sysextDir := t.TempDir()
	oldSysextDir := SysextDir
	SysextDir = sysextDir
	t.Cleanup(func() { SysextDir = oldSysextDir })

	linkPath := filepath.Join(sysextDir, "myext.raw")
	if err := os.Symlink("/tmp/myext_1.0.0.raw", linkPath); err != nil {
		t.Fatalf("failed to create sysext link: %v", err)
	}

	transfer := &config.Transfer{
		Component: "myext",
		Target: config.TargetSection{
			MatchPattern: "myext_@v.raw",
		},
	}

	if err := UnlinkFromSysext(transfer); err != nil {
		t.Fatalf("UnlinkFromSysext() error = %v", err)
	}
	if _, err := os.Lstat(linkPath); !os.IsNotExist(err) {
		t.Errorf("expected sysext link to be removed, stat err = %v", err)
	}
}

func TestVacuumWithDetailsKeepsActiveSymlinkedVersion(t *testing.T) {
	tmpDir := t.TempDir()

	// Three versions present; the current symlink points at the OLDEST one.
	// This reproduces the dangling-symlink failure: vacuum must never delete
	// the file the CurrentSymlink resolves to, even when it sorts oldest.
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

	// Active version = 1.0.0 (the oldest).
	if err := os.Symlink("myext_1.0.0.raw", filepath.Join(tmpDir, "myext.raw")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	transfer := &config.Transfer{
		Transfer: config.TransferSection{
			InstancesMax: 1, // Keep only 1 by count...
		},
		Target: config.TargetSection{
			Path:           tmpDir,
			MatchPattern:   "myext_@v.raw",
			CurrentSymlink: "myext.raw",
		},
	}

	removed, kept, err := VacuumWithDetails(transfer)
	if err != nil {
		t.Fatalf("VacuumWithDetails() error = %v", err)
	}

	keptMap := make(map[string]bool)
	for _, v := range kept {
		keptMap[v] = true
	}
	// ...but the active version must be kept on top of the count.
	if !keptMap["1.0.0"] {
		t.Error("active (symlinked) version 1.0.0 must be kept")
	}
	if !keptMap["3.0.0"] {
		t.Error("newest version 3.0.0 should be kept")
	}

	// The active file must still exist on disk (no dangling symlink).
	if _, err := os.Stat(filepath.Join(tmpDir, "myext_1.0.0.raw")); err != nil {
		t.Errorf("active file myext_1.0.0.raw must still exist: %v", err)
	}

	removedMap := make(map[string]bool)
	for _, v := range removed {
		removedMap[v] = true
	}
	if !removedMap["2.0.0"] {
		t.Error("non-active middle version 2.0.0 should be removed")
	}
}

func TestPlanVacuumAfterInstallPreservesFiles(t *testing.T) {
	tmpDir := t.TempDir()
	files := []string{
		"myext_1.0.0.raw",
		"myext_2.0.0.raw",
		"myext_3.0.0.raw",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	transfer := &config.Transfer{
		Transfer: config.TransferSection{
			InstancesMax: 1,
		},
		Target: config.TargetSection{
			Path:         tmpDir,
			MatchPattern: "myext_@v.raw",
		},
	}

	removed, kept, err := PlanVacuumAfterInstall(transfer, "1.0.0")
	if err != nil {
		t.Fatalf("PlanVacuumAfterInstall() error = %v", err)
	}
	if len(removed) != 1 || removed[0] != "2.0.0" {
		t.Errorf("PlanVacuumAfterInstall() removed = %v, want [2.0.0]", removed)
	}
	if !slices.Equal(kept, []string{"3.0.0", "1.0.0"}) {
		t.Errorf("PlanVacuumAfterInstall() kept = %v, want [3.0.0 1.0.0]", kept)
	}

	for _, name := range files {
		if _, err := os.Stat(filepath.Join(tmpDir, name)); err != nil {
			t.Errorf("dry-run planner removed %s: %v", name, err)
		}
	}
}

func TestGetActiveVersionUsesCurrentSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	for _, name := range []string{"myext_1.0.0.raw", "myext_2.0.0.raw"} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}
	if err := os.Symlink("myext_1.0.0.raw", filepath.Join(tmpDir, "myext.raw")); err != nil {
		t.Fatalf("failed to create current symlink: %v", err)
	}

	transfer := &config.Transfer{
		Target: config.TargetSection{
			Path:           tmpDir,
			MatchPattern:   "myext_@v.raw",
			CurrentSymlink: "myext.raw",
		},
	}

	active, err := GetActiveVersion(transfer)
	if err != nil {
		t.Fatalf("GetActiveVersion() error = %v", err)
	}
	if active != "1.0.0" {
		t.Errorf("GetActiveVersion() = %q, want 1.0.0", active)
	}

	active, err = GetActiveVersionAt(transfer, t.TempDir())
	if err != nil {
		t.Fatalf("GetActiveVersionAt() error = %v", err)
	}
	if active != "1.0.0" {
		t.Errorf("GetActiveVersionAt() = %q, want 1.0.0", active)
	}
}

func TestGetActiveVersionInRunExtensions(t *testing.T) {
	transfer := &config.Transfer{
		Target: config.TargetSection{
			MatchPattern: "myext_@v.raw",
		},
	}

	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		want    string
		wantErr bool
	}{
		{
			name: "match",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "myext_2.0.0.raw"), []byte("test"), 0644); err != nil {
					t.Fatalf("write merged image: %v", err)
				}
				return dir
			},
			want: "2.0.0",
		},
		{
			name: "no match",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "other_2.0.0.raw"), []byte("test"), 0644); err != nil {
					t.Fatalf("write unrelated merged image: %v", err)
				}
				return dir
			},
		},
		{
			name: "directory absent",
			setup: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "missing")
			},
		},
		{
			name: "read error",
			setup: func(t *testing.T) string {
				t.Helper()
				path := filepath.Join(t.TempDir(), "not-a-directory")
				if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
					t.Fatalf("write non-directory path: %v", err)
				}
				return path
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			active, err := GetActiveVersionIn(transfer, t.TempDir(), tt.setup(t))
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetActiveVersionIn() error = %v, wantErr %v", err, tt.wantErr)
			}
			if active != tt.want {
				t.Errorf("GetActiveVersionIn() = %q, want %q", active, tt.want)
			}
		})
	}
}

func TestGetActiveVersionInRunExtensionsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory read permissions")
	}

	runExtensionsDir := t.TempDir()
	if err := os.Chmod(runExtensionsDir, 0000); err != nil {
		t.Fatalf("make merged-image directory unreadable: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(runExtensionsDir, 0700); err != nil {
			t.Errorf("restore merged-image directory permissions: %v", err)
		}
	})

	transfer := &config.Transfer{
		Target: config.TargetSection{
			MatchPattern: "myext_@v.raw",
		},
	}
	if _, err := GetActiveVersionIn(transfer, t.TempDir(), runExtensionsDir); err == nil {
		t.Fatal("GetActiveVersionIn() error = nil, want unreadable-directory error")
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
