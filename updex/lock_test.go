package updex

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/frostyard/updex/v2/sysext"
	"github.com/frostyard/updex/v2/systemd"
)

// newLockTestClient returns a client whose operations all take lockPath.
func newLockTestClient(t *testing.T, lockPath string) *Client {
	t.Helper()
	root := t.TempDir()
	return NewClient(ClientConfig{
		SysextRunner:   &sysext.MockRunner{},
		SystemdManager: systemd.NewTestManager(t.TempDir(), &systemd.MockSystemctlRunner{}),
		Paths: RuntimePaths{
			DefinitionRoots:    []string{root},
			CatalogConfigRoots: []string{t.TempDir()},
			CatalogCacheDir:    DisableCatalogCache,
			SysextLinkDir:      t.TempDir(),
			RunExtensionsDir:   t.TempDir(),
			SysextHierarchies:  []string{t.TempDir()},
			LockPath:           lockPath,
			RollbackDir:        filepath.Join(t.TempDir(), "rollback"),
		},
	})
}

// Two clients on the same lock path: while one holds the lock, every
// mutating operation of the other returns ErrBusy and changes nothing; after
// the release, it works.
func TestMutatingOperationsReturnErrBusyWhileLockHeld(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "run", "updex", "lock")
	holder := newLockTestClient(t, lockPath)
	other := newLockTestClient(t, lockPath)

	release, err := holder.lockMutation(false)
	if err != nil {
		t.Fatalf("client 1 lock: %v", err)
	}

	ops := map[string]func() error{
		"UpdateFeatures": func() error {
			_, err := other.UpdateFeatures(t.Context(), UpdateFeaturesOptions{})
			return err
		},
		"EnableFeature": func() error {
			_, err := other.EnableFeature(t.Context(), "x", EnableFeatureOptions{})
			return err
		},
		"DisableFeature": func() error {
			_, err := other.DisableFeature(t.Context(), "x", DisableFeatureOptions{})
			return err
		},
		"RollbackFeature": func() error {
			_, err := other.RollbackFeature(t.Context(), "x", RollbackFeatureOptions{})
			return err
		},
		"CatalogAdd": func() error {
			_, err := other.CatalogAdd(t.Context(), "x", CatalogAddOptions{})
			return err
		},
		"CatalogRemove": func() error {
			_, err := other.CatalogRemove(t.Context(), "x", CatalogRemoveOptions{})
			return err
		},
		"EnableDaemon": func() error {
			_, err := other.EnableDaemon(t.Context(), EnableDaemonOptions{})
			return err
		},
		"DisableDaemon": func() error {
			_, err := other.DisableDaemon(t.Context(), DisableDaemonOptions{})
			return err
		},
	}
	for name, op := range ops {
		if err := op(); !errors.Is(err, ErrBusy) {
			t.Errorf("%s while the lock is held: error = %v, want ErrBusy", name, err)
		}
	}

	// Dry runs and read-only operations never take the lock.
	readOnly := map[string]func() error{
		"UpdateFeatures dry run": func() error {
			_, err := other.UpdateFeatures(t.Context(), UpdateFeaturesOptions{DryRun: true})
			return err
		},
		"EnableFeature dry run": func() error {
			// The feature does not exist: the error is not ErrBusy.
			_, err := other.EnableFeature(t.Context(), "x", EnableFeatureOptions{DryRun: true})
			if err == nil || errors.Is(err, ErrBusy) {
				t.Errorf("EnableFeature dry run error = %v, want a not-found error", err)
			}
			return nil
		},
		"DisableDaemon dry run": func() error {
			_, err := other.DisableDaemon(t.Context(), DisableDaemonOptions{DryRun: true})
			if errors.Is(err, ErrBusy) {
				t.Errorf("DisableDaemon dry run error = %v, want no lock", err)
			}
			return nil
		},
		"Features": func() error {
			_, err := other.Features(t.Context())
			return err
		},
		"CheckFeatures": func() error {
			_, err := other.CheckFeatures(t.Context(), CheckFeaturesOptions{})
			return err
		},
		"Components": func() error {
			_, err := other.Components(t.Context())
			return err
		},
		"CatalogList": func() error {
			_, err := other.CatalogList(t.Context(), CatalogListOptions{})
			if errors.Is(err, ErrBusy) {
				t.Errorf("CatalogList error = %v, want no lock", err)
			}
			return nil
		},
		"DaemonStatus": func() error {
			_, err := other.DaemonStatus(t.Context(), DaemonStatusOptions{})
			return err
		},
	}
	for name, op := range readOnly {
		if err := op(); err != nil {
			t.Errorf("%s while the lock is held: %v", name, err)
		}
	}

	release()
	release() // releasing twice is harmless

	if _, err := other.UpdateFeatures(t.Context(), UpdateFeaturesOptions{}); err != nil {
		t.Fatalf("UpdateFeatures after the release: %v", err)
	}
	// The operation released the lock when it returned.
	again, err := holder.lockMutation(false)
	if err != nil {
		t.Fatalf("lock after UpdateFeatures returned: %v", err)
	}
	again()
}

