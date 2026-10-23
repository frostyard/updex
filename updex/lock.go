package updex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// DefaultLockPath is the lock file every mutating operation holds while it
// runs, unless RuntimePaths.LockPath names another one. /run is cleared at
// every boot, so a stale lock never survives a reboot. See ADR-0016.
const DefaultLockPath = "/run/updex/lock"

// lockPathDefault is the value resolveRuntimePaths uses for a zero
// RuntimePaths.LockPath. Production code never assigns it: it exists so the
// package's own tests can point every default-constructed client at a
// temporary file in TestMain, and no test touches /run/updex.
var lockPathDefault = DefaultLockPath

// ErrBusy reports that another updex operation holds the lock file, so this
// operation did not start and changed nothing. updex never waits for the
// lock. Callers can test for it with errors.Is; a scheduled run can treat it
// as "try again at the next run".
var ErrBusy = errors.New("another updex operation is running")

// acquireLock takes the exclusive, non-blocking flock on path, creating the
// file (mode 0600) and its parent directory (mode 0755) when they are
// missing. It returns a release function that drops the lock; release is
// safe to call more than once. The lock is also dropped when the process
// exits, so a crash never leaves a stale lock.
//
// When another open file description holds the lock, it returns an error
// that wraps ErrBusy. A path that is a symlink or is not a regular file is
// refused (ADR-0005): updex runs as root, and the open uses O_NOFOLLOW so a
// link planted after the check is not followed either.
func acquireLock(path string) (release func(), err error) {
	dir := filepath.Dir(path)
	switch info, err := os.Lstat(dir); {
	case err == nil && info.IsDir():
		// present and real: nothing to create
	case err == nil:
		return nil, fmt.Errorf("lock directory %s exists and is not a directory (mode %s); remove it manually", dir, info.Mode().Type())
	case os.IsNotExist(err):
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create lock directory: %w", err)
		}
	default:
		return nil, fmt.Errorf("check lock directory: %w", err)
	}

	switch info, err := os.Lstat(path); {
	case err == nil && !info.Mode().IsRegular():
		return nil, fmt.Errorf("lock file %s is not a regular file (mode %s); remove it manually", path, info.Mode().Type())
	case err != nil && !os.IsNotExist(err):
		return nil, fmt.Errorf("check lock file: %w", err)
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	// The Lstat above and the open are not atomic; check what was opened.
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("check lock file: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("lock file %s is not a regular file (mode %s)", path, info.Mode().Type())
	}

	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w (lock file %s)", ErrBusy, path)
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}

	released := false
	return func() {
		if released {
			return
		}
		released = true
		// Closing the descriptor drops the flock; the file stays, because
		// removing it would let two operations lock different files.
		_ = f.Close()
	}, nil
}

// lockMutation takes the client's operation lock for a mutating operation.
// A dry run changes nothing, so it takes no lock and gets a no-op release.
func (c *Client) lockMutation(dryRun bool) (release func(), err error) {
	if dryRun {
		return func() {}, nil
	}
	return acquireLock(c.paths.lockPath)
}
