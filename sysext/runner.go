package sysext

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/frostyard/updex/v2/config"
)

// SysextRunner executes systemd-sysext commands
type SysextRunner interface {
	// Refresh activates the staged images. DefaultRunner runs the
	// ADR-0019 sequence: merge without a reload, apply the merged
	// sysusers.d and tmpfiles.d, then refresh again so the manager
	// reloads.
	Refresh() error
	Merge() error
	Unmerge() error
	LinkToSysext(t *config.Transfer) error
}

// PathSysextRunner optionally lets a runner create links in an explicit
// directory. Clients use this when available while retaining compatibility
// with existing SysextRunner implementations.
type PathSysextRunner interface {
	SysextRunner
	LinkToSysextAt(t *config.Transfer, sysextDir string) error
}

// VersionSysextRunner optionally lets a runner link a specific staged
// version instead of the newest one. A rollback needs it; a runner that
// implements only PathSysextRunner is refused for that operation.
type VersionSysextRunner interface {
	PathSysextRunner
	LinkVersionAt(t *config.Transfer, sysextDir, version string) error
}

// DefaultRunner executes real systemd-sysext commands.
//
// Its Refresh activates a merged set the way a boot does, in the order
// ADR-0019 gives: merge with --no-reload, create the accounts and paths the
// merged sysusers.d and tmpfiles.d declare, then refresh again so that
// systemd reloads the manager and starts the units that name them. The
// second refresh compares nothing, so it remounts the same set.
type DefaultRunner struct {
	// run executes one command. A nil run runs the command with os/exec,
	// os.Stdout and os.Stderr. Tests set it to record the calls.
	run func(name string, args ...string) error
	// stderr receives the tmpfiles warning. A nil stderr means os.Stderr.
	stderr io.Writer
}

// Refresh activates the staged images in the ADR-0019 order: systemd-sysext
// refresh --no-reload, systemd-sysusers, systemd-tmpfiles --create, then
// systemd-sysext refresh. The first refresh merges the new set before the
// manager sees it, so the accounts and paths exist when the second refresh
// reloads the manager and starts a unit that names them; systemd gives no
// hook between the merge and the reload.
//
// A first-refresh failure returns at once and runs nothing else. A sysusers
// failure is a refresh failure, joined with a second-refresh failure: the
// merged set must still reach the manager. A tmpfiles failure is one warning
// line on stderr, so one bad rule cannot fail every refresh. It needs
// systemd 255 or newer for --no-reload.
func (r *DefaultRunner) Refresh() error {
	if err := r.exec("systemd-sysext", "refresh", "--no-reload"); err != nil {
		return fmt.Errorf("systemd-sysext refresh failed: %w", err)
	}

	var sysusersErr error
	if err := r.exec("systemd-sysusers"); err != nil {
		sysusersErr = fmt.Errorf("systemd-sysusers failed after refresh: %w", err)
	}

	if err := r.exec("systemd-tmpfiles", "--create", "--graceful"); err != nil {
		fmt.Fprintf(r.errStream(), "warning: systemd-tmpfiles --create failed after refresh: %v\n", err)
	}

	var refreshErr error
	if err := r.exec("systemd-sysext", "refresh"); err != nil {
		refreshErr = fmt.Errorf("systemd-sysext refresh failed: %w", err)
	}

	return errors.Join(sysusersErr, refreshErr)
}

func (r *DefaultRunner) Merge() error {
	if err := r.exec("systemd-sysext", "merge"); err != nil {
		return fmt.Errorf("systemd-sysext merge failed: %w", err)
	}
	return nil
}

func (r *DefaultRunner) Unmerge() error {
	if err := r.exec("systemd-sysext", "unmerge"); err != nil {
		return fmt.Errorf("systemd-sysext unmerge failed: %w", err)
	}
	return nil
}

func (r *DefaultRunner) LinkToSysext(t *config.Transfer) error {
	return LinkToSysext(t)
}

func (r *DefaultRunner) LinkToSysextAt(t *config.Transfer, sysextDir string) error {
	return LinkToSysextAt(t, sysextDir)
}

func (r *DefaultRunner) LinkVersionAt(t *config.Transfer, sysextDir, version string) error {
	return LinkVersionAt(t, sysextDir, version)
}

// exec runs one command through the seam, or with os/exec when it is nil.
func (r *DefaultRunner) exec(name string, args ...string) error {
	if r.run != nil {
		return r.run(name, args...)
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// errStream is the writer for warning text.
func (r *DefaultRunner) errStream() io.Writer {
	if r.stderr != nil {
		return r.stderr
	}
	return os.Stderr
}
