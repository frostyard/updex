package updex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// This file holds the ADR-0005 filesystem guards shared by every SDK path
// that writes as root under a managed definition directory: Lstat-only
// existence checks that refuse to write through non-regular entries, and a
// temp-file-plus-rename write that never follows a symlink planted between
// the check and the write. See docs/adr/0005-transactional-writes-lstat-checks.md.

// managedFileExists reports whether a regular file exists at path, which
// is the only thing updex generates or deletes at a managed definition
// path.
//
// It uses Lstat, never Stat: a symlink must be seen as itself rather than
// followed. A dangling link would otherwise stat as absent, skipping the
// ownership check, and the subsequent os.WriteFile would follow it and
// create the target wherever it points — a privileged write outside the
// component directory. Anything present that is not a regular file is an
// error so the operator resolves it deliberately.
//
// A stat failure for any reason other than absence is likewise an error
// rather than "not there": reading an unreadable path as absent would
// skip a safety check or under-report what was left behind.
func managedFileExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s is not a regular file (mode %s); updex manages definitions in place and will not write through it", path, info.Mode().Type())
	}
	return true, nil
}

// mkdirAllModeStep runs after mkdirAllMode created and chmodded one
// directory, with that directory's full path. It is nil in production: a test
// sets it to replace an ancestor path in the middle of the walk and check
// that the next step is not redirected, which is what the descriptor-relative
// walk in mkdirAllMode is for (issue #524 review, updex/fsguard_test.go).
var mkdirAllModeStep func(path string)

// mkdirAllMode creates dir and every missing parent, like os.MkdirAll, and
// gives each directory it creates the mode mode.Perm(). The result therefore
// does not depend on the process umask: the lock, rollback and last-run
// directories are documented as 0755 so that a read-only command can read
// what a root-privileged run wrote under a stricter umask (issue #524). A
// directory that already exists keeps its mode.
//
// Every step is bound to the descriptor of the directory it created, not to a
// path: each new directory is made with mkdirat inside the descriptor of its
// parent and chmodded through an O_NOFOLLOW descriptor opened with openat, so
// replacing an ancestor path after it was created — with a symlink, say —
// cannot redirect a later mkdir or chmod at an unrelated directory. The
// deepest existing ancestor is opened by path, as os.MkdirAll resolves it.
func mkdirAllMode(dir string, mode os.FileMode) error {
	clean := filepath.Clean(dir)
	var missing []string // collected deepest first
	base := clean
	for p := clean; ; {
		if _, err := os.Lstat(p); err == nil {
			base = p
			break
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check directory %s: %w", p, err)
		}
		missing = append(missing, filepath.Base(p))
		parent := filepath.Dir(p)
		if parent == p {
			return fmt.Errorf("check directory %s: no existing ancestor", p)
		}
		p = parent
	}
	if len(missing) == 0 {
		return nil
	}

	parentFd, err := syscall.Open(base, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open directory %s: %w", base, err)
	}
	path := base
	for i := len(missing) - 1; i >= 0; i-- {
		path = filepath.Join(path, missing[i])
		childFd, err := mkdirAtMode(parentFd, missing[i], mode.Perm(), path)
		if err != nil {
			_ = syscall.Close(parentFd)
			return err
		}
		if err := syscall.Close(parentFd); err != nil {
			_ = syscall.Close(childFd)
			return fmt.Errorf("close directory %s: %w", filepath.Dir(path), err)
		}
		parentFd = childFd
		if mkdirAllModeStep != nil {
			mkdirAllModeStep(path)
		}
	}
	if err := syscall.Close(parentFd); err != nil {
		return fmt.Errorf("close directory %s: %w", path, err)
	}
	return nil
}

// mkdirAtMode creates name inside the directory whose descriptor is parentFd,
// sets the new directory to mode (mkdir applies the process umask, so the
// chmod is what makes the mode explicit), and returns an O_NOFOLLOW
// descriptor of it. A name another process created first (EEXIST) is used
// without changing its mode: an existing directory keeps its mode, and a
// symlink or non-directory there is refused rather than followed. path is the
// full path of the new directory, for error messages only.
func mkdirAtMode(parentFd int, name string, mode os.FileMode, path string) (int, error) {
	err := syscall.Mkdirat(parentFd, name, uint32(mode))
	created := err == nil
	if err != nil && !errors.Is(err, syscall.EEXIST) {
		return -1, fmt.Errorf("create directory %s: %w", path, err)
	}
	fd, err := syscall.Openat(parentFd, name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open directory %s: %w", path, err)
	}
	if !created {
		return fd, nil
	}
	if err := syscall.Fchmod(fd, uint32(mode)); err != nil {
		_ = syscall.Close(fd)
		return -1, fmt.Errorf("set mode of directory %s: %w", path, err)
	}
	return fd, nil
}

// writeManagedFile writes content to path as a fresh regular file, mode
// 0644, via a temporary file in the same directory plus rename (the same
// shape as systemd.writeUnitFile). The write itself therefore never
// follows a symlink that appeared at path between the managedFileExists
// check and the write: rename replaces the directory entry rather than
// opening whatever it points at, and a failure part-way leaves no
// truncated file and no temp debris behind.
func writeManagedFile(path, content string) error {
	return writeManagedFileWithMode(path, []byte(content), 0644)
}

// writeManagedFileWithMode is the mode-preserving form used when rollback
// restores a captured managed file.
func writeManagedFileWithMode(path string, content []byte, mode os.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err = tmp.Write(content); err != nil {
		return err
	}
	if err = tmp.Chmod(mode.Perm()); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
