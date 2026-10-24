package sysext

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/config"
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

// writeMergedStateFile writes one systemd-sysext merged-state file under the
// given hierarchy and returns its path.
func writeMergedStateFile(t *testing.T, hierarchy, name, content string) string {
	t.Helper()
	stateDir := filepath.Join(hierarchy, MergedStateDirName)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("create merged-state directory: %v", err)
	}
	path := filepath.Join(stateDir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write merged-state file %s: %v", name, err)
	}
	return path
}

func TestGetActiveVersionWithMergedState(t *testing.T) {
	const (
		image       = "docker_5+29.9.1-1~debian.13~trixie_13_x86-64.raw"
		imagePath   = "/var/lib/extensions.d/" + image
		wantVersion = "5+29.9.1-1~debian.13~trixie"
		pattern     = "docker_@v_13_x86-64.raw"
	)

	// originWith renders the shape systemd-sysext writes, including fields
	// updex does not read.
	originWith := func(name, path string) string {
		return `{
  "mutable" : { "mode" : "no" },
  "extensions" : {
    "` + name + `" : {
      "path" : "` + path + `",
      "onMountId" : 2147483764,
      "fileHandle" : { "type" : 77, "handle" : "6a8d" },
      "crtime" : 1791467316045655,
      "mtime" : 1791467316149657
    }
  }
}`
	}

	// statePaths returns paths whose runtime image search directory does not
	// exist, as on a snosi host.
	statePaths := func(t *testing.T, hierarchies ...string) ActiveStatePaths {
		t.Helper()
		return ActiveStatePaths{
			DefaultDir:       t.TempDir(),
			RunExtensionsDir: filepath.Join(t.TempDir(), "run-extensions"),
			Hierarchies:      hierarchies,
		}
	}

	tests := []struct {
		name           string
		component      string
		currentSymlink string
		setup          func(t *testing.T) ActiveStatePaths
		want           string
		wantErr        bool
		skipAsRoot     bool
	}{
		{
			name:      "snosi origin records the merged image",
			component: "docker",
			setup: func(t *testing.T) ActiveStatePaths {
				hierarchy := t.TempDir()
				writeMergedStateFile(t, hierarchy, "origin", originWith("docker", imagePath))
				return statePaths(t, hierarchy)
			},
			want: wantVersion,
		},
		{
			name:      "origin matches by source path only",
			component: "renamed-service",
			setup: func(t *testing.T) ActiveStatePaths {
				hierarchy := t.TempDir()
				writeMergedStateFile(t, hierarchy, "origin", originWith("renamed", imagePath))
				return statePaths(t, hierarchy)
			},
			want: wantVersion,
		},
		{
			name:      "origin matches by name with an unrecognized path",
			component: "docker",
			setup: func(t *testing.T) ActiveStatePaths {
				hierarchy := t.TempDir()
				writeMergedStateFile(t, hierarchy, "origin", originWith("docker", "/opt/images/unrelated.raw"))
				return statePaths(t, hierarchy)
			},
			want: UnknownVersion,
		},
		{
			name:      "extensions file only records the component name",
			component: "docker",
			setup: func(t *testing.T) ActiveStatePaths {
				hierarchy := t.TempDir()
				writeMergedStateFile(t, hierarchy, "extensions", "\n  bitwarden  \ndocker\nvscode\n")
				return statePaths(t, hierarchy)
			},
			want: UnknownVersion,
		},
		{
			name:      "malformed origin falls back to extensions",
			component: "docker",
			setup: func(t *testing.T) ActiveStatePaths {
				hierarchy := t.TempDir()
				writeMergedStateFile(t, hierarchy, "origin", "{not json")
				writeMergedStateFile(t, hierarchy, "extensions", "docker\n")
				return statePaths(t, hierarchy)
			},
			want: UnknownVersion,
		},
		{
			name:      "parsed origin without the image still checks extensions",
			component: "docker",
			setup: func(t *testing.T) ActiveStatePaths {
				hierarchy := t.TempDir()
				writeMergedStateFile(t, hierarchy, "origin", originWith("dev", "/var/lib/extensions.d/dev_1.0.0.raw"))
				writeMergedStateFile(t, hierarchy, "extensions", "bitwarden\ndocker\nvscode\n")
				return statePaths(t, hierarchy)
			},
			want: UnknownVersion,
		},
		{
			name:      "parsed origin without the image and extensions without it",
			component: "docker",
			setup: func(t *testing.T) ActiveStatePaths {
				hierarchy := t.TempDir()
				writeMergedStateFile(t, hierarchy, "origin", originWith("dev", "/var/lib/extensions.d/dev_1.0.0.raw"))
				writeMergedStateFile(t, hierarchy, "extensions", "bitwarden\nvscode\n")
				return statePaths(t, hierarchy)
			},
			want: "",
		},
		{
			name:      "other merged images only",
			component: "docker",
			setup: func(t *testing.T) ActiveStatePaths {
				hierarchy := t.TempDir()
				writeMergedStateFile(t, hierarchy, "origin", originWith("dev", "/var/lib/extensions.d/dev_1.0.0.raw"))
				return statePaths(t, hierarchy)
			},
			want: "",
		},
		{
			name:      "no merged-state directory",
			component: "docker",
			setup: func(t *testing.T) ActiveStatePaths {
				return statePaths(t, t.TempDir())
			},
			want: "",
		},
		{
			name:      "second hierarchy records the image",
			component: "docker",
			setup: func(t *testing.T) ActiveStatePaths {
				first := t.TempDir()
				second := t.TempDir()
				writeMergedStateFile(t, second, "origin", originWith("docker", imagePath))
				return statePaths(t, first, second)
			},
			want: wantVersion,
		},
		{
			name:      "unreadable origin fails",
			component: "docker",
			setup: func(t *testing.T) ActiveStatePaths {
				hierarchy := t.TempDir()
				origin := writeMergedStateFile(t, hierarchy, "origin", originWith("docker", imagePath))
				if err := os.Chmod(origin, 0o000); err != nil {
					t.Fatalf("make origin unreadable: %v", err)
				}
				t.Cleanup(func() {
					if err := os.Chmod(origin, 0o644); err != nil {
						t.Errorf("restore origin permissions: %v", err)
					}
				})
				return statePaths(t, hierarchy)
			},
			wantErr:    true,
			skipAsRoot: true,
		},
		{
			name:      "runtime search directory still signals active",
			component: "docker",
			setup: func(t *testing.T) ActiveStatePaths {
				runExtensionsDir := t.TempDir()
				if err := os.WriteFile(filepath.Join(runExtensionsDir, image), []byte("image"), 0o644); err != nil {
					t.Fatalf("write runtime search image: %v", err)
				}
				return ActiveStatePaths{
					DefaultDir:       t.TempDir(),
					RunExtensionsDir: runExtensionsDir,
				}
			},
			want: wantVersion,
		},
		{
			name:           "legacy current symlink still signals active",
			component:      "docker",
			currentSymlink: "docker.raw",
			setup: func(t *testing.T) ActiveStatePaths {
				defaultDir := t.TempDir()
				if err := os.WriteFile(filepath.Join(defaultDir, image), []byte("image"), 0o644); err != nil {
					t.Fatalf("write staged image: %v", err)
				}
				if err := os.Symlink(image, filepath.Join(defaultDir, "docker.raw")); err != nil {
					t.Fatalf("create current symlink: %v", err)
				}
				return ActiveStatePaths{DefaultDir: defaultDir}
			},
			want: wantVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipAsRoot && os.Geteuid() == 0 {
				t.Skip("root bypasses file read permissions")
			}

			transfer := &config.Transfer{
				Component: tt.component,
				Target: config.TargetSection{
					MatchPattern:   pattern,
					CurrentSymlink: tt.currentSymlink,
				},
			}

			got, err := GetActiveVersionWith(transfer, tt.setup(t))
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetActiveVersionWith() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if err != nil && !strings.Contains(err.Error(), "read merged state") {
					t.Errorf("error = %v, want read merged state context", err)
				}
				return
			}
			if got != tt.want {
				t.Errorf("GetActiveVersionWith() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestGetActiveVersionWithEmptyRunExtensionsDir verifies that an empty runtime
// search directory skips that signal instead of reading the current directory.
func TestGetActiveVersionWithEmptyRunExtensionsDir(t *testing.T) {
	transfer := &config.Transfer{
		Target: config.TargetSection{MatchPattern: "myext_@v.raw"},
	}

	got, err := GetActiveVersionWith(transfer, ActiveStatePaths{DefaultDir: t.TempDir()})
	if err != nil {
		t.Fatalf("GetActiveVersionWith() error = %v", err)
	}
	if got != "" {
		t.Errorf("GetActiveVersionWith() = %q, want empty", got)
	}
}

// TestGetActiveVersionInIgnoresMergedState pins the compatibility wrapper to
// its documented behaviour: it does not read hierarchy metadata.
func TestGetActiveVersionInIgnoresMergedState(t *testing.T) {
	hierarchy := t.TempDir()
	writeMergedStateFile(t, hierarchy, "extensions", "myext\n")

	transfer := &config.Transfer{
		Component: "myext",
		Target:    config.TargetSection{MatchPattern: "myext_@v.raw"},
	}

	got, err := GetActiveVersionIn(transfer, t.TempDir(), filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("GetActiveVersionIn() error = %v", err)
	}
	if got != "" {
		t.Errorf("GetActiveVersionIn() = %q, want empty", got)
	}
}

// TestVacuumWithProtectsLinkedAndMergedVersions covers the ADR-0016
// retention contract: the vacuum keeps the linked version, the merged
// version, ProtectVersion=, explicit protected versions, the legacy
// CurrentSymlink target and the newest InstancesMax versions, and it deletes
// nothing when it cannot read the link or the merged state.
func TestVacuumWithProtectsLinkedAndMergedVersions(t *testing.T) {
	const pattern = "myext_@v.raw"

	// fixture is the on-disk state of one case.
	type fixture struct {
		stagingDir string
		paths      VacuumPaths
	}

	// stage writes one staged image per version and returns the fixture with
	// its (still empty) link directory and one hierarchy.
	stage := func(t *testing.T, versions ...string) fixture {
		t.Helper()
		stagingDir := t.TempDir()
		for _, v := range versions {
			if err := os.WriteFile(filepath.Join(stagingDir, "myext_"+v+".raw"), []byte("image"), 0o644); err != nil {
				t.Fatalf("write staged image: %v", err)
			}
		}
		return fixture{
			stagingDir: stagingDir,
			paths: VacuumPaths{
				DefaultDir:  stagingDir,
				LinkDir:     t.TempDir(),
				Hierarchies: []string{t.TempDir()},
			},
		}
	}
	linkTo := func(t *testing.T, f fixture, target string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(f.paths.LinkDir, "myext.raw")); err != nil {
			t.Fatalf("create sysext link: %v", err)
		}
	}
	linkVersion := func(t *testing.T, f fixture, v string) {
		t.Helper()
		linkTo(t, f, filepath.Join(f.stagingDir, "myext_"+v+".raw"))
	}
	merged := func(t *testing.T, f fixture, v string) {
		t.Helper()
		origin := `{"extensions":{"myext":{"path":"` + filepath.Join(f.stagingDir, "myext_"+v+".raw") + `"}}}`
		writeMergedStateFile(t, f.paths.Hierarchies[0], "origin", origin)
	}

	tests := []struct {
		name         string
		versions     []string
		instancesMax int
		protect      string
		setup        func(t *testing.T, f *fixture)
		wantKept     []string
		wantRemoved  []string
		wantErr      string
	}{
		{
			name:     "linked is the newest",
			versions: []string{"1.0.0", "2.0.0", "3.0.0"},
			setup: func(t *testing.T, f *fixture) {
				linkVersion(t, *f, "3.0.0")
			},
			wantKept:    []string{"3.0.0", "2.0.0"},
			wantRemoved: []string{"1.0.0"},
		},
		{
			name:     "merged is older than InstancesMax and is kept",
			versions: []string{"1.0.0", "2.0.0", "3.0.0", "4.0.0"},
			setup: func(t *testing.T, f *fixture) {
				linkVersion(t, *f, "4.0.0")
				merged(t, *f, "1.0.0")
			},
			wantKept:    []string{"4.0.0", "3.0.0", "1.0.0"},
			wantRemoved: []string{"2.0.0"},
		},
		{
			name:     "linked and merged differ: newest 2 and an older merged image make 3",
			versions: []string{"1.0.0", "2.0.0", "3.0.0"},
			setup: func(t *testing.T, f *fixture) {
				linkVersion(t, *f, "3.0.0")
				merged(t, *f, "1.0.0")
			},
			wantKept: []string{"3.0.0", "2.0.0", "1.0.0"},
		},
		{
			name:     "linked and merged both older than the newest are kept",
			versions: []string{"1.0.0", "2.0.0", "3.0.0", "4.0.0", "5.0.0"},
			setup: func(t *testing.T, f *fixture) {
				linkVersion(t, *f, "2.0.0")
				merged(t, *f, "1.0.0")
			},
			wantKept:    []string{"5.0.0", "4.0.0", "2.0.0", "1.0.0"},
			wantRemoved: []string{"3.0.0"},
		},
		{
			name:     "merged is absent",
			versions: []string{"1.0.0", "2.0.0", "3.0.0"},
			setup: func(t *testing.T, f *fixture) {
				linkVersion(t, *f, "3.0.0")
			},
			wantKept:    []string{"3.0.0", "2.0.0"},
			wantRemoved: []string{"1.0.0"},
		},
		{
			name:     "link is absent",
			versions: []string{"1.0.0", "2.0.0", "3.0.0"},
			setup: func(t *testing.T, f *fixture) {
				merged(t, *f, "1.0.0")
			},
			wantKept: []string{"3.0.0", "2.0.0", "1.0.0"},
		},
		{
			name:     "dangling link protects nothing",
			versions: []string{"1.0.0", "2.0.0", "3.0.0"},
			setup: func(t *testing.T, f *fixture) {
				// The link names version 1.0.0 but its target does not exist.
				linkTo(t, *f, filepath.Join(t.TempDir(), "gone", "myext_1.0.0.raw"))
			},
			wantKept:    []string{"3.0.0", "2.0.0"},
			wantRemoved: []string{"1.0.0"},
		},
		{
			name:     "merged version that is not staged takes no slot",
			versions: []string{"1.0.0", "2.0.0", "3.0.0"},
			setup: func(t *testing.T, f *fixture) {
				linkVersion(t, *f, "3.0.0")
				merged(t, *f, "9.0.0")
			},
			wantKept:    []string{"3.0.0", "2.0.0"},
			wantRemoved: []string{"1.0.0"},
		},
		{
			name:     "merged image with unknown version protects nothing",
			versions: []string{"1.0.0", "2.0.0", "3.0.0"},
			setup: func(t *testing.T, f *fixture) {
				linkVersion(t, *f, "3.0.0")
				writeMergedStateFile(t, f.paths.Hierarchies[0], "extensions", "myext\n")
			},
			wantKept:    []string{"3.0.0", "2.0.0"},
			wantRemoved: []string{"1.0.0"},
		},
		{
			name:         "ProtectVersion is kept",
			versions:     []string{"1.0.0", "2.0.0", "3.0.0"},
			instancesMax: 1,
			protect:      "1.0.0",
			setup: func(t *testing.T, f *fixture) {
				linkVersion(t, *f, "3.0.0")
			},
			wantKept:    []string{"3.0.0", "1.0.0"},
			wantRemoved: []string{"2.0.0"},
		},
		{
			name:         "extra protected versions are kept",
			versions:     []string{"1.0.0", "2.0.0", "3.0.0"},
			instancesMax: 1,
			setup: func(t *testing.T, f *fixture) {
				linkVersion(t, *f, "3.0.0")
				f.paths.ProtectVersions = []string{"", "2.0.0"}
			},
			wantKept:    []string{"3.0.0", "2.0.0"},
			wantRemoved: []string{"1.0.0"},
		},
		{
			name:         "InstancesMax 1 keeps only the newest, the linked and the merged image",
			versions:     []string{"1.0.0", "2.0.0", "3.0.0", "4.0.0"},
			instancesMax: 1,
			setup: func(t *testing.T, f *fixture) {
				linkVersion(t, *f, "4.0.0")
				merged(t, *f, "2.0.0")
			},
			wantKept:    []string{"4.0.0", "2.0.0"},
			wantRemoved: []string{"3.0.0", "1.0.0"},
		},
		{
			name:     "empty LinkDir and Hierarchies skip the protection",
			versions: []string{"1.0.0", "2.0.0", "3.0.0"},
			setup: func(t *testing.T, f *fixture) {
				linkVersion(t, *f, "1.0.0")
				merged(t, *f, "1.0.0")
				f.paths.LinkDir = ""
				f.paths.Hierarchies = nil
			},
			wantKept:    []string{"3.0.0", "2.0.0"},
			wantRemoved: []string{"1.0.0"},
		},
		{
			name:     "unreadable merged state fails and deletes nothing",
			versions: []string{"1.0.0", "2.0.0", "3.0.0"},
			setup: func(t *testing.T, f *fixture) {
				linkVersion(t, *f, "3.0.0")
				// A directory where the origin file belongs cannot be read.
				if err := os.MkdirAll(filepath.Join(f.paths.Hierarchies[0], MergedStateDirName, "origin"), 0o755); err != nil {
					t.Fatalf("create origin directory: %v", err)
				}
			},
			wantErr: "read merged version",
		},
		{
			name:     "unreadable link fails and deletes nothing",
			versions: []string{"1.0.0", "2.0.0", "3.0.0"},
			setup: func(t *testing.T, f *fixture) {
				// A regular file as the link directory makes Lstat fail with
				// ENOTDIR, not ENOENT.
				notDir := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(notDir, []byte("x"), 0o644); err != nil {
					t.Fatalf("write file: %v", err)
				}
				f.paths.LinkDir = notDir
			},
			wantErr: "read linked version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := stage(t, tt.versions...)
			tt.setup(t, &f)

			transfer := &config.Transfer{
				Component: "myext",
				Transfer: config.TransferSection{
					InstancesMax:   tt.instancesMax,
					ProtectVersion: tt.protect,
				},
				Target: config.TargetSection{Path: f.stagingDir, MatchPattern: pattern},
			}

			listStaged := func() []string {
				entries, err := os.ReadDir(f.stagingDir)
				if err != nil {
					t.Fatalf("read staging dir: %v", err)
				}
				var names []string
				for _, e := range entries {
					names = append(names, e.Name())
				}
				return names
			}
			before := listStaged()

			// The plan reports the same decision and removes nothing.
			planRemoved, planKept, planErr := PlanVacuumWith(transfer, "", f.paths)
			if got := listStaged(); !slices.Equal(got, before) {
				t.Fatalf("PlanVacuumWith changed the staging dir: %v -> %v", before, got)
			}

			removed, kept, err := VacuumWith(transfer, f.paths)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("VacuumWith() error = %v, want %q", err, tt.wantErr)
				}
				if planErr == nil {
					t.Error("PlanVacuumWith() error = nil, want the same failure")
				}
				if got := listStaged(); !slices.Equal(got, before) {
					t.Errorf("a failed vacuum changed the staging dir: %v -> %v", before, got)
				}
				return
			}
			if err != nil || planErr != nil {
				t.Fatalf("VacuumWith() error = %v, PlanVacuumWith() error = %v", err, planErr)
			}

			sortVersions := func(s []string) []string {
				s = slices.Clone(s)
				slices.Sort(s)
				return s
			}
			if got, want := sortVersions(kept), sortVersions(tt.wantKept); !slices.Equal(got, want) {
				t.Errorf("kept = %v, want %v", got, want)
			}
			if got, want := sortVersions(removed), sortVersions(tt.wantRemoved); !slices.Equal(got, want) {
				t.Errorf("removed = %v, want %v", got, want)
			}
			if !slices.Equal(sortVersions(planKept), sortVersions(kept)) || !slices.Equal(sortVersions(planRemoved), sortVersions(removed)) {
				t.Errorf("plan (%v kept, %v removed) differs from vacuum (%v kept, %v removed)", planKept, planRemoved, kept, removed)
			}

			// The files on disk match the report.
			for _, v := range tt.wantKept {
				if _, err := os.Stat(filepath.Join(f.stagingDir, "myext_"+v+".raw")); err != nil {
					t.Errorf("kept version %s is not on disk: %v", v, err)
				}
			}
			for _, v := range tt.wantRemoved {
				if _, err := os.Stat(filepath.Join(f.stagingDir, "myext_"+v+".raw")); !os.IsNotExist(err) {
					t.Errorf("removed version %s is still on disk (err = %v)", v, err)
				}
			}
		})
	}
}

