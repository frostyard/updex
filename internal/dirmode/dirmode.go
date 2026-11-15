// Package dirmode creates directories with an explicit mode that does not
// depend on the process umask.
//
// updex documents the mode of every directory it creates as root under /etc,
// /var/lib and /run: a read-only command run by an operator must be able to
// read what a root-privileged run wrote, also when that run had a stricter
// umask than the default 0022 (issue #524, follow-up of PR #525).
package dirmode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// afterStep runs after MkdirAll created and chmodded one directory, with that
// directory's full path. It is nil in production: a test sets it to replace an
// ancestor path in the middle of the walk and check that the next step is not
// redirected, which is what the descriptor-relative walk in MkdirAll is for
// (issue #524 review, internal/dirmode/dirmode_test.go).
var afterStep func(path string)

// beforeOpen runs between mkdirat and the openat of the same name, with the
// new directory's full path, and only when mkdirat succeeded. It is nil in
// production: a test sets it to swap the new entry inside a parent other users
// can write and check that the directory it opens is not chmodded
// (internal/dirmode/dirmode_test.go, the trusted-parent rule in mkdirAtMode).
var beforeOpen func(path string)

// MkdirAll creates dir and every missing parent, like os.MkdirAll, and gives
// each directory it creates the mode mode.Perm(). The result therefore does
// not depend on the process umask: the directories updex documents as 0755
// must stay readable by a non-root reader after a root-privileged run under a
// stricter umask (issue #524). A directory that already exists keeps its mode.
// A setgid bit that the new directory inherits from its parent is kept, as it
// is with os.MkdirAll, so a shared group goes on inheriting what is created
// there.
//
// Every step is bound to the descriptor of the directory it created, not to a
// path: each new directory is made with mkdirat inside the descriptor of its
// parent and chmodded through an O_NOFOLLOW descriptor opened with openat, so
// replacing an ancestor path after it was created — with a symlink, say —
// cannot redirect a later mkdir or chmod at an unrelated directory. The
// deepest existing ancestor is opened by path, as os.MkdirAll resolves it.
// The chmod happens only when the parent directory is trusted (see
// mkdirAtMode): a directory created in a parent that another user can write
// keeps the mode the umask gives it, which is never more open than mode.
func MkdirAll(dir string, mode os.FileMode) error {
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
		if afterStep != nil {
			afterStep(path)
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
//
// A new directory inside a setgid parent inherits the parent's setgid bit, and
// the chmod keeps it: clearing it would quietly end group inheritance for
// everything created there, which os.MkdirAll does not do. The bit is read
// from the mode the kernel applied, never taken from the caller's mode.
//
// Unix has no atomic mkdir-and-open, so the chmod is a permission change of
// whatever openat returns. It therefore happens only when the parent is
// trusted (trustedParent): then no other user can rename the new entry away
// and move another directory in its place between mkdirat and openat. In a
// parent another user can write, the directory is created and opened but not
// chmodded, so it keeps the umask-applied mode, which is never more open than
// mode (os.MkdirAll behaviour).
func mkdirAtMode(parentFd int, name string, mode os.FileMode, path string) (int, error) {
	trusted, err := trustedParent(parentFd)
	if err != nil {
		return -1, err
	}
	err = syscall.Mkdirat(parentFd, name, uint32(mode))
	created := err == nil
	if err != nil && !errors.Is(err, syscall.EEXIST) {
		return -1, fmt.Errorf("create directory %s: %w", path, err)
	}
	if created && beforeOpen != nil {
		beforeOpen(path)
	}
	fd, err := syscall.Openat(parentFd, name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open directory %s: %w", path, err)
	}
	if !created || !trusted {
		return fd, nil
	}
	// Keep a setgid bit the new directory inherited from its parent (a
	// shared group's directories): os.MkdirAll leaves it in place, and
	// clearing it would end group inheritance for what is created there.
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		_ = syscall.Close(fd)
		return -1, fmt.Errorf("check the mode of directory %s: %w", path, err)
	}
	want := uint32(mode)
	if st.Mode&syscall.S_ISGID != 0 {
		want |= syscall.S_ISGID
	}
	if err := syscall.Fchmod(fd, want); err != nil {
		_ = syscall.Close(fd)
		return -1, fmt.Errorf("set mode of directory %s: %w", path, err)
	}
	return fd, nil
}

// trustedParent reports whether only this process's own user or root can add,
// rename or remove entries in the directory open at fd. That is what makes the
// mkdirat-then-openat pair in mkdirAtMode safe: in a directory other users can
// write, one of them can rename the new entry away and move an existing
// private directory in its place, and the chmod would then expose that
// directory. A parent is trusted when it is owned by the effective user or by
// root, and either is not writable by group or others, or carries the sticky
// bit (with the sticky bit another user cannot rename or remove an entry this
// process created).
func trustedParent(fd int) (bool, error) {
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return false, fmt.Errorf("check the parent directory's owner: %w", err)
	}
	owned := st.Uid == uint32(os.Geteuid()) || st.Uid == 0
	writable := st.Mode&0o022 != 0 && st.Mode&syscall.S_ISVTX == 0
	return owned && !writable, nil
}
