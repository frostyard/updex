package systemd

import (
	"fmt"
	"os/exec"
)

// SystemctlRunner executes systemctl commands
type SystemctlRunner interface {
	DaemonReload() error
	Enable(unit string) error
	Disable(unit string) error
	Start(unit string) error
	Stop(unit string) error
	IsActive(unit string) (bool, error)
	IsEnabled(unit string) (bool, error)
}

// DefaultSystemctlRunner executes real systemctl commands
type DefaultSystemctlRunner struct{}

func (r *DefaultSystemctlRunner) DaemonReload() error {
	return runSystemctl("daemon-reload")
}

func (r *DefaultSystemctlRunner) Enable(unit string) error {
	return runSystemctl("enable", unit)
}

func (r *DefaultSystemctlRunner) Disable(unit string) error {
	return runSystemctl("disable", unit)
}

func (r *DefaultSystemctlRunner) Start(unit string) error {
	return runSystemctl("start", unit)
}

func (r *DefaultSystemctlRunner) Stop(unit string) error {
	return runSystemctl("stop", unit)
}

func (r *DefaultSystemctlRunner) IsActive(unit string) (bool, error) {
	cmd := exec.Command("systemctl", "is-active", unit)
	err := cmd.Run()
	if err != nil {
		// Exit code 3 means inactive, not an error
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 3 {
				return false, nil
			}
		}
		return false, nil
	}
	return true, nil
}

func (r *DefaultSystemctlRunner) IsEnabled(unit string) (bool, error) {
	cmd := exec.Command("systemctl", "is-enabled", unit)
	err := cmd.Run()
	if err != nil {
		// Exit code 1 means disabled/not-found, not an error
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return false, nil
			}
		}
		return false, nil
	}
	return true, nil
}

// runner is the package-level runner used by systemctl operations
var runner SystemctlRunner = &DefaultSystemctlRunner{}

// SetRunner sets the runner for testing (returns cleanup function)
func SetRunner(r SystemctlRunner) func() {
	old := runner
	runner = r
	return func() { runner = old }
}

// runSystemctl executes a systemctl command with the given arguments
func runSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	if err := cmd.Run(); err != nil {
		if len(args) > 0 {
			return fmt.Errorf("systemctl %s failed: %w", args[0], err)
		}
		return fmt.Errorf("systemctl failed: %w", err)
	}
	return nil
}