// TestVacuumWithKeepsLegacyCurrentSymlinkTarget verifies the legacy
// CurrentSymlink still protects its target next to the new protections.
func TestVacuumWithKeepsLegacyCurrentSymlinkTarget(t *testing.T) {
	stagingDir := t.TempDir()
	for _, v := range []string{"1.0.0", "2.0.0", "3.0.0"} {
		if err := os.WriteFile(filepath.Join(stagingDir, "myext_"+v+".raw"), []byte("image"), 0o644); err != nil {
			t.Fatalf("write staged image: %v", err)
		}
	}
	if err := os.Symlink("myext_1.0.0.raw", filepath.Join(stagingDir, "myext.raw")); err != nil {
		t.Fatalf("create current symlink: %v", err)
	}

	transfer := &config.Transfer{
		Component: "myext",
		Transfer:  config.TransferSection{InstancesMax: 1},
		Target:    config.TargetSection{Path: stagingDir, MatchPattern: "myext_@v.raw", CurrentSymlink: "myext.raw"},
	}
	removed, kept, err := VacuumWith(transfer, VacuumPaths{DefaultDir: stagingDir, LinkDir: t.TempDir(), Hierarchies: []string{t.TempDir()}})
	if err != nil {
		t.Fatalf("VacuumWith() error = %v", err)
	}
	if len(removed) != 1 || removed[0] != "2.0.0" {
		t.Errorf("removed = %v, want [2.0.0]", removed)
	}
	if len(kept) != 2 {
		t.Errorf("kept = %v, want 3.0.0 and 1.0.0", kept)
	}
}

