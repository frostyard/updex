package systemd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Manager handles systemd unit file operations (install, remove, etc.)
type Manager struct {
	// UnitPath is the path to install unit files (default: /etc/systemd/system)
	UnitPath string
	// runner is the SystemctlRunner for daemon-reload, enable, etc.
	runner SystemctlRunner
}

// NewManager creates a manager with default paths
func NewManager() *Manager {
	return &Manager{
		UnitPath: "/etc/systemd/system",
		runner:   &DefaultSystemctlRunner{},
	}
}

// NewTestManager creates a manager with provided unitPath and runner (for testing)
func NewTestManager(unitPath string, runner SystemctlRunner) *Manager {
	return &Manager{
		UnitPath: unitPath,
		runner:   runner,
	}
}

// unitFileState reports whether a unit path is occupied. It follows the
// ADR-0005 rule for privileged writes to managed paths: os.Lstat, never
// os.Stat, so a dangling symlink is seen as itself rather than reported
// absent, and anything present that is not a regular file is an error the
// operator must resolve — Install never writes through it. Stat failures
// other than not-exist are returned too, so an unreadable path can never be
// misread as absent and skip the guard.
func unitFileState(path string) (exists bool, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to inspect unit path %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return true, fmt.Errorf("unit path %s exists and is not a regular file; remove it manually", path)
	}
	return true, nil
}

// removeUnitFile is os.Remove, replaceable by tests that inject a removal
// failure during Install's partial-failure rollback.
var removeUnitFile = os.Remove

// writeUnitFile writes content to path as a fresh regular file, mode 0644,
// via a temporary file in the same directory plus rename. The write itself
// therefore never follows a symlink that appeared at path between the
// unitFileState check and the write: rename replaces the directory entry
// rather than opening whatever it points at.
func writeUnitFile(path, content string) (err error) {
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
	if _, err = tmp.WriteString(content); err != nil {
		return err
	}
	if err = tmp.Chmod(0644); err != nil {
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

// Install installs timer and service unit files atomically.
// It generates both files from the configs, writes them to UnitPath,
// and calls daemon-reload after installation.
//
// Both unit paths are checked with unitFileState before anything is
// written: an existing regular file is an "already exists" error (reinstall
// requires an explicit Remove first, see ADR-0007), and a symlink,
// directory, or other non-regular entry is refused outright rather than
// written through (ADR-0005). ctx is honored before any filesystem write and
// is threaded through the daemon-reload systemctl call, so an already
// canceled or expired context prevents the install outright and a context
// that ends while daemon-reload is running interrupts it promptly.
func (m *Manager) Install(ctx context.Context, timer *TimerConfig, service *ServiceConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Generate content
	timerContent := GenerateTimer(timer)
	serviceContent := GenerateService(service)

	timerPath := filepath.Join(m.UnitPath, timer.Name+".timer")
	servicePath := filepath.Join(m.UnitPath, service.Name+".service")

	// Check if files already exist - require explicit removal first
	if exists, err := unitFileState(timerPath); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("timer file already exists: %s", timerPath)
	}
	if exists, err := unitFileState(servicePath); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("service file already exists: %s", servicePath)
	}

	// Write timer file
	if err := writeUnitFile(timerPath, timerContent); err != nil {
		return fmt.Errorf("failed to write timer: %w", err)
	}

	// Write service file
	if err := writeUnitFile(servicePath, serviceContent); err != nil {
		// Clean up timer file on partial failure; surface a failed
		// cleanup instead of silently dropping it.
		var removeErr error
		if rmErr := removeUnitFile(timerPath); rmErr != nil {
			removeErr = fmt.Errorf("failed to remove timer file after service write failure: %w", rmErr)
		}
		return errors.Join(fmt.Errorf("failed to write service: %w", err), removeErr)
	}

	// Reload systemd
	if err := m.daemonReload(ctx); err != nil {
		return fmt.Errorf("daemon-reload failed: %w", err)
	}

	return nil
}

