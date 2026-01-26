package systemd

import (
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

// Install installs timer and service unit files atomically.
// It generates both files from the configs, writes them to UnitPath,
// and calls daemon-reload after installation.
func (m *Manager) Install(timer *TimerConfig, service *ServiceConfig) error {
	// Generate content
	timerContent := GenerateTimer(timer)
	serviceContent := GenerateService(service)

	timerPath := filepath.Join(m.UnitPath, timer.Name+".timer")
	servicePath := filepath.Join(m.UnitPath, service.Name+".service")

	// Check if files already exist - require explicit removal first
	if _, err := os.Stat(timerPath); err == nil {
		return fmt.Errorf("timer file already exists: %s", timerPath)
	}
	if _, err := os.Stat(servicePath); err == nil {
		return fmt.Errorf("service file already exists: %s", servicePath)
	}

	// Write timer file
	if err := os.WriteFile(timerPath, []byte(timerContent), 0644); err != nil {
		return fmt.Errorf("failed to write timer: %w", err)
	}

	// Write service file
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		// Clean up timer file on partial failure
		os.Remove(timerPath)
		return fmt.Errorf("failed to write service: %w", err)
	}

	// Reload systemd
	if err := m.runner.DaemonReload(); err != nil {
		return fmt.Errorf("daemon-reload failed: %w", err)
	}

	return nil
}

// Remove removes timer and service unit files and calls daemon-reload.
// It stops and disables the timer first (ignoring errors if not running/enabled),
// then removes both files. Returns nil on success, aggregates actual errors.
func (m *Manager) Remove(name string) error {
	timerPath := filepath.Join(m.UnitPath, name+".timer")
	servicePath := filepath.Join(m.UnitPath, name+".service")

	// Stop timer (ignore errors - may not be running)
	_ = m.runner.Stop(name + ".timer")

	// Disable timer (ignore errors - may not be enabled)
	_ = m.runner.Disable(name + ".timer")

	var errs []error

	// Remove timer file (ignore IsNotExist errors)
	if err := os.Remove(timerPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("remove timer: %w", err))
	}

	// Remove service file (ignore IsNotExist errors)
	if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("remove service: %w", err))
	}

	// Reload daemon
	if err := m.runner.DaemonReload(); err != nil {
		errs = append(errs, fmt.Errorf("daemon-reload: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during removal: %v", errs)
	}
	return nil
}

// Exists checks if either timer or service file exists at UnitPath
func (m *Manager) Exists(name string) bool {
	timerPath := filepath.Join(m.UnitPath, name+".timer")
	servicePath := filepath.Join(m.UnitPath, name+".service")

	if _, err := os.Stat(timerPath); err == nil {
		return true
	}
	if _, err := os.Stat(servicePath); err == nil {
		return true
	}
	return false
}
