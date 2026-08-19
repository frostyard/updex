package updex

import (
	"fmt"
	"os"
	"path/filepath"
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

// writeManagedFile writes content to path as a fresh regular file, mode
// 0644, via a temporary file in the same directory plus rename (the same
// shape as systemd.writeUnitFile). The write itself therefore never
// follows a symlink that appeared at path between the managedFileExists
// check and the write: rename replaces the directory entry rather than
// opening whatever it points at, and a failure part-way leaves no
// truncated file and no temp debris behind.
func writeManagedFile(path, content string) error {
	return writeManagedFileBytes(path, []byte(content), 0644)
}

// writeManagedFileBytes is writeManagedFile for byte content with an explicit
// permission mode — the one temp-file-plus-rename write every managed
// definition path shares (drop-ins, catalog transfer and feature files, and
// the snapshot restore that puts a previous definition back with its
// captured mode).
func writeManagedFileBytes(path string, content []byte, mode os.FileMode) (err error) {
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
	if err = tmp.Chmod(mode); err != nil {
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
