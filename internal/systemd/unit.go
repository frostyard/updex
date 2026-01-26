// Package systemd provides types and functions for generating and managing
// systemd unit files (timers and services) for scheduling automatic updates.
package systemd

import (
	"fmt"
	"strings"
)

// TimerConfig represents configuration for a systemd timer unit.
type TimerConfig struct {
	// Name is the unit name without extension (e.g., "updex-update")
	Name string
	// Description is the human-readable description for the [Unit] section
	Description string
	// OnCalendar is the timer schedule (e.g., "daily" or "*-*-* 04:00:00")
	OnCalendar string
	// Persistent runs the timer if it missed the last start time
	Persistent bool
	// RandomDelaySec randomizes the start time within this window (in seconds)
	RandomDelaySec int
}

// ServiceConfig represents configuration for a systemd service unit.
type ServiceConfig struct {
	// Name is the unit name without extension (e.g., "updex-update")
	Name string
	// Description is the human-readable description for the [Unit] section
	Description string
	// ExecStart is the full command to execute (e.g., "/usr/bin/updex update --quiet")
	ExecStart string
	// Type is the service type (e.g., "oneshot", "simple")
	Type string
}

// GenerateTimer generates a systemd timer unit file content from the config.
// The returned string contains valid systemd unit file syntax with [Unit],
// [Timer], and [Install] sections.
func GenerateTimer(cfg *TimerConfig) string {
	var b strings.Builder

	// [Unit] section
	b.WriteString("[Unit]\n")
	b.WriteString(fmt.Sprintf("Description=%s\n", cfg.Description))
	b.WriteString("\n")

	// [Timer] section
	b.WriteString("[Timer]\n")
	b.WriteString(fmt.Sprintf("OnCalendar=%s\n", cfg.OnCalendar))
	if cfg.Persistent {
		b.WriteString("Persistent=true\n")
	}
	if cfg.RandomDelaySec > 0 {
		b.WriteString(fmt.Sprintf("RandomizedDelaySec=%ds\n", cfg.RandomDelaySec))
	}
	b.WriteString("\n")

	// [Install] section
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=timers.target\n")

	return b.String()
}

// GenerateService generates a systemd service unit file content from the config.
// The returned string contains valid systemd unit file syntax with [Unit] and
// [Service] sections. No [Install] section is generated since the timer
// handles activation.
func GenerateService(cfg *ServiceConfig) string {
	var b strings.Builder

	// [Unit] section
	b.WriteString("[Unit]\n")
	b.WriteString(fmt.Sprintf("Description=%s\n", cfg.Description))
	b.WriteString("\n")

	// [Service] section
	b.WriteString("[Service]\n")
	b.WriteString(fmt.Sprintf("Type=%s\n", cfg.Type))
	b.WriteString(fmt.Sprintf("ExecStart=%s\n", cfg.ExecStart))

	return b.String()
}