// The lock file and its parent directory are created with the documented
// modes, and the default path is /run/updex/lock.
func TestAcquireLockCreatesFileAndDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "updex", "lock")
	release, err := acquireLock(path)
	if err != nil {
		t.Fatalf("acquireLock: %v", err)
	}
	defer release()

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode().Perm() != 0o755 {
		t.Errorf("lock directory: %v, %v; want a 0755 directory", dirInfo, err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Errorf("lock file: %v, %v; want a 0600 regular file", info, err)
	}

	if DefaultLockPath != "/run/updex/lock" {
		t.Errorf("DefaultLockPath = %q, want /run/updex/lock", DefaultLockPath)
	}
}

// A second acquire on the same path fails with ErrBusy and the lock is
// usable again after a release, also from another process-level open.
func TestAcquireLockIsExclusiveAndNonBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	release, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireLock(path); !errors.Is(err, ErrBusy) {
		t.Fatalf("second acquireLock error = %v, want ErrBusy", err)
	}
	release()
	release2, err := acquireLock(path)
	if err != nil {
		t.Fatalf("acquireLock after release: %v", err)
	}
	release2()
}

// A lock path that is a symlink (dangling or live), a directory, or a
// non-regular file is refused, and nothing is created through the link.
func TestAcquireLockRefusesUnsafePaths(t *testing.T) {
	t.Run("dangling symlink", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "lock")
		victim := filepath.Join(dir, "victim")
		if err := os.Symlink(victim, path); err != nil {
			t.Fatal(err)
		}
		if _, err := acquireLock(path); err == nil || errors.Is(err, ErrBusy) {
			t.Fatalf("acquireLock error = %v, want a refusal", err)
		}
		if _, err := os.Lstat(victim); !os.IsNotExist(err) {
			t.Errorf("the link target was created (err = %v)", err)
		}
	})

	t.Run("symlink to a regular file", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "lock")
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if _, err := acquireLock(path); err == nil || errors.Is(err, ErrBusy) {
			t.Fatalf("acquireLock error = %v, want a refusal", err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "lock")
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := acquireLock(path); err == nil || errors.Is(err, ErrBusy) {
			t.Fatalf("acquireLock error = %v, want a refusal", err)
		}
	})

	t.Run("fifo", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "lock")
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := acquireLock(path); err == nil || errors.Is(err, ErrBusy) {
			t.Fatalf("acquireLock error = %v, want a refusal", err)
		}
	})

	t.Run("parent is a symlink", func(t *testing.T) {
		dir := t.TempDir()
		real := filepath.Join(dir, "real")
		if err := os.Mkdir(real, 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		if _, err := acquireLock(filepath.Join(link, "lock")); err == nil {
			t.Fatal("acquireLock through a symlinked directory must be refused")
		}
	})

	t.Run("through a client operation", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "lock")
		if err := os.Symlink(filepath.Join(dir, "victim"), path); err != nil {
			t.Fatal(err)
		}
		client := newLockTestClient(t, path)
		if _, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{}); err == nil || errors.Is(err, ErrBusy) {
			t.Fatalf("UpdateFeatures error = %v, want a refusal of the lock path", err)
		}
	})
}

// A zero RuntimePaths.LockPath resolves once, at construction, to the package
// default; a set one is kept.
func TestResolveRuntimePathsLockPath(t *testing.T) {
	if got := resolveRuntimePaths(RuntimePaths{}).lockPath; got != lockPathDefault {
		t.Errorf("zero LockPath resolved to %q, want %q", got, lockPathDefault)
	}
	if got := resolveRuntimePaths(RuntimePaths{LockPath: "/x/lock"}).lockPath; got != "/x/lock" {
		t.Errorf("LockPath resolved to %q, want /x/lock", got)
	}
	// Tests must never default to the production path.
	if lockPathDefault == DefaultLockPath {
		t.Errorf("tests use the production lock path %q", DefaultLockPath)
	}
}
