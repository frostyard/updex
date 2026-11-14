package updex

import (
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

// mkdirAllMode creates dir and every missing parent, like os.MkdirAll, and
// gives each directory it creates the mode mode.Perm(). The result therefore
// does not depend on the process umask: the lock, rollback and last-run
// directories are documented as 0755 so that a read-only command can read
// what a root-privileged run wrote under a stricter umask (issue #524). A
// directory that already exists keeps its mode.
//
// The missing components are collected first and fixed afterwards, top-down
// through an O_NOFOLLOW descriptor of each directory: os.MkdirAll applies the
// process umask, and a chmod by path would follow a symlink planted in
// between the mkdir and the chmod.
func mkdirAllMode(dir string, mode os.FileMode) error {
	clean := filepath.Clean(dir)
	var missing []string
	for p := clean; ; {
		_, err := os.Lstat(p)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("check directory %s: %w", p, err)
		}
		missing = append(missing, p)
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	if err := os.MkdirAll(clean, mode); err != nil {
		return err
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := chmodDirNoFollow(missing[i], mode.Perm()); err != nil {
			return err
		}
	}
	return nil
}

// chmodDirNoFollow gives the directory at path the permission bits mode. It
// opens the directory with O_NOFOLLOW and O_DIRECTORY, so a symlink or a
// non-directory at path is refused instead of chmodded through, and sets the
// mode on the open descriptor.
func chmodDirNoFollow(path string, mode os.FileMode) error {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open directory %s: %w", path, err)
	}
	dir := os.NewFile(uintptr(fd), path)
	defer func() { _ = dir.Close() }()
	if err := dir.Chmod(mode); err != nil {
		return fmt.Errorf("set mode of directory %s: %w", path, err)
	}
	return nil
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