// Remove stops and disables the timer, removes both unit files, and calls
// daemon-reload. Every step is attempted, and all failures are returned
// together. An already canceled or expired ctx prevents the removal outright;
// a context that ends while one of the systemctl steps is running interrupts
// that step promptly, and later steps are still attempted.
func (m *Manager) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	timerPath := filepath.Join(m.UnitPath, name+".timer")
	servicePath := filepath.Join(m.UnitPath, name+".service")

	var errs []error

	if err := m.stop(ctx, name+".timer"); err != nil {
		errs = append(errs, fmt.Errorf("stop timer: %w", err))
	}

	if err := m.disable(ctx, name+".timer"); err != nil {
		errs = append(errs, fmt.Errorf("disable timer: %w", err))
	}

	// Remove timer file (ignore IsNotExist errors)
	if err := os.Remove(timerPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("remove timer: %w", err))
	}

	// Remove service file (ignore IsNotExist errors)
	if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("remove service: %w", err))
	}

	// Reload daemon
	if err := m.daemonReload(ctx); err != nil {
		errs = append(errs, fmt.Errorf("daemon-reload: %w", err))
	}

	return errors.Join(errs...)
}

// Exists checks if either timer or service file exists at UnitPath.
//
// It uses os.Lstat, so a dangling symlink or any other non-regular entry at
// a unit path counts as present: the path is occupied by something Install
// would refuse to write through, so callers such as `daemon enable` report
// "already installed, remove first" instead of attempting an install that
// unitFileState will reject, and `daemon status` does not report a planted
// entry as absent. Only a not-exist result means absent; any other Lstat
// error is treated as present for the same conservative reason.
func (m *Manager) Exists(name string) bool {
	timerPath := filepath.Join(m.UnitPath, name+".timer")
	servicePath := filepath.Join(m.UnitPath, name+".service")

	for _, path := range []string{timerPath, servicePath} {
		if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
			return true
		}
	}
	return false
}

// Enable enables a systemd unit through the manager's configured runner.
func (m *Manager) Enable(ctx context.Context, unit string) error {
	return m.enable(ctx, unit)
}

// Start starts a systemd unit through the manager's configured runner.
func (m *Manager) Start(ctx context.Context, unit string) error {
	return m.start(ctx, unit)
}

// IsEnabled reports whether a systemd unit is enabled through the manager's
// configured runner.
func (m *Manager) IsEnabled(ctx context.Context, unit string) (bool, error) {
	return m.isEnabled(ctx, unit)
}

// IsActive reports whether a systemd unit is active through the manager's
// configured runner.
func (m *Manager) IsActive(ctx context.Context, unit string) (bool, error) {
	return m.isActive(ctx, unit)
}

// daemonReload, enable, disable, start, stop, isActive, and isEnabled honor
// ctx before touching the runner at all, then prefer the runner's
// ContextSystemctlRunner methods when it implements that interface so an
// in-flight systemctl command is interrupted promptly; a runner that only
// implements the legacy SystemctlRunner keeps working exactly as before,
// without in-flight cancellation.

func (m *Manager) daemonReload(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cr, ok := m.runner.(ContextSystemctlRunner); ok {
		return cr.DaemonReloadContext(ctx)
	}
	return m.runner.DaemonReload()
}

func (m *Manager) enable(ctx context.Context, unit string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cr, ok := m.runner.(ContextSystemctlRunner); ok {
		return cr.EnableContext(ctx, unit)
	}
	return m.runner.Enable(unit)
}

func (m *Manager) disable(ctx context.Context, unit string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cr, ok := m.runner.(ContextSystemctlRunner); ok {
		return cr.DisableContext(ctx, unit)
	}
	return m.runner.Disable(unit)
}

func (m *Manager) start(ctx context.Context, unit string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cr, ok := m.runner.(ContextSystemctlRunner); ok {
		return cr.StartContext(ctx, unit)
	}
	return m.runner.Start(unit)
}

func (m *Manager) stop(ctx context.Context, unit string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cr, ok := m.runner.(ContextSystemctlRunner); ok {
		return cr.StopContext(ctx, unit)
	}
	return m.runner.Stop(unit)
}

func (m *Manager) isActive(ctx context.Context, unit string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if cr, ok := m.runner.(ContextSystemctlRunner); ok {
		return cr.IsActiveContext(ctx, unit)
	}
	return m.runner.IsActive(unit)
}

func (m *Manager) isEnabled(ctx context.Context, unit string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if cr, ok := m.runner.(ContextSystemctlRunner); ok {
		return cr.IsEnabledContext(ctx, unit)
	}
	return m.runner.IsEnabled(unit)
}
