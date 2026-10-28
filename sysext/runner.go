package sysext

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/frostyard/updex/v2/config"
)

// ErrActivationIncomplete reports that a live refresh merged the extensions,
// but that systemd-sysusers failed, so the accounts the merged images declare
// may be missing. A reboot finishes the activation. Refresh wraps this error
// and the sysusers error together, and only when the last refresh succeeded:
// a plain refresh does not run systemd-sysusers.
var ErrActivationIncomplete = errors.New("extensions merged, but activation did not finish")

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
// ADR-0019 gives: merge without a reload, create the accounts and paths the
// merged sysusers.d and tmpfiles.d declare, then refresh again so that
// systemd reloads the manager and starts the units that name them. The
// second refresh forces a remount, because systemd 260 and newer skip a
// refresh whose image set did not change.
type DefaultRunner struct {
	// run executes one command. A nil run runs the command with os/exec,
	// os.Stdout and os.Stderr. Tests set it to record the calls.
	run func(name string, args ...string) error
	// output runs a command and returns its standard output, for the
	// version query. A nil output returns exec.Command.Output.
	output func(name string, args ...string) ([]byte, error)
	// stderr receives the tmpfiles warning. A nil stderr means os.Stderr.
	stderr io.Writer
}

// Refresh activates the staged images in the ADR-0019 order, chosen by the
// major version that systemd-sysext reports:
//
//   - 260 and newer, or an unreadable version: systemd-sysext refresh
//     --no-reload, systemd-sysusers, systemd-tmpfiles --create --graceful,
//     then systemd-sysext refresh --always-refresh=yes. The first command
//     merges the new set before the manager sees it, so the accounts and
//     paths exist when the last one reloads the manager and starts a unit
//     that names them; systemd gives no hook between the merge and the
//     reload. --always-refresh=yes is needed because the set did not change
//     since the first command, and 260 and newer would skip a plain refresh,
//     and with it the reload.
//   - 255 to 259: the same, but the last command is a plain refresh, which
//     always remounts there.
//   - older than 255: a plain refresh, which never reloads the manager, then
//     systemd-sysusers and systemd-tmpfiles --create. No last refresh.
//
// An unreadable version takes the newest form, so an older systemd fails
// loudly on the unknown option instead of skipping the reload silently.
//
// A first-refresh failure returns at once and runs nothing else. A sysusers
// failure is a refresh failure and the sequence continues: the merged set
// must still reach the manager. When the last refresh then succeeds, the
// error wraps ErrActivationIncomplete. A tmpfiles failure is one warning line
// on stderr, so one bad rule cannot fail every refresh.
func (r *DefaultRunner) Refresh() error {
	version, known := r.systemdVersion()
	noReload := !known || version >= 255
	// 260 added --always-refresh. An unreadable version takes the newest
	// form: the unknown option then fails loudly.
	alwaysRefresh := !known || version >= 260

	// Step 1: merge the new set, without a reload when the version can
	// skip one. Older systemd never reloads in a refresh.
	if noReload {
		if err := r.exec("systemd-sysext", "refresh", "--no-reload"); err != nil {
			return fmt.Errorf("systemd-sysext refresh failed: %w", err)
		}
	} else {
		if err := r.exec("systemd-sysext", "refresh"); err != nil {
			return fmt.Errorf("systemd-sysext refresh failed: %w", err)
		}
	}

	var sysusersErr error
	if err := r.exec("systemd-sysusers"); err != nil {
		sysusersErr = fmt.Errorf("systemd-sysusers failed after refresh: %w", err)
	}

	if err := r.exec("systemd-tmpfiles", "--create", "--graceful"); err != nil {
		fmt.Fprintf(r.errStream(), "warning: systemd-tmpfiles --create failed after refresh: %v\n", err)
	}

	// The last refresh, only when step 1 skipped the reload.
	var refreshErr error
	if noReload {
		args := []string{"refresh"}
		if alwaysRefresh {
			args = append(args, "--always-refresh=yes")
		}
		if err := r.exec("systemd-sysext", args...); err != nil {
			refreshErr = fmt.Errorf("systemd-sysext refresh failed: %w", err)
		}
	}

	switch {
	case sysusersErr == nil:
		return refreshErr
	case refreshErr != nil:
		// The merge state is not known, so the extensions may not be
		// merged: report the plain refresh failure only.
		return errors.Join(sysusersErr, refreshErr)
	default:
		return fmt.Errorf("%w: %w", ErrActivationIncomplete, sysusersErr)
	}
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

// systemdVersion reports the major systemd version, from the first line of
// systemd-sysext --version. It returns false when the command fails or the
// line does not parse.
func (r *DefaultRunner) systemdVersion() (int, bool) {
	out, err := r.query("systemd-sysext", "--version")
	if err != nil {
		return 0, false
	}
	return parseSystemdVersion(out)
}

// parseSystemdVersion parses a version line such as "systemd 262 (262-1)":
// the leading digits of the second field are the major version.
func parseSystemdVersion(out []byte) (int, bool) {
	line, _, _ := strings.Cut(string(out), "\n")
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, false
	}
	digits := fields[1]
	end := 0
	for end < len(digits) && digits[end] >= '0' && digits[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	version, err := strconv.Atoi(digits[:end])
	if err != nil {
		return 0, false
	}
	return version, true
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

// query runs one command through the output seam, or with os/exec when it is
// nil, and returns its standard output.
func (r *DefaultRunner) query(name string, args ...string) ([]byte, error) {
	if r.output != nil {
		return r.output(name, args...)
	}
	return exec.Command(name, args...).Output()
}

// errStream is the writer for warning text.
func (r *DefaultRunner) errStream() io.Writer {
	if r.stderr != nil {
		return r.stderr
	}
	return os.Stderr
}
