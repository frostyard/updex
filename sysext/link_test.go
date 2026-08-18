package sysext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/updex/config"
)

// linkTransfer returns a transfer whose staging directory is stagingDir and
// whose images match myext_@v.raw. Tests mutate the fields they need.
func linkTransfer(stagingDir string) *config.Transfer {
	return &config.Transfer{
		Component: "myext",
		Target: config.TargetSection{
			Path:         stagingDir,
			MatchPattern: "myext_@v.raw",
		},
	}
}

func stageImages(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("image "+name), 0644); err != nil {
			t.Fatalf("failed to stage %s: %v", name, err)
		}
	}
}

func readLink(t *testing.T, path string) string {
	t.Helper()
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("expected %s to be a symlink: %v", path, err)
	}
	return target
}

// TestLinkToSysextAt covers the lifecycle of the /var/lib/extensions link:
// it is the gate that makes a staged image visible to `systemd-sysext
// refresh`, so every branch that could leave a stale link, hide a download,
// or clobber unexpected filesystem state is pinned here. All cases run
// rootless against temporary staging and sysext directories through the
// explicit-directory entry point, never the package-global SysextDir.
func TestLinkToSysextAt(t *testing.T) {
	tests := []struct {
		name string
		// staged files are written to the staging dir before linking.
		staged []string
		// setup runs after staging and may adjust the transfer, staging
		// dir, or sysext dir (e.g. pre-create a conflicting destination).
		setup func(t *testing.T, tr *config.Transfer, stagingDir, sysextDir string)
		// wantErr is a substring the error must contain; empty means the
		// call must succeed.
		wantErr string
		// wantTarget is the expected link target basename on success.
		wantTarget string
		// check runs after the call for extra assertions.
		check func(t *testing.T, stagingDir, sysextDir string)
		// sysextSubdir, when set, links into <sysextDir>/<sysextSubdir>
		// instead of sysextDir itself (after setup has run).
		sysextSubdir string
		// keepLinkOnErr exempts a case from the "a failed link leaves no
		// symlink" invariant because a pre-existing link is expected to
		// survive.
		keepLinkOnErr bool
	}{
		{
			name:       "selects the newest staged image by version, not directory order",
			staged:     []string{"myext_1.9.0.raw", "myext_1.10.0.raw", "myext_1.2.0.raw"},
			wantTarget: "myext_1.10.0.raw",
		},
		{
			name:   "creates the sysext directory when it does not exist yet",
			staged: []string{"myext_1.0.0.raw"},
			setup: func(t *testing.T, _ *config.Transfer, _, sysextDir string) {
				if err := os.RemoveAll(sysextDir); err != nil {
					t.Fatal(err)
				}
			},
			wantTarget: "myext_1.0.0.raw",
		},
		{
			name:   "replaces an existing symlink that points at an older image",
			staged: []string{"myext_1.0.0.raw", "myext_2.0.0.raw"},
			setup: func(t *testing.T, _ *config.Transfer, stagingDir, sysextDir string) {
				if err := os.Symlink(filepath.Join(stagingDir, "myext_1.0.0.raw"), filepath.Join(sysextDir, "myext.raw")); err != nil {
					t.Fatal(err)
				}
			},
			wantTarget: "myext_2.0.0.raw",
			check: func(t *testing.T, stagingDir, _ string) {
				// Replacing the link must not touch the image it pointed at.
				if _, err := os.Stat(filepath.Join(stagingDir, "myext_1.0.0.raw")); err != nil {
					t.Errorf("old image removed while replacing link: %v", err)
				}
			},
		},
		{
			name:   "replaces a dangling symlink",
			staged: []string{"myext_1.0.0.raw"},
			setup: func(t *testing.T, _ *config.Transfer, _, sysextDir string) {
				if err := os.Symlink("/nonexistent/myext_0.1.0.raw", filepath.Join(sysextDir, "myext.raw")); err != nil {
					t.Fatal(err)
				}
			},
			wantTarget: "myext_1.0.0.raw",
		},
		{
			name:   "replaces an existing regular file at the link path",
			staged: []string{"myext_1.0.0.raw"},
			setup: func(t *testing.T, _ *config.Transfer, _, sysextDir string) {
				if err := os.WriteFile(filepath.Join(sysextDir, "myext.raw"), []byte("a real file, not a link"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantTarget: "myext_1.0.0.raw",
		},
		{
			name:   "ignores symlinks in the staging directory when choosing the image",
			staged: []string{"myext_1.0.0.raw"},
			setup: func(t *testing.T, _ *config.Transfer, stagingDir, _ string) {
				// A legacy CurrentSymlink named like a newer version must
				// not be selected as the link target.
				if err := os.Symlink("myext_1.0.0.raw", filepath.Join(stagingDir, "myext_9.9.9.raw")); err != nil {
					t.Fatal(err)
				}
			},
			wantTarget: "myext_1.0.0.raw",
		},
		{
			name:   "falls back to the sysext directory when Target.Path is empty",
			staged: nil,
			setup: func(t *testing.T, tr *config.Transfer, _, sysextDir string) {
				tr.Target.Path = ""
				stageImages(t, sysextDir, "myext_3.0.0.raw")
			},
			wantTarget: "myext_3.0.0.raw",
		},
		{
			name:   "rejects a transfer without a component",
			staged: []string{"myext_1.0.0.raw"},
			setup: func(_ *testing.T, tr *config.Transfer, _, _ string) {
				tr.Component = ""
			},
			wantErr: "cannot determine sysext link name",
		},
		{
			name:   "rejects a transfer without target patterns",
			staged: []string{"myext_1.0.0.raw"},
			setup: func(_ *testing.T, tr *config.Transfer, _, _ string) {
				tr.Target.MatchPattern = ""
			},
			wantErr: "cannot determine sysext link name",
		},
		{
			name:   "rejects a target pattern without a version placeholder",
			staged: []string{"myext_1.0.0.raw"},
			setup: func(_ *testing.T, tr *config.Transfer, _, _ string) {
				tr.Target.MatchPattern = "myext.raw"
			},
			wantErr: "invalid target pattern",
		},
		{
			name:    "rejects an empty staging directory",
			staged:  nil,
			wantErr: "no installed versions found for myext",
		},
		{
			name:    "rejects a staging directory with no matching images",
			staged:  []string{"other_1.0.0.raw", "myext.raw", "notes.txt"},
			wantErr: "no installed versions found for myext",
		},
		{
			name:   "rejects a missing staging directory",
			staged: nil,
			setup: func(t *testing.T, tr *config.Transfer, _, _ string) {
				tr.Target.Path = filepath.Join(t.TempDir(), "never-created")
			},
			wantErr: "no installed versions found for myext",
		},
		{
			name:   "fails when the sysext directory cannot be created",
			staged: []string{"myext_1.0.0.raw"},
			setup: func(t *testing.T, _ *config.Transfer, _, sysextDir string) {
				// Turn the sysext path into a child of a regular file so
				// MkdirAll fails with ENOTDIR regardless of privileges.
				if err := os.RemoveAll(sysextDir); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(sysextDir, []byte("not a directory"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			sysextSubdir: "extensions",
			wantErr:      "failed to create sysext directory",
		},
		{
			name:   "fails and keeps the old link when it cannot be removed",
			staged: []string{"myext_1.0.0.raw", "myext_2.0.0.raw"},
			setup: func(t *testing.T, _ *config.Transfer, stagingDir, sysextDir string) {
				if os.Geteuid() == 0 {
					t.Skip("root bypasses directory write permissions")
				}
				old := filepath.Join(stagingDir, "myext_1.0.0.raw")
				if err := os.Symlink(old, filepath.Join(sysextDir, "myext.raw")); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(sysextDir, 0555); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(sysextDir, 0755) })
			},
			wantErr: "failed to remove existing",
			check: func(t *testing.T, stagingDir, sysextDir string) {
				want := filepath.Join(stagingDir, "myext_1.0.0.raw")
				if got := readLink(t, filepath.Join(sysextDir, "myext.raw")); got != want {
					t.Errorf("old link must survive a failed replacement, got %q", got)
				}
			},
			keepLinkOnErr: true,
		},
		{
			name:   "preserves a conflicting destination directory and returns an error",
			staged: []string{"myext_1.0.0.raw"},
			setup: func(t *testing.T, _ *config.Transfer, _, sysextDir string) {
				conflict := filepath.Join(sysextDir, "myext.raw")
				if err := os.MkdirAll(conflict, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(conflict, "keep-me"), []byte("operator data"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "failed to create symlink",
			check: func(t *testing.T, _, sysextDir string) {
				conflict := filepath.Join(sysextDir, "myext.raw")
				info, err := os.Lstat(conflict)
				if err != nil || !info.IsDir() {
					t.Fatalf("conflicting directory must be preserved, got info=%v err=%v", info, err)
				}
				if _, err := os.Stat(filepath.Join(conflict, "keep-me")); err != nil {
					t.Errorf("contents of conflicting directory must be preserved: %v", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stagingDir := t.TempDir()
			sysextDir := t.TempDir()
			tr := linkTransfer(stagingDir)
			stageImages(t, stagingDir, tc.staged...)
			if tc.setup != nil {
				tc.setup(t, tr, stagingDir, sysextDir)
			}

			if tc.sysextSubdir != "" {
				sysextDir = filepath.Join(sysextDir, tc.sysextSubdir)
			}
			err := LinkToSysextAt(tr, sysextDir)

			linkPath := filepath.Join(sysextDir, "myext.raw")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("LinkToSysextAt() error = %v, want substring %q", err, tc.wantErr)
				}
				// A failed link must never leave a new symlink behind.
				if info, statErr := os.Lstat(linkPath); !tc.keepLinkOnErr && statErr == nil && info.Mode()&os.ModeSymlink != 0 {
					t.Errorf("failed LinkToSysextAt left a symlink at %s", linkPath)
				}
			} else {
				if err != nil {
					t.Fatalf("LinkToSysextAt() error = %v", err)
				}
				stagedDir := tr.Target.Path
				if stagedDir == "" {
					stagedDir = sysextDir
				}
				want := filepath.Join(stagedDir, tc.wantTarget)
				if got := readLink(t, linkPath); got != want {
					t.Errorf("link target = %q, want %q", got, want)
				}
				// The link must resolve to an existing regular file.
				if info, err := os.Stat(linkPath); err != nil || !info.Mode().IsRegular() {
					t.Errorf("link does not resolve to a regular file: info=%v err=%v", info, err)
				}
			}
			if tc.check != nil {
				tc.check(t, stagingDir, sysextDir)
			}
		})
	}
}

// TestLinkToSysextAt_Relink pins that linking twice as newer images land
// always moves the link forward and never accumulates extra entries in the
// sysext directory.
func TestLinkToSysextAt_Relink(t *testing.T) {
	stagingDir := t.TempDir()
	sysextDir := t.TempDir()
	tr := linkTransfer(stagingDir)

	stageImages(t, stagingDir, "myext_1.0.0.raw")
	if err := LinkToSysextAt(tr, sysextDir); err != nil {
		t.Fatalf("first link: %v", err)
	}
	if got := readLink(t, filepath.Join(sysextDir, "myext.raw")); filepath.Base(got) != "myext_1.0.0.raw" {
		t.Fatalf("first link target = %q", got)
	}

	stageImages(t, stagingDir, "myext_2.0.0.raw")
	if err := LinkToSysextAt(tr, sysextDir); err != nil {
		t.Fatalf("second link: %v", err)
	}
	if got := readLink(t, filepath.Join(sysextDir, "myext.raw")); filepath.Base(got) != "myext_2.0.0.raw" {
		t.Fatalf("second link target = %q", got)
	}

	entries, err := os.ReadDir(sysextDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "myext.raw" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("sysext dir should hold exactly the one link, got %v", names)
	}
}

// TestDefaultRunnerLinksAtExplicitDir pins that the production runner
// satisfies PathSysextRunner — the interface the SDK client type-asserts to
// route links into its captured SysextLinkDir — and that it delegates to
// LinkToSysextAt rather than the package-global directory.
func TestDefaultRunnerLinksAtExplicitDir(t *testing.T) {
	stagingDir := t.TempDir()
	sysextDir := t.TempDir()
	untouched := t.TempDir()
	oldSysextDir := SysextDir
	SysextDir = untouched
	t.Cleanup(func() { SysextDir = oldSysextDir })

	stageImages(t, stagingDir, "myext_1.0.0.raw")

	var runner SysextRunner = &DefaultRunner{}
	pathRunner, ok := runner.(PathSysextRunner)
	if !ok {
		t.Fatal("DefaultRunner must implement PathSysextRunner")
	}
	if err := pathRunner.LinkToSysextAt(linkTransfer(stagingDir), sysextDir); err != nil {
		t.Fatalf("LinkToSysextAt() error = %v", err)
	}
	if got := readLink(t, filepath.Join(sysextDir, "myext.raw")); filepath.Base(got) != "myext_1.0.0.raw" {
		t.Errorf("link target = %q", got)
	}
	if entries, _ := os.ReadDir(untouched); len(entries) != 0 {
		t.Errorf("package-global SysextDir must not be written when an explicit dir is given, got %d entries", len(entries))
	}
}