// TestMergedVersionIn pins that MergedVersionIn reads only the hierarchy
// metadata: a legacy CurrentSymlink and an image in the runtime search
// directory (both of which GetActiveVersionWith counts as active) do not make
// an image "merged now".
func TestMergedVersionIn(t *testing.T) {
	staging := t.TempDir()
	tr := linkTransfer(staging)
	tr.Target.CurrentSymlink = "myext.raw"
	stageImages(t, staging, "myext_1.raw", "myext_2.raw")
	if err := os.Symlink(filepath.Join(staging, "myext_2.raw"), filepath.Join(staging, "myext.raw")); err != nil {
		t.Fatal(err)
	}

	empty := t.TempDir()
	if got, err := MergedVersionIn(tr, []string{empty}); err != nil || got != "" {
		t.Errorf("no metadata: got %q, %v; want empty (the legacy symlink is not merged state)", got, err)
	}
	if got, err := MergedVersionIn(tr, nil); err != nil || got != "" {
		t.Errorf("no hierarchies: got %q, %v", got, err)
	}

	first, second := t.TempDir(), t.TempDir()
	writeMergedStateFile(t, second, "origin", `{"extensions":{"myext":{"path":"`+filepath.Join(staging, "myext_1.raw")+`"}}}`)
	if got, err := MergedVersionIn(tr, []string{first, second}); err != nil || got != "1" {
		t.Errorf("origin in the second hierarchy: got %q, %v; want 1", got, err)
	}

	names := t.TempDir()
	writeMergedStateFile(t, names, "extensions", "myext\n")
	if got, err := MergedVersionIn(tr, []string{names}); err != nil || got != UnknownVersion {
		t.Errorf("name list only: got %q, %v; want %q", got, err, UnknownVersion)
	}

	bad := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bad, MergedStateDirName, "origin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := MergedVersionIn(tr, []string{bad}); err == nil || !strings.Contains(err.Error(), "read merged state") {
		t.Errorf("unreadable metadata: err = %v, want read merged state", err)
	}

	broken := linkTransfer(staging)
	broken.Target.MatchPattern = ""
	if _, err := MergedVersionIn(broken, []string{empty}); err == nil {
		t.Error("a transfer without a target pattern must fail")
	}
}
