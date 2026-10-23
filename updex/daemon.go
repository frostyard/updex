package updex

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/frostyard/updex/v2/systemd"
)

const (
	daemonUnitName = "updex-update"
	daemonSchedule = "daily"
)

// daemonUnitPaths returns the absolute paths of the daemon's timer and
// service unit files in the client's systemd manager unit directory, in that
// order.
func (c *Client) daemonUnitPaths() []string {
	return []string{
		filepath.Join(c.systemd.UnitPath, daemonUnitName+".timer"),
		filepath.Join(c.systemd.UnitPath, daemonUnitName+".service"),
	}
}

// EnableDaemon installs, enables, and starts the automatic update timer.
// With opts.DryRun it reports the unit files and actions without writing any
// file or calling systemctl; the already-installed precondition still applies.
// Except in a dry run it holds the operation lock; when another updex
// operation holds it, EnableDaemon changes nothing and returns an error that
// wraps ErrBusy.
func (c *Client) EnableDaemon(ctx context.Context, opts EnableDaemonOptions) (*DaemonActionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("enable daemon: %w", err)
	}
	release, err := c.lockMutation(opts.DryRun)
	if err != nil {
		return nil, err
	}
	defer release()
	if c.systemd.Exists(daemonUnitName) {
		return nil, fmt.Errorf("timer already installed; run 'updex daemon disable' first to reinstall")
	}

	unitFiles := c.daemonUnitPaths()
	if opts.DryRun {
		return &DaemonActionResult{
			Success:   true,
			DryRun:    true,
			UnitFiles: unitFiles,
			Message: fmt.Sprintf("Would install %s and %s, then enable and start %s.timer",
				unitFiles[0], unitFiles[1], daemonUnitName),
		}, nil
	}

	timer := &systemd.TimerConfig{
		Name:           daemonUnitName,
		Description:    "Automatic sysext updates",
		OnCalendar:     daemonSchedule,
		Persistent:     true,
		RandomDelaySec: 3600,
	}
	service := &systemd.ServiceConfig{
		Name:        daemonUnitName,
		Description: "Automatic sysext update service",
		ExecStart:   "/usr/bin/updex features update --no-refresh",
		Type:        "oneshot",
		// Sandbox the root oneshot: read-only /usr and /etc, no new
		// privileges, and restricted syscalls/address families.
		Sandbox: true,
	}

	if err := c.systemd.Install(ctx, timer, service); err != nil {
		return nil, fmt.Errorf("failed to install timer: %w", err)
	}
	if err := c.systemd.Enable(ctx, daemonUnitName+".timer"); err != nil {
		return nil, fmt.Errorf("failed to enable timer: %w", err)
	}
	if err := c.systemd.Start(ctx, daemonUnitName+".timer"); err != nil {
		return nil, fmt.Errorf("failed to start timer: %w", err)
	}

	return &DaemonActionResult{
		Success:   true,
		UnitFiles: unitFiles,
		Message:   "Auto-update daemon enabled",
	}, nil
}

// DisableDaemon stops, disables, and removes the automatic update timer.
// With opts.DryRun it reports the unit files and actions without stopping,
// disabling, or removing anything; the not-installed precondition still
// applies. Except in a dry run it holds the operation lock; when another
// updex operation holds it, DisableDaemon changes nothing and returns an error
// that wraps ErrBusy.
func (c *Client) DisableDaemon(ctx context.Context, opts DisableDaemonOptions) (*DaemonActionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("disable daemon: %w", err)
	}
	release, err := c.lockMutation(opts.DryRun)
	if err != nil {
		return nil, err
	}
	defer release()
	if !c.systemd.Exists(daemonUnitName) {
		return nil, fmt.Errorf("timer not installed; nothing to disable")
	}

	unitFiles := c.daemonUnitPaths()
	if opts.DryRun {
		return &DaemonActionResult{
			Success:   true,
			DryRun:    true,
			UnitFiles: unitFiles,
			Message: fmt.Sprintf("Would stop and disable %s.timer, remove %s and %s, and reload systemd",
				daemonUnitName, unitFiles[0], unitFiles[1]),
		}, nil
	}
	if err := c.systemd.Remove(ctx, daemonUnitName); err != nil {
		return nil, fmt.Errorf("failed to remove timer: %w", err)
	}

	return &DaemonActionResult{
		Success:   true,
		UnitFiles: unitFiles,
		Message:   "Auto-update daemon disabled",
	}, nil
}

// DaemonStatus reports whether the automatic update timer is installed,
// enabled, and active.
func (c *Client) DaemonStatus(ctx context.Context, _ DaemonStatusOptions) (*DaemonStatusResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("get daemon status: %w", err)
	}

	status := &DaemonStatusResult{
		Installed: c.systemd.Exists(daemonUnitName),
	}
	if status.Installed {
		enabled, err := c.systemd.IsEnabled(ctx, daemonUnitName+".timer")
		if err != nil {
			return nil, fmt.Errorf("get daemon status: query enabled state: %w", err)
		}
		active, err := c.systemd.IsActive(ctx, daemonUnitName+".timer")
		if err != nil {
			return nil, fmt.Errorf("get daemon status: query active state: %w", err)
		}
		status.Enabled = enabled
		status.Active = active
		status.Schedule = daemonSchedule
	}
	return status, nil
}
