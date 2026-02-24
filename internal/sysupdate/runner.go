package sysupdate

import (
	"fmt"
	"os"
	"os/exec"
)

// SysupdateRunner executes systemd-sysupdate commands.
type SysupdateRunner interface {
	// Update runs systemd-sysupdate for a specific component or all components.
	// component can be empty string for "update all enabled".
	Update(component string) error
}

// DefaultRunner executes real systemd-sysupdate commands.
type DefaultRunner struct{}

func (r *DefaultRunner) Update(component string) error {
	args := []string{"update"}
	if component != "" {
		args = append(args, "-C", component)
	}
	cmd := exec.Command("systemd-sysupdate", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemd-sysupdate %v failed: %w", args, err)
	}
	return nil
}

var runner SysupdateRunner = &DefaultRunner{}

// SetRunner replaces the package-level runner. Returns a cleanup function
// that restores the previous runner.
func SetRunner(r SysupdateRunner) func() {
	old := runner
	runner = r
	return func() { runner = old }
}

// Update runs systemd-sysupdate for the given component (or all if empty).
func Update(component string) error {
	return runner.Update(component)
}
