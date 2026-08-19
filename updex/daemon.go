package updex

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/systemd"
)

const (
	daemonUnitName = "updex-update"
	daemonSchedule = "daily"
)

// EnableDaemon installs, enables, and starts the automatic update timer.
func (c *Client) EnableDaemon(ctx context.Context, _ EnableDaemonOptions) (*DaemonActionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("enable daemon: %w", err)
	}
	if c.systemd.Exists(daemonUnitName) {
		return nil, fmt.Errorf("timer already installed; run 'updex daemon disable' first to reinstall")
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

	if err := c.systemd.Install(timer, service); err != nil {
		return nil, fmt.Errorf("failed to install timer: %w", err)
	}
	if err := c.systemd.Enable(daemonUnitName + ".timer"); err != nil {
		return nil, fmt.Errorf("failed to enable timer: %w", err)
	}
	if err := c.systemd.Start(daemonUnitName + ".timer"); err != nil {
		return nil, fmt.Errorf("failed to start timer: %w", err)
	}

	return &DaemonActionResult{
		Success: true,
		Message: "Auto-update daemon enabled",
	}, nil
}

// DisableDaemon stops, disables, and removes the automatic update timer.
func (c *Client) DisableDaemon(ctx context.Context, _ DisableDaemonOptions) (*DaemonActionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("disable daemon: %w", err)
	}
	if !c.systemd.Exists(daemonUnitName) {
		return nil, fmt.Errorf("timer not installed; nothing to disable")
	}
	if err := c.systemd.Remove(daemonUnitName); err != nil {
		return nil, fmt.Errorf("failed to remove timer: %w", err)
	}

	return &DaemonActionResult{
		Success: true,
		Message: "Auto-update daemon disabled",
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
		status.Enabled, _ = c.systemd.IsEnabled(daemonUnitName + ".timer")
		status.Active, _ = c.systemd.IsActive(daemonUnitName + ".timer")
		status.Schedule = daemonSchedule
	}
	return status, nil
}
