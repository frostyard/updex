package sysext

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/frostyard/updex/config"
)

// SysextRunner executes systemd-sysext commands
type SysextRunner interface {
	Refresh() error
	Merge() error
	Unmerge() error
	LinkToSysext(t *config.Transfer) error
}

// DefaultRunner executes real systemd-sysext commands
type DefaultRunner struct{}

func (r *DefaultRunner) Refresh() error {
	return runSysextCommand("refresh")
}

func (r *DefaultRunner) Merge() error {
	return runSysextCommand("merge")
}

func (r *DefaultRunner) Unmerge() error {
	return runSysextCommand("unmerge")
}

func (r *DefaultRunner) LinkToSysext(t *config.Transfer) error {
	return LinkToSysext(t)
}

// runSysextCommand executes a systemd-sysext subcommand
func runSysextCommand(subcommand string) error {
	cmd := exec.Command("systemd-sysext", subcommand)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemd-sysext %s failed: %w", subcommand, err)
	}
	return nil
}
